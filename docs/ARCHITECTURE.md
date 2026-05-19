# Архитектура — Рубеж ИИ

«Программный комплекс безопасной маршрутизации, обезличивания, аудита и контроля
запросов к системам искусственного интеллекта».

## 1. Назначение

On-prem-ready AI-шлюз для госкомпаний, операторов КИИ и enterprise. Сотрудники
безопасно используют LLM; служба ИБ, юристы (комплаенс) и администраторы
контролируют данные, модели, политики, инциденты и аудит.

## 2. Главный принцип: Rules-first, LLM-assisted, policy-decided

1. Сначала работают **детерминированные** правила: regex, словари, NER, secret scanner.
2. Малая локальная LLM **может помогать** находить смысловые риски, но **не принимает**
   финальное решение.
3. Финальное решение принимает **policy engine**.
4. **Все** действия журналируются (append-only audit).

## 3. Компоненты MVP

| Сервис | Технология | Ответственность |
|--------|------------|-----------------|
| `rubezh-web` | React + TypeScript | UI: чат, документы, политики, модели, аудит, инциденты |
| `rubezh-api` | Go | API Gateway, auth, оркестрация чата, **Policy Engine**, **LLM Router**, Audit API |
| `rubezh-sanitizer` | Python / FastAPI | Детекторы ПДн/секретов/коммерческих данных, маскирование, risk scoring, NER-интерфейс |
| `rubezh-worker` | Python | Парсинг документов, chunking, embeddings, индексация |
| `PostgreSQL` + `pgvector` | PostgreSQL 16 | Единый source of truth: пользователи, политики, документы, чанки, embeddings, аудит, инциденты, mapping'и |
| `MinIO` | MinIO | Object storage загруженных документов |

Откладываются до обоснования: Keycloak/OIDC (auth — после базового MVP), vLLM
(начинаем с mock-адаптера), Redis/Valkey, NATS, ClickHouse, Qdrant, Kubernetes.

## 4. Поток данных (главный MVP-сценарий)

```
            ┌────────────┐
            │ rubezh-web │  React + TS
            └─────┬──────┘
                  │ REST + SSE
            ┌─────▼───────────────────────────────────┐
            │ rubezh-api (Go)                          │
            │  api → auth → orchestration              │
            │   ├── LLM Router        ── Policy Engine │
            │   └── Audit API                          │
            └──┬───────────────┬──────────────┬────────┘
               │ HTTP          │ pgx          │ S3
        ┌──────▼──────┐  ┌─────▼────────┐  ┌──▼──────┐
        │ sanitizer   │  │ PostgreSQL   │  │ MinIO   │
        │ (Python)    │  │  + pgvector  │  └─────────┘
        └─────────────┘  └─────┬────────┘
                               │ poll: FOR UPDATE SKIP LOCKED
                        ┌──────▼───────┐
                        │ rubezh-worker│ (Python)
                        └──────────────┘
```

## 5. Жизненный цикл запроса `POST /api/chat`

1. `rubezh-api` принимает запрос (текст + опционально ссылки на документы).
2. API вызывает `sanitizer /sanitize/preview` → получает сущности, маскированный
   текст, risk score, псевдонимы.
3. **Policy Engine** (Go) оценивает: `(режим доверия модели, риск-классы, типы
   сущностей, роль пользователя)` → решение
   `allow_raw | allow_masked | allow_summary_only | deny | escalate`.
4. Если `deny` → создаётся инцидент + audit event, пользователю возвращается отказ.
5. Если разрешено → **LLM Router** отправляет (как правило, маскированный) текст
   выбранному провайдеру (mock / OpenAI-совместимый), ответ стримится через **SSE**.
6. Пост-проверка ответа: повторное сканирование на утёкшие секреты и маркеры
   prompt injection.
7. По политике — обратная подстановка псевдонимов в ответе для пользователя.
8. Записывается **append-only** audit event (риск-классы + masked representation,
   raw-значения — отдельно и зашифрованно).

## 6. Жизненный цикл загрузки документа

1. `POST /api/documents` — `rubezh-api` сохраняет файл в MinIO, создаёт запись
   `documents` со статусом `pending`.
2. `rubezh-worker` опрашивает БД (`SELECT ... FOR UPDATE SKIP LOCKED`), берёт задачу.
3. Worker: парсинг → chunking → вызов sanitizer для разметки сущностей →
   embeddings (mock/реальные) → запись `document_chunks` + `embeddings`.
4. Статус документа: `pending → processing → done | failed`.

## 7. Ключевые архитектурные решения

### 7.1. Очередь задач — на PostgreSQL, без брокера

Worker берёт задачи через `SELECT ... FOR UPDATE SKIP LOCKED`. Для MVP-объёмов
этого достаточно, не нужны Redis/Kafka/NATS. **Путь апгрейда:** при росте нагрузки
очередь выносится в NATS JetStream без изменения доменной логики worker'а.

### 7.2. Streaming — SSE, не WebSocket

Поток токенов LLM **однонаправленный** (server → client). SSE проще WebSocket:
работает поверх обычного HTTP, имеет авто-reconnect, дружелюбен к reverse-proxy,
тривиально аудируется. Двунаправленность WebSocket здесь не нужна.

### 7.3. Policy Engine — внутри Go API

Решение принимается в синхронном пути запроса детерминированным кодом Go
(`internal/policy`). Sanitizer лишь поставляет findings — он **не решает**.

### 7.4. Контракты Go ↔ Python

Межсервисные контракты зафиксированы как JSON Schema в `docs/contracts/`. Обе
стороны валидируют payload по схеме; контрактные тесты — в итерациях 4 и 8.

## 8. Режимы доверия моделей

| Режим | Описание | Политика по умолчанию |
|-------|----------|----------------------|
| `external` | Внешние облачные LLM (OpenAI и т. п.) | Только masked text; raw corporate data **запрещён** |
| `russian_cloud` | Российские облачные LLM | Только masked text по умолчанию |
| `on_prem` | LLM в периметре заказчика | Расширенные разрешения по политике |
| `trusted_local` | Доверенная локальная LLM (vLLM в периметре) | Максимальные разрешения по политике |

**Полный запрет** отправки raw corporate data во внешние модели по умолчанию.

> **MVP:** четыре режима — стабильный контракт (`docs/contracts/policy.schema.json`).
> LLM Router MVP реализует подмножество: mock-провайдер и OpenAI-совместимый
> адаптер; `russian_cloud` / `on_prem` / `trusted_local` подключаются позже без
> изменения контракта.

## 9. Модель данных (MVP-сущности)

`users`, `roles`, `model_providers`, `policies`, `policy_versions`, `documents`,
`document_chunks`, `embeddings`, `chat_sessions`, `chat_messages`,
`sanitization_results`, `pseudonym_mappings`, `audit_events`, `incidents`.

Принципы: PostgreSQL — единый source of truth; все изменения через миграции;
`created_at`/`updated_at` где применимо; `audit_events` — **append-only**;
`pseudonym_mappings` — отдельная таблица, шифрование значений.

## 10. Слои внутри сервисов

Доменная логика, API-слой, хранение и UI **не смешиваются**.

- **rubezh-api (Go):** `cmd/` · `internal/{api,auth,audit,llm,policy,storage,config}`.
- **rubezh-sanitizer (Python):** `app/{api,domain,detectors,masking,policy_client}` · `tests/`.
- **rubezh-worker (Python):** `app/{parsers,chunking,embeddings,queue}` · `tests/`.
- **rubezh-web (TS):** `src/{api,components,features,hooks,routes,types}`.

## 11. Роли пользователей

`user` (сотрудник), `security_officer` (ИБ), `compliance_officer` (юрист/комплаенс),
`admin` (администратор), `auditor` (аудитор), `developer` (модуль «Рубеж Код» — будущее).

## 12. Деплой

MVP — Docker Compose. Позже — Helm/Kubernetes. Миграциями владеет `rubezh-api`,
применяет one-shot контейнер `migrate` при старте окружения.

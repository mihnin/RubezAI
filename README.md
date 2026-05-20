# Рубеж ИИ

**Программный комплекс безопасной маршрутизации, обезличивания, аудита и
контроля запросов к системам искусственного интеллекта.**

On-prem-ready приложение для госкомпаний, операторов КИИ и enterprise. Позволяет
сотрудникам безопасно использовать LLM, а службе ИБ, юристам и администраторам —
контролировать данные, модели, политики, инциденты и аудит.

## Главный сценарий

Пользователь вводит запрос или загружает договор → система находит ПДн,
реквизиты, коммерческую тайну, секреты и рискованные фрагменты → обезличивает
данные → policy engine принимает решение → запрос уходит в разрешённую LLM →
ответ проверяется → пользователь получает результат → ИБ видит полный audit trail.

Принцип архитектуры: **Rules-first, LLM-assisted, policy-decided**.

## Архитектура

| Сервис | Стек | Назначение |
|--------|------|------------|
| `rubezh-web` | React + TypeScript | Пользовательский и админский интерфейс |
| `rubezh-api` | Go | API Gateway, auth, Policy Engine, LLM Router, Audit API |
| `rubezh-sanitizer` | Python / FastAPI | Детекция и обезличивание ПДн, секретов, коммерческих данных |
| `rubezh-worker` | Python | Парсинг документов, chunking, embeddings, индексация |
| PostgreSQL + pgvector | PostgreSQL 16 | Единый source of truth, audit, embeddings |
| MinIO | — | Object storage документов |

Подробно — [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## Документация

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — архитектура и решения
- [`docs/PLAN.md`](docs/PLAN.md) — живой план реализации по итерациям
- [`docs/API.md`](docs/API.md) — REST API
- [`docs/THREAT_MODEL.md`](docs/THREAT_MODEL.md) — модель угроз
- [`docs/SECURITY_CHECKLIST.md`](docs/SECURITY_CHECKLIST.md) — чеклист безопасности
- [`docs/contracts/`](docs/contracts/) — межсервисные контракты (JSON Schema)

## Требования

- Docker 24+ и Docker Compose v2
- Для разработки frontend — Node.js 20+
- Go SDK **не требуется** — `rubezh-api` собирается в Docker

## Быстрый старт

```bash
# 1. Переменные окружения
cp .env.example .env          # Windows: Copy-Item .env.example .env

# 2. Применить миграции БД (one-shot)
docker compose up -d postgres minio
docker compose run --rm migrate

# 3. Поднять все сервисы (включая Web UI)
docker compose up -d --build

# 4. Проверить статус
docker compose ps
```

После старта:

- **Web UI**     — http://localhost:5173 (вход через выбор роли в dev-режиме)
- **API**       — http://localhost:8080/health
- **Sanitizer** — http://localhost:8001/health
- **Worker**    — http://localhost:8002/health
- **MinIO**     — http://localhost:9001 (консоль)

### Минимальный e2e-сценарий

1. Открыть http://localhost:5173 → выбрать роль `user` → войти.
2. **Чат** → отправить «Меня зовут Иван Иванов, мой телефон +79001234567» →
   увидеть SSE-стрим ответа + плашку решения и количество обезличенных сущностей.
3. **Документы** → загрузить PDF/DOCX → дождаться `status=готов`.
4. **Аудит** → видны события `chat_request`, `chat_response`,
   `document_uploaded` без raw-данных пользователей.
5. **Инциденты** — при утечке или deny создаётся `auto`-инцидент со ссылкой
   на `audit_event_id`; терминальный переход требует `resolution`.

### Подключение локальной LLM (LM Studio / Ollama / vLLM)

«Рубеж ИИ» поддерживает любые OpenAI-совместимые endpoint'ы. Пример с
**LM Studio + DeepSeek-R1-Distill-Qwen-7B**:

1. Запустить LM Studio → загрузить модель → включить server на `:1234`.
2. Открыть **Web UI → Модели → Добавить**:
   - Имя: `deepseek-local`
   - Trust level: **trusted_local** (LLM получит raw данные — оправдано
     для модели внутри периметра)
   - Adapter: `openai_compatible`
   - Endpoint: `http://host.docker.internal:1234/v1`
     *(Windows/Mac Docker Desktop; на Linux — IP хоста)*
   - API key: пустой (LM Studio не требует)
3. В разделе **Чат** в правом верхнем углу — picker провайдеров.
   Раскройте → в поле «Модель» впишите имя загруженной модели
   (например `deepseek-r1-distill-qwen-7b`). Выбор сохраняется в
   localStorage.
4. Hot-reload Router — новая модель доступна **без restart api**.

Для **внешних API** (OpenAI/Anthropic/Yandex GPT) — `trust_level: external`
и API key через UI или env. Внешние модели получают только **masked** текст.

## Статус проекта

**MVP завершён.** Backend — Go (api) + Python (sanitizer, worker), Frontend —
React + Vite, всё запускается одной командой `docker compose up`. Все 15
критериев приёмки MVP закрыты. Прогресс по итерациям — в
[`docs/PLAN.md`](docs/PLAN.md).

## Лицензия

MIT — см. [LICENSE](LICENSE).

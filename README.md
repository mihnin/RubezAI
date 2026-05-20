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

### Внешние модели (Claude, ChatGPT, Gemini, Grok, DeepSeek-cloud)

Миграция `000013_seed_external_providers` создаёт seed для 5 внешних
провайдеров (выключены до ввода ключа):

| Имя              | Adapter           | Endpoint                                               |
|------------------|-------------------|--------------------------------------------------------|
| `openai-gpt`     | openai_compatible | `https://api.openai.com/v1`                            |
| `anthropic-claude` | **anthropic**   | `https://api.anthropic.com` (Messages API)             |
| `google-gemini`  | openai_compatible | `https://generativelanguage.googleapis.com/v1beta/openai` |
| `xai-grok`       | openai_compatible | `https://api.x.ai/v1`                                  |
| `deepseek-cloud` | openai_compatible | `https://api.deepseek.com/v1`                          |

Установка ключа (UI: **Модели → Изменить API-ключ**, или curl/CLI):
```bash
rubezh login --role admin
rubezh models set-key deepseek-cloud --key 'sk-…'
# или через env: --key '$DEEPSEEK_KEY'
```

Все 5 — `trust_level: external` → получают **только masked** текст. ПДн в
ответе восстанавливаются обратно для пользователя (псевдоним → raw).

### Локальные модели (LM Studio / Ollama / vLLM) — для обезличивания

Локальные LLM в архитектуре «Рубежа» — **не для основного чата**, а как
LLM-reviewer внутри `rubezh-sanitizer` (фильтр 2/3, см.
`docs/ARCHITECTURE.md §2.1`). Пример: DeepSeek-R1-Distill-Qwen-7B через
LM Studio на `host.docker.internal:1234`. Trust level = `trusted_local`.

### CLI

Бинарь `rubezh` — статический Go-CLI к API. Сборка:
```bash
docker build -t rubezh-cli -f cli/Dockerfile .
alias rubezh='docker run --rm --network rubezh-ai_rubezh \
  -e RUBEZH_API_URL=http://rubezh-api:8080 -e HOME=/tmp \
  -v ~/.rubezh:/tmp/.rubezh rubezh-cli'
```

Команды:
```bash
rubezh login --role user                              # сохранить токен
rubezh models list
rubezh models set-key NAME --key 'sk-…'
rubezh chat --provider deepseek-cloud --model deepseek-chat "Привет"
rubezh docs upload ./contract.pdf
rubezh docs list
rubezh audit list --type chat_request
rubezh incidents list
```

Все CLI-команды проходят тот же sanitizer + policy engine, что и Web UI;
audit-trail и инциденты создаются одинаково.

## Статус проекта

**MVP завершён.** Backend — Go (api) + Python (sanitizer, worker), Frontend —
React + Vite, всё запускается одной командой `docker compose up`. Все 15
критериев приёмки MVP закрыты. Прогресс по итерациям — в
[`docs/PLAN.md`](docs/PLAN.md).

## Лицензия

MIT — см. [LICENSE](LICENSE).

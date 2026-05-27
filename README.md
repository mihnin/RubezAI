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

### Внешние модели — два пути

#### А. Прямые API-провайдеры (с собственным ключом)

Миграция `000013_seed_external_providers` создаёт seed для 5 cloud-
провайдеров (выключены до ввода ключа):

| Имя              | Adapter           | Endpoint                                               |
|------------------|-------------------|--------------------------------------------------------|
| `openai-gpt`     | openai_compatible | `https://api.openai.com/v1`                            |
| `anthropic-claude` | **anthropic**   | `https://api.anthropic.com` (Messages API)             |
| `google-gemini`  | openai_compatible | `https://generativelanguage.googleapis.com/v1beta/openai` |
| `xai-grok`       | openai_compatible | `https://api.x.ai/v1`                                  |
| `deepseek-cloud` | openai_compatible | `https://api.deepseek.com/v1`                          |

Установка ключа (UI: **Модели → Изменить API-ключ**, или CLI):

```bash
rubezh login --role admin
rubezh models set-key deepseek-cloud --key 'sk-…'   # или --key '$DEEPSEEK_KEY'
```

#### Б. SSH-CLI bridge (без API-ключей у RubezAI)

Если Codex / Claude Code / Gemini Antigravity / Grok-CLI уже
залогинены на отдельном Ubuntu-сервере серверной учёткой — RubezAI
может ходить туда по SSH (ed25519 + known_hosts pinning) без хранения
API-ключей.

Adapter `ssh_cli` (миграции `000017`–`000019`) запускает обёртку
`/usr/local/bin/ai-bridge <provider>` на сервере; JSON stdin/stdout с
полями `prompt, model, files?[]`. Контракт — `deploy/ssh-bridge/README.md`.

| Имя              | Endpoint     | Default model               |
|------------------|--------------|-----------------------------|
| `codex-cli`      | `codex`      | `gpt-5.3-codex`             |
| `claude-code-cli`| `claude`     | `claude-opus-4-7`           |
| `gemini-cli`     | `gemini`     | `Gemini 3.5 Flash (High)` (через Antigravity CLI) |
| `grok-build`     | `grok-build` | `grok-build`                |

Включается в `.env`:

```
SSH_LLM_ENABLED=true
SSH_LLM_HOST=<server-ip>
SSH_LLM_USER=aiagent
SSH_LLM_REMOTE_COMMAND=/usr/local/bin/ai-bridge
RUBEZH_SSH_KEY_PATH=C:/Users/<user>/.ssh/...  # host path
RUBEZH_SSH_KNOWN_HOSTS_PATH=C:/Users/<user>/.ssh/known_hosts
```

`default_model` хранится в БД (миграция 000019); меняется через
`PATCH /api/models/:id`. См. `docs/SSH_CLI_MODELS.md` про процедуру
безопасной смены модели.

**Файлы-артефакты от модели:** Codex/Claude/Gemini создают
xlsx/csv/png/pdf в `WORKSPACE` — bridge возвращает base64, adapter
формирует Markdown data-links, UI рендерит download-chips в чате.

Все провайдеры — `trust_level: external` → получают **только masked**
текст. ПДн в ответе восстанавливаются обратно для пользователя
(псевдоним → raw, через кнопку «Показать реальные данные» в UI).

#### CLI fan-out по всем ssh_cli

```bash
rubezh chat --all "Сравни три подхода к декомпозиции"
# Последовательно проходит по codex-cli, claude-code-cli, gemini-cli, grok-build.
# Каждый вызов — отдельный chat-request: sanitize/policy/audit
# отрабатывают независимо, инварианты не обходятся.
```

### Локальные модели (LM Studio / Ollama / vLLM) — для обезличивания

Локальные LLM в архитектуре «Рубежа» — **не для основного чата**, а как
LLM-reviewer внутри `rubezh-sanitizer` (фильтр 2/3, см.
`docs/ARCHITECTURE.md §2.1`). Пример: DeepSeek-R1-Distill-Qwen-7B через
LM Studio на `host.docker.internal:1234`. Trust level = `trusted_local`.

Включается переменными окружения санитайзера (H.3) — пусто по умолчанию
(тогда работают только детерминированные детекторы, фильтр 1):

```bash
SANITIZER_LLM_URL=http://host.docker.internal:1234/v1
SANITIZER_LLM_MODEL=deepseek-r1-distill-qwen-7b
SANITIZER_LLM_TIMEOUT=20   # reasoning-моделям нужен запас
```

LLM **не принимает** решений allow/deny (это policy engine) и fail-open:
её недоступность не ломает обезличивание — детектор просто не добавляет
кандидатов. Подбирает то, что пропустил regex (контекстные секреты и т. п.).

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

## Разработка

### Контракт Go ↔ TypeScript (G.1)

Go-DTO (`rubezh-api/internal/api/*.go`) и Zod-схемы
(`rubezh-web/src/api/schemas.ts`) держатся синхронно автоматически:

1. Go golden-тест `internal/api/contract_export_test.go` рефлексией DTO
   генерирует нормализованные формы в `rubezh-web/src/test/contracts/*.json`.
2. TS-тест `rubezh-web/src/test/contract.test.ts` сверяет эти формы с
   Zod-схемами (поля, типы, nullability).

**При изменении любого DTO:**

```bash
# 1. перегенерировать контракт (упадёт при дрейфе — это и есть сигнал)
docker run --rm -v c:/dev/RubezAI:/repo -v rubezh-go-cache:/go/pkg/mod \
  -w /repo/rubezh-api golang:1.25-bookworm \
  go test ./internal/api/ -run TestContractShape
# 2. закоммитить обновлённые rubezh-web/src/test/contracts/*.json
# 3. привести Zod-схему в соответствие до зелёного npm test
```

CI гоняет оба теста (`web`) и проверяет отсутствие незакоммиченного дрейфа
контракта (`contract-sync`). Подробнее — `docs/design/g1-contract-tests.md`.

## Статус проекта

**MVP завершён + post-MVP волны W1/W2/W3 закрыты:**

- Итерации 0–16 + дополнения E/F/G.1/G.2/H/H.3/J + RAG (11) + SSH-CLI
  bridge (17–19) + Codex/Claude/Gemini/Grok файлы — все реализованы и
  подтверждены живым e2e через `docker compose up` (без mock'ов).
- W1 (security P1): RBAC + sanitize + audit для `system_prompt` и
  `review.system_prompts`; документ-flow корректно подставляет тело
  документа из preview_token.
- W2 (UX/stability P2): SSE truncation guard, review-loop видит
  файлы (с защитой от PII через pmap.Remask), worker `/live`/`/ready`
  раздельно, чистка тестовой полюции БД (–353 провайдеров).
- W3 (contract + docs): `sanitize.schema.json` расширен до 4 контекстов,
  preview_token_miss audit дедуплицирован, документация
  (API/ARCHITECTURE/PLAN/README) синхронизирована с фактической
  реализацией.

Прогресс — в [`docs/PLAN.md`](docs/PLAN.md), архитектура —
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), API —
[`docs/API.md`](docs/API.md).

## Лицензия

MIT — см. [LICENSE](LICENSE).

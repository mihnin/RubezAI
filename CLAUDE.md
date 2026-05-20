# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Что это

«Рубеж ИИ» — on-prem AI-шлюз для госкомпаний, операторов КИИ и enterprise.
Сотрудники безопасно используют LLM; ИБ, юристы и админы контролируют данные,
модели, политики, инциденты и аудит. Подробно — `docs/ARCHITECTURE.md`.

## Главный архитектурный принцип

**Rules-first, LLM-assisted, policy-decided.** Это определяет всю систему:

1. Сначала работают **детерминированные** детекторы (regex, словари, secret
   scanner) — фильтр 1.
2. Малая локальная русскоязычная LLM **подсказывает** смысловые риски — фильтр
   2/3. Она подключается через интерфейс `Detector` и **не принимает** решений.
3. Финальное решение `allow_raw / allow_masked / allow_summary_only / deny /
   escalate` принимает **policy engine** (Go, `internal/policy`).
4. Каждое решение и действие журналируется в append-only `audit_events`.

Найденные сущности заменяются обратимыми псевдонимами (`ФИО_001`, `ДОГОВОР_014`);
raw-значения шифруются (AES-256-GCM) и хранятся отдельно; в ответе LLM псевдонимы
подставляются обратно. Конвейер — `docs/ARCHITECTURE.md §2.1`.

## Архитектура

Шесть компонентов (без Redis/Kafka/ClickHouse/Qdrant/K8s — намеренно):

| Сервис | Стек | Роль |
|--------|------|------|
| `rubezh-web` | Vite + React 19 + RR v7 + TanStack Query + Zod + Tailwind v4 | Web UI: 6 экранов с picker'ом провайдера/модели в чате |
| `rubezh-api` | Go 1.25 + chi v5 + pgx v5 | API Gateway, auth, Policy Engine, LLM Router, Audit/Incidents API |
| `rubezh-sanitizer` | Python 3.12 / FastAPI | детекция и обезличивание ПДн/секретов/коммерческих данных |
| `rubezh-worker` | Python 3.12 / asyncpg | парсинг документов, chunking, embeddings, БД-очередь |
| `rubezh` (CLI) | Go 1.25 statically linked | login/chat (SSE)/models/docs/audit/incidents из терминала |
| PostgreSQL 16 + pgvector | — | единый source of truth: данные, аудит, embeddings |
| MinIO | — | object storage документов |

Что важно знать, прежде чем менять код:

- **Контракты между сервисами** — JSON Schema в `docs/contracts/`
  (`sanitize.schema.json`, `policy.schema.json`). Go и Python обязаны
  соответствовать; контракт sanitizer проверяется тестом против схемы.
- **Схемой БД владеет `rubezh-api`** — миграции в `rubezh-api/migrations/`
  (golang-migrate). БД вручную не создаётся. `audit_events` — append-only
  (триггер БД); `pseudonym_mappings` — отдельная таблица, raw шифруется.
- **Очередь worker'а — на PostgreSQL** (`FOR UPDATE SKIP LOCKED`), без брокера.
- **LLM-streaming — SSE**, не WebSocket (поток токенов однонаправленный).
- Где код:
  - `rubezh-sanitizer/app/{detectors,masking,domain,api}`
  - `rubezh-api/internal/{api,auth,policy,llm,audit,storage,config,crypto,chat,sanitizer}`
  - `rubezh-api/cmd/{rubezh-api,rubezh}` — сервер и CLI
  - `rubezh-web/src/{pages,components,auth,api,test}` — TSX-страницы, Zod-схемы (`api/schemas.ts`), SSE-клиент (`api/sse.ts`), fetch-обёртка (`api/client.ts`)
  - `rubezh-worker/app/{parsers,embeddings,queue,processor,sanitizer_client}`

## Адаптеры LLM и trust levels

- **`mock`** (`internal/llm/mock.go`) — для тестов и development.
- **`openai_compatible`** (`internal/llm/openai.go`) — `POST /chat/completions`,
  заголовок `Authorization: Bearer`. Покрывает OpenAI, DeepSeek-cloud, Grok
  (api.x.ai), Gemini (`/v1beta/openai/...`), любой OpenAI-совместимый endpoint
  (vLLM, LM Studio, Ollama).
- **`anthropic`** (`internal/llm/anthropic.go`) — `POST /v1/messages`,
  заголовки `x-api-key` + `anthropic-version`, system отдельным top-level
  полем, `max_tokens` обязателен. Для Claude.

`model_providers.trust_level` определяет, что получает LLM:

- **`trusted_local`** → `allow_raw` (модель внутри периметра, raw данные
  допустимы). Используется для **локальных** моделей (LM Studio + DeepSeek-7B,
  Ollama). В архитектуре «Рубежа» локальные модели — это **роль
  sanitizer-reviewer** (фильтр 2/3), а не chat. Эти провайдеры можно скрыть
  из chat-picker'а через `is_enabled=false`.
- **`external`** → `allow_masked` (внешний API получает только обезличенный
  текст; в ответе LLM псевдонимы восстанавливаются обратно для пользователя).
  Используется для **облачных** моделей (OpenAI / Claude / Gemini / Grok /
  DeepSeek cloud).

Hot-reload: `Router.Replace(providers)` атомарно подменяет набор провайдеров
после `POST /api/models` или `POST /api/models/:id/api-key` — изменения видны
без restart api (`tryReloadRouter` в `internal/api/models.go`).

## Команды

### rubezh-sanitizer (Python, каталог `rubezh-sanitizer/`)

```
uv run pytest                                              # все тесты
uv run pytest tests/test_pii_detectors.py::test_detect_email   # один тест
uv run ruff check app tests          # линт (добавить --fix для автоправок)
uv run mypy app                      # проверка типов (strict)
uv lock                              # пересобрать uv.lock после правки зависимостей
```

### rubezh-api (Go) — собирается и тестируется **только в Docker**

Go SDK локально не установлен. Команды запускать **из PowerShell** (Git Bash
искажает unix-пути в аргументах docker). Монтируется весь репозиторий —
контрактные тесты читают `docs/contracts/`. Префикс:

```
docker run --rm -v c:/dev/RubezAI:/repo -v rubezh-go-cache:/go/pkg/mod -w /repo/rubezh-api golang:1.25-bookworm
```

```
<префикс> go test -race ./...                       # все тесты
<префикс> go test -run TestParseToken ./internal/auth   # один тест/пакет
<префикс> sh -c "go vet ./... && gofmt -l ."        # анализ и формат
<префикс> go mod tidy                               # обновить go.mod/go.sum
<префикс> sh -c "CGO_ENABLED=0 go build -o /repo/bin/rubezh ./cmd/rubezh"   # CLI binary
```

### rubezh-web (TypeScript, каталог `rubezh-web/`)

Тесты и сборка через docker (Node 20+ — на хосте может не быть):

```
docker run --rm -v c:/dev/RubezAI/rubezh-web:/work -w /work node:20-alpine sh -c "npm test"
docker run --rm -v c:/dev/RubezAI/rubezh-web:/work -w /work node:20-alpine sh -c "npm run build"
docker run --rm -v c:/dev/RubezAI/rubezh-web:/work -w /work node:20-alpine sh -c "npm install --package-lock-only --ignore-scripts"
```

Тесты Vitest в `src/test/`: `schemas.test.ts` (Zod ↔ Go DTO), `sse.test.ts`
(парсер RFC 6202), `client.test.ts` (apiFetch + 401-redirect),
`LoginPage.test.tsx` (RTL).

### Инфраструктура и сервисы

```
docker compose up -d --build --wait <service>   # собрать и поднять сервис
docker compose ps                               # статус
docker compose run --rm migrate                 # применить миграции БД
make migrate-verify       (Linux/CI)            # миграции + проверка схемы
.\make.ps1 migrate-verify (Windows)
```

`make` / `make.ps1` (зеркала): `infra`, `infra-down`, `config`, `migrate`,
`migrate-verify`, `ps`, `logs`, `clean`.

## Особенности окружения (Windows)

- **Go — только в Docker** (нет локального SDK); используется `golang:1.25` —
  это требование `pgx v5.9.2`.
- **Python локально 3.14, в контейнерах 3.12** — `uv` сам ставит 3.12 по
  `requires-python`.
- **Git Bash искажает unix-пути** в аргументах docker (`/src` → `C:/Program
  Files/Git/src`). Для таких команд использовать PowerShell.
- **`python -m json.tool` на Windows** читает UTF-8 ответ как cp1251 — для
  проверки JSON-ответов сервисов читать с явным `encoding="utf-8"`.

## Рабочий процесс

- **Живой план — `docs/PLAN.md`.** Принятые пункты зачёркнуты; технический долг —
  в секции «Технический долг (бэклог)».
- Итерации идут **автономно**, без паузы на подтверждение пользователя.
- Каждая итерация: TDD (тест отдельным коммитом раньше реализации) → QA-агент
  проектирует функциональные тесты → реализация → отдельный управляемый коммит →
  ревью независимого архитектора (subagent `Plan`).
- Порог приёмки — **≥ 9.5/10**, цель — 10. При оценке < 9.5 — доработка и
  повторное ревью того же шага.
- **После завершения итерации обновлять `CLAUDE.md` и `docs/PLAN.md`.**
- CI — GitHub Actions, `.github/workflows/ci.yml`.

## Конвенции кода

- Файлы ≤ 500 строк, функции ≤ 60 строк (без серьёзного обоснования).
- Не смешивать domain / API-слой / storage / UI.
- Все зависимости — в lock-файлах (`package-lock.json`, `go.sum`, `uv.lock`).
- **Python:** FastAPI; Pydantic v2; Ruff; mypy strict; без `any` без обоснования.
  NER и LLM-review — интерфейсы (`Detector`), для MVP — mock.
- **Go:** `context` во всех I/O; structured logging (`slog`); ошибки оборачивать
  с контекстом (`%w`); без глобального состояния; тесты — стандартный `testing`.
- **TypeScript:** strict; Zod для рантайм-валидации; TanStack Query; React
  Router v7; тесты — Vitest + RTL.

## Безопасность (инварианты)

- Raw secrets и raw ПДн **никогда** не пишутся в application logs (доменные
  модели исключают raw из `repr`).
- Внешние LLM (`trust_level: external`) получают **только masked text**;
  локальные (`trusted_local`) могут получить raw.
- `audit_events` — append-only (триггер БД `rubezh_block_mutation`); хранит
  риск-классы и masked representation.
- `pseudonym_mappings` — отдельная таблица, raw зашифрован AES-256-GCM с
  AAD = SHA-256(session_id‖pseudonym).
- API-ключи провайдеров — `model_providers.api_key_encrypted`, AES-256-GCM с
  AAD = `"model_provider_api_key:"+id` (двухфазный CREATE из-за AAD=id).
- Решение allow/deny принимает **только** policy engine; всё логируется.
- `PATCH /api/incidents/:id` требует `If-Match: <updated_at-RFC3339Nano>`
  (RFC 7232: 428 если отсутствует, 412 при mismatch). Backend выставляет
  `ETag` на `GET /:id` и на успешный `PATCH`.
- DELETE/PATCH провайдера → `Router.Replace()` синхронно (hot-reload без
  restart api). Best-effort: ошибка reload логируется, но не откатывает CRUD.
- Чеклист — `docs/SECURITY_CHECKLIST.md`; модель угроз — `docs/THREAT_MODEL.md`.

## Текущий статус

**MVP полностью готов к продуктовой эксплуатации.** Итерации 0–16 + E +
F + H реализованы; backend и frontend подтверждены реальным e2e-прогоном
через `docker compose up` (без mock'ов на критическом пути).

**Что работает end-to-end:**

- `docker compose up` поднимает 6 сервисов (postgres / minio /
  rubezh-api / rubezh-sanitizer / rubezh-worker / rubezh-web), все
  healthy. Миграции 000001–000013 применяются через
  `docker compose run --rm migrate`.
- Web UI на `http://localhost:5173` (Vite + React 19 + RR v7 +
  TanStack Query + Zod + Tailwind v4) с 6 экранами: Login (выбор роли),
  Chat (SSE + provider/model picker), Documents, Policies, Models,
  Audit Log, Incidents.
- CLI `rubezh` (`rubezh-api/cmd/rubezh`, Go static binary,
  `cli/Dockerfile`) даёт тот же pipeline через терминал.
- 5 внешних провайдеров засеяны миграцией `000013` (`openai-gpt`,
  `anthropic-claude`, `google-gemini`, `xai-grok`, `deepseek-cloud`) с
  `is_enabled=false` до ввода ключа.

**Ключевые архитектурные блоки:**

- **Адаптеры LLM** — `mock`, `openai_compatible`, `anthropic` (см.
  одноимённую секцию выше). `Router.Replace()` для hot-reload.
- **Шифрование** (`internal/crypto/aesgcm.go`) с AAD: mappings
  (`AAD=SHA-256(session_id‖pseudonym)`) и api_key
  (`AAD="model_provider_api_key:"+id`, двухфазный CREATE).
- **Audit / Incidents** — append-only, ETag/If-Match (RFC 7232) на
  PATCH, auto-incident через atomic Tx3 c partial unique index
  `idx_incidents_one_auto_per_event` (race-safe).
- **Graceful shutdown** — `SIGINT/SIGTERM → srv.Shutdown(30s) →
  orchestrator.Wait()` для гарантии завершения фоновых
  auto-incident-горутин.
- **Frontend контракты** — Zod-схемы в `rubezh-web/src/api/schemas.ts`
  сверены с реальными Go-DTO (имена корневых полей: `documents`,
  `events`, `incidents`; голые массивы для policies/models).
  Изменение контракта → одновременная правка DTO и Zod.

**Авторизация (MVP):** dev-токен HMAC-SHA256 на роль (6 ролей: user,
security_officer, compliance_officer, admin, auditor, developer).
`POST /api/auth/dev-login` (вне auth-middleware) выпускает токен;
фронт хранит в `localStorage` + `Authorization: Bearer`. После MVP —
OIDC RP (`docs/design/identity.md §«MVP auth-flow»`).

**Технический долг — пуст.** Все F.* (F1 ETag, F2 hot-reload, F3
resolution UX) закрыты. См. `docs/PLAN.md §«Технический долг»`.

Прогресс по итерациям — всегда в `docs/PLAN.md`.

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
| `rubezh-web` | React + TS | UI (итерация 12+) |
| `rubezh-api` | Go 1.25 | API Gateway, auth, Policy Engine, LLM Router, Audit API |
| `rubezh-sanitizer` | Python 3.12 / FastAPI | детекция и обезличивание ПДн/секретов/коммерческих данных |
| `rubezh-worker` | Python | парсинг документов, chunking, embeddings (итерация 10+) |
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
- Где код: `rubezh-sanitizer/app/{detectors,masking,domain,api}`,
  `rubezh-api/internal/{api,auth,policy,llm,audit,storage,config}`.

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
```

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
- Внешние LLM по умолчанию получают **только masked text**.
- `audit_events` — append-only; хранит риск-классы и masked representation.
- `pseudonym_mappings` — отдельная таблица, raw зашифрован (AES-256-GCM).
- Решение allow/deny принимает **только** policy engine; всё логируется.
- Чеклист — `docs/SECURITY_CHECKLIST.md`; модель угроз — `docs/THREAT_MODEL.md`.

## Текущий статус

Итерации 0–8 приняты архитектором; Итерации 4–8 доведены до 10/10.
Итерация 8 закрывает критерий 5 («можно отправить chat-запрос»):
`/api/chat` (SSE, два аудит-события, проверка утечки до restore,
fail-closed), `/api/chat/sessions`, контракт `chat.schema.json`.
**Этап A (UX/UI дизайн перед frontend-итерациями)** — принят
архитектором 9.7/10; артефакты — `docs/design/ui-{trends-2026,scope,
system}.md` + `docs/design/ui/{login,chat,documents,policies,models,
audit-log,incidents}.md`; ретро-правка Итерации 8 (M2): `SseError` и
`SseMeta` получили `request_id` (тесты зелёные).
Открытый техдолг: единый `LLM_API_KEY` на все openai-провайдеры —
пункт после MVP (см. `docs/PLAN.md`, секция «Технический долг»).
Идентичность: dev-токен на роль + посев dev-пользователей,
**фронт-flow зафиксирован**: `localStorage` + `Authorization: Bearer`
для MVP, замена — OIDC RP после MVP (см. `docs/design/identity.md
§«MVP auth-flow»`).
Следующая — Итерация 9 (Audit/Incidents API, авто-инцидент при deny,
шифрованный mapping псевдонимов, `GET /api/chat/sessions/:id/messages`).
Прогресс — всегда в `docs/PLAN.md`.

# CLAUDE.md — Рубеж ИИ

Указания для Claude Code при работе в этом репозитории.

## Что это

«Рубеж ИИ» — on-prem AI-шлюз для госкомпаний, операторов КИИ и enterprise.
Сотрудники безопасно используют LLM; ИБ, юристы и админы контролируют данные,
модели, политики, инциденты и аудит. Подробно — `docs/ARCHITECTURE.md`.

## Главный принцип

**Rules-first, LLM-assisted, policy-decided.** Сначала детерминированные правила
(regex, словари, NER, secret scanner); локальная LLM лишь подсказывает смысловые
риски; финальное решение принимает policy engine; всё журналируется.

## Архитектура (MVP)

Шесть компонентов: `rubezh-web` (React/TS), `rubezh-api` (Go), `rubezh-sanitizer`
(Python/FastAPI), `rubezh-worker` (Python), PostgreSQL+pgvector, MinIO. Без
Redis/Kafka/ClickHouse/Qdrant/K8s в MVP.

## Рабочий процесс

- **Живой план — `docs/PLAN.md`.** Обновляется в конце каждой итерации.
- Каждая итерация: TDD → реализация → отдельный управляемый коммит → ревью
  независимого архитектора → самооценка. При оценке ≥ 9/10 пункт в `PLAN.md`
  зачёркивается.
- Перед началом новой итерации свериться с `PLAN.md`.
- **После завершения итерации обновлять `CLAUDE.md` и `docs/PLAN.md`.**

## Команды

Go-сервис собирается **только в Docker** (локальный Go SDK не нужен).

| Действие | Linux/CI | Windows |
|----------|----------|---------|
| Инфраструктура | `make infra` | `.\make.ps1 infra` |
| Проверка compose | `make config` | `.\make.ps1 config` |
| Остановить | `make infra-down` | `.\make.ps1 infra-down` |

Прямые команды: `docker compose up -d postgres minio`, `docker compose config`.

## Конвенции кода

- Файлы ≤ 500 строк, функции ≤ 60 строк (без серьёзного обоснования).
- Не смешивать domain / API-слой / storage / UI.
- **TypeScript:** strict mode; без `any` без обоснования; Zod для рантайм-валидации
  payload'ов; TanStack Query; React Router v7; ESLint + Prettier; тесты — Vitest + RTL.
- **Python:** 3.12 в контейнерах; FastAPI; Pydantic v2; pytest; Ruff; mypy.
  Слои: `api / domain / detectors / masking / policy_client / tests`. NER и
  LLM-review — интерфейсы (mock для MVP).
- **Go:** 1.23+; пакеты `cmd` + `internal/{api,auth,audit,llm,policy,storage,config}`;
  `context` во всех I/O; structured logging (`slog`); ошибки оборачивать с
  контекстом; без глобального состояния; тесты — стандартный `testing`.
- Все зависимости — в lock-файлах (`package-lock.json`, `go.sum`, `uv.lock`).

## Безопасность (инварианты)

- Raw secrets **никогда** не пишутся в обычные application logs.
- Audit log хранит риск-классы и masked representation, не raw.
- `pseudonym_mappings` — отдельная таблица, шифрование AES-GCM.
- Внешние LLM по умолчанию получают **только masked text**.
- Все решения policy engine логируются.
- `audit_events` — append-only.
- Чеклист — `docs/SECURITY_CHECKLIST.md`; модель угроз — `docs/THREAT_MODEL.md`.

## Текущий статус

Итерация 0 (скелет репозитория) — **принята архитектором, 9.5/10**.
Следующая — Итерация 1 (схема БД и миграции). Актуальный прогресс — всегда
в `docs/PLAN.md`.

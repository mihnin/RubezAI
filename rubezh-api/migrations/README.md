# Миграции БД — rubezh-api

Инструмент: [golang-migrate](https://github.com/golang-migrate/migrate).
Файлы — `NNNNNN_name.up.sql` / `NNNNNN_name.down.sql`, применяются по порядку
версии. PostgreSQL — единый source of truth; БД вручную не создаётся.

## Применение

```
docker compose run --rm migrate          # применить все up-миграции
make migrate                              # то же (Linux/CI)
.\make.ps1 migrate                        # то же (Windows)
```

## Проверка схемы

```
make migrate-verify        /        .\make.ps1 migrate-verify
```

Скрипт `tests/verify_schema.sql` проверяет: расширения `vector`/`pgcrypto`,
наличие всех 14 MVP-таблиц, конвенцию `created_at`/`updated_at`, append-only
поведение `audit_events`, отсутствие raw-колонок в `pseudonym_mappings`.
До применения миграций скрипт намеренно падает (TDD-«красный» тест).

## Откат (только для разработки)

Сервис `migrate` по умолчанию выполняет `up`. Для отката команда переопределяется;
значения подключения берутся из `.env` (`POSTGRES_USER` / `POSTGRES_PASSWORD` /
`POSTGRES_DB`, по умолчанию — `rubezh`):

```
docker compose run --rm migrate \
  -path=/migrations \
  -database="postgres://rubezh:rubezh@postgres:5432/rubezh?sslmode=disable" \
  down 6
```

> На Windows выполнять из PowerShell — Git Bash искажает путь `/migrations`.

## TDD-цикл (Итерация 1)

Тест `tests/verify_schema.sql` написан до миграций и закоммичен отдельным
коммитом раньше них.

- **Красный:** на чистой БД скрипт падает — `ERROR: Расширение pgvector не
  установлено` (psql exit 3).
- **Зелёный:** после `docker compose run --rm migrate` —
  `=== SCHEMA VERIFICATION PASSED ===` (exit 0).
- **Откат:** `down 6` → повторный прогон снова красный → `up` → снова зелёный.

## Список миграций

| Версия | Содержание |
|--------|------------|
| 000001 | расширения `vector`/`pgcrypto`, функции `set_updated_at`, `rubezh_block_mutation` |
| 000002 | `roles`, `users` (+ сид 6 ролей) |
| 000003 | `model_providers`, `policies`, `policy_versions` |
| 000004 | `documents`, `document_chunks`, `embeddings` |
| 000005 | `chat_sessions`, `chat_messages`, `sanitization_results` |
| 000006 | `pseudonym_mappings`, `audit_events` (append-only), `incidents` |

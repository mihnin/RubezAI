# Scale-ready throttle (W4.3 — design-doc, реализация по необходимости)

## Контекст

`internal/chat/throttle.go::throttleReporter` — in-memory token-bucket
per user, используется для:

- `ragPolicyRevisedReporter` (10/час): policy_revised_after_rag (план §Р4).
- `previewTokenMissReporter` (5/мин): дедуп W3.2.

**KNOWN LIMITATION** (зафиксировано в `throttle.go:18-20`):

1. Restart api сбрасывает state — пользователь после рестарта снова получит
   полный лимит. Для редко-срабатывающих событий (10/час, 5/мин) это
   почти неощутимо.
2. При горизонтальном масштабировании (несколько rubezh-api за балансером)
   throttle становится per-instance → эффективный лимит `5 × N` per user.

Для on-prem MVP с одним инстансом обе проблемы пренебрежимы; реализация
in-memory выбрана осознанно (нулевые транзакционные накладные).

## Когда нужна реализация

**Триггеры:**
- Развёртывание `rubezh-api` в HA-режиме (≥2 инстанса за балансером).
- ИБ-требование «throttle нельзя обойти рестартом» (compliance).
- Появление throttle'ов с low-limit/high-frequency, где per-instance
  дублирование делает аудит-сигнал бесполезным.

Пока ни один триггер не активен, реализация остаётся in-memory.

## Дизайн

### Схема БД

```sql
-- migration 000020_throttle_buckets.up.sql
CREATE TABLE throttle_buckets (
    kind         TEXT NOT NULL,        -- например 'preview_token_miss', 'rag_policy_revised'
    bucket_key   TEXT NOT NULL,        -- user_id (или composite, см. ниже)
    window_start TIMESTAMPTZ NOT NULL,
    count        INTEGER NOT NULL DEFAULT 0,
    throttled    BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (kind, bucket_key)
);

-- Дополнительный индекс для очистки старых window'ов (не Pkey).
CREATE INDEX idx_throttle_buckets_window
    ON throttle_buckets (window_start);
```

`bucket_key` — обычно `user_id`, но при необходимости per-session
переключение делается на `user_id || ':' || session_id` без изменения схемы.

### Алгоритм Allow (атомарный)

```sql
-- Использует UPSERT + window reset в одной транзакции.
INSERT INTO throttle_buckets (kind, bucket_key, window_start, count, throttled)
VALUES ($kind, $key, now(), 1, false)
ON CONFLICT (kind, bucket_key) DO UPDATE
   SET count = CASE
                  WHEN throttle_buckets.window_start + $window < now()
                  THEN 1
                  ELSE throttle_buckets.count + 1
               END,
       window_start = CASE
                        WHEN throttle_buckets.window_start + $window < now()
                        THEN now()
                        ELSE throttle_buckets.window_start
                      END,
       throttled = CASE
                     WHEN throttle_buckets.window_start + $window < now()
                     THEN false
                     ELSE throttle_buckets.throttled OR
                          throttle_buckets.count + 1 > $limit
                   END
RETURNING count, throttled, window_start = now() AS window_reset;
```

`Allow()` возвращает `(allowed, shouldEmitThrottled)` по:

- `allowed = count <= limit`
- `shouldEmitThrottled = NOT allowed AND first_time_throttled` (нужно
  сравнить старое `throttled` с новым — простейшая реализация через
  отдельный SELECT перед INSERT в одной TX).

### Интерфейс Go

```go
// В chat-пакете:
type ThrottleStore interface {
    Allow(ctx context.Context, kind, key string) (allowed, throttled bool, err error)
}

// Реализация in-memory (existing throttleReporter за фасадом):
type MemoryThrottleStore struct { ... }

// Реализация Postgres (новая):
type PgThrottleStore struct { pool *pgxpool.Pool }
```

Orchestrator получает `ThrottleStore` через DI; `NewOrchestrator(...)`
по умолчанию использует Memory; production-конфиг переключается env
`THROTTLE_BACKEND=postgres`.

### Периодическая очистка

Старые window'ы (`window_start < now() - 2*window`) → DELETE по cron-job'у
или background-задаче в `rubezh-api` (5-минутный тик). Без чистки таблица
растёт линейно с числом уникальных user'ов.

## Цена ошибки в текущей in-memory реализации

Сценарий: атакующий с RBAC=user устроил шторм с одним `preview_token`
(SSE-обрыв retry-loop). Без throttle: 60 audit-events в минуту. С W3.2:
5 + 1_throttled, дальше тишина до конца окна → 6 событий/мин. Уже норма.

Сценарий: 2 инстанса api за nginx. Тот же шторм: до 12 событий/мин
(5 + 1 throttled per instance × 2). Тоже норма, не атака.

## Решение

**Сейчас (W4):** не реализуем, фиксируем design-doc, **зачёркиваем
ребро технического долга** через явное «известное ограничение» в
комментариях `throttle.go` (уже есть, W4.4 дополнен).

**Когда понадобится:** ~3-5 дней работы по этому design-doc'у:
миграция + storage-реализация + DI-интеграция + integration-тесты +
cron очистки + операционная документация.

## Связанные итерации

- W3.2 MJ-3 — первый throttle на preview_token_miss.
- Итерация 11 §Р4 — `ragPolicyRevisedReporter` (10/час).
- W4 ревью архитектора зафиксировало «in-memory переживёт restart» как
  acceptable для on-prem MVP.

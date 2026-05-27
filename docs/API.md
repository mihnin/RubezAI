# REST API — Рубеж ИИ

Базовый префикс: `/api`. Формат — JSON, UTF-8.

**Аутентификация:** `Authorization: Bearer <token>`. Два пути:

- **dev-токен** `role.HMAC-SHA256` (MVP, `POST /api/auth/dev-login`).
- **OIDC** (`OIDC_*` env): Authorization Code + PKCE через `/api/auth/oidc/login`
  и `/api/auth/oidc/callback` (см. `docs/design/oidc-rp.md`). CLI `rubezh login --sso` —
  loopback по RFC 8252.

**Авторитетные контракты** — JSON Schema в `docs/contracts/`:

- `chat.schema.json` — чат, SSE-события, preview/reveal.
- `policy.schema.json` — policy engine.
- `sanitize.schema.json` — обезличивание (4 контекста).
- `rag.schema.json` — RAG-параметры и метаданные источников.

Если эта таблица расходится с .schema.json — **истина в schema**.

## Соглашения

- Идентификаторы — UUID v4.
- Временные метки — RFC 3339 (UTC).
- Ошибки — `{ "error": { "code": "string", "message": "string" } }`,
  для SSE — `event: error` с `request_id`.
- Доступ по ролям указан в колонке «Роли».
- Все эндпойнты ниже **реализованы и протестированы вживую**, если не
  указано иное.

## Аутентификация

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| POST | `/api/auth/dev-login` | — | Получить dev-токен по роли |
| GET | `/api/auth/oidc/login` | — | Старт OIDC-flow (Authorization Code + PKCE) |
| GET | `/api/auth/oidc/callback` | — | OIDC callback; редирект на frontend с токеном |

`POST /api/auth/dev-login`:

```json
{ "role": "user" }
```

Ответ:

```json
{
  "token": "user.a4f3...b9c2",
  "role": "user",
  "user_id": "uuid",
  "expires_at": "2026-05-21T14:32:00Z"
}
```

OIDC включается env-параметрами (`OIDC_ISSUER/CLIENT_ID/CLIENT_SECRET/REDIRECT_URL`);
пустые → остаётся только dev-login. Подробно — `docs/design/oidc-rp.md`,
`docs/design/identity.md`.

## Чат

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| POST | `/api/chat` | any | Отправить запрос; ответ — **SSE-поток** |
| POST | `/api/chat/preview` | any | Гейт «J.1»: одиночный sanitize, кэширует preview_token |
| POST | `/api/chat/messages/{id}/reveal` | владелец | «J.2»: показать реальные данные за псевдонимами (audit + no-store) |
| GET | `/api/chat/sessions` | any | Список сессий |
| POST | `/api/chat/sessions` | any | Создать пустую сессию |
| GET | `/api/chat/sessions/:id/messages` | владелец | История с masked-payload + sanitization_summary |

### `POST /api/chat`

Тело — `chat.schema.json#ChatRequest`:

```jsonc
{
  "session_id": "uuid | null",
  "message": "string (1..16384)",
  "provider": "string",
  "model": "string | null",
  "preview_token": "hex(64) | null",   // J.0 — токен из /api/chat/preview
  "system_prompt": "string?",          // admin/developer ONLY; иначе 403
  "review": {                          // server-side ревизия несколькими моделями
    "enabled": true,
    "providers": ["claude-code-cli", "gemini-cli"],
    "system_prompts": { "claude-code-cli": "..." },  // admin/developer ONLY
    "max_rounds": 3
  },
  "rag": {                             // Итерация 11
    "enabled": true,
    "document_ids": ["uuid", "..."],
    "top_k": 5
  }
}
```

**RBAC (W1.1):** `system_prompt` и `review.system_prompts` доступны
только `admin`/`developer`. Любая другая роль → `403 Forbidden`. Для
этих ролей значения проходят тот же sanitize-pipeline (context=
`system_prompt`/`review_system_prompt`); audit `chat_request.detail`
содержит `system_prompt_sha256` + `system_prompt_masked`, raw plaintext
не хранится.

### SSE-поток (Content-Type: `text/event-stream`)

Порядок событий:

```
event: meta       data: {decision, risk{level,score,classes}, provider, reasons[], request_id}
event: status     data: {request_id, stage, message, provider, model}      ← W2/W3 — live progress
event: rag_hits   data: {request_id, hits:[{document_id, filename, chunk_index, relevance}]}
event: delta      data: {content: "частичный ответ"}
event: done       data: {request_id, assistant_message_id}                 ← W1 — id для reveal
event: error      data: {message, request_id}                              ← терминальное при сбое
```

- `meta` — всегда первое.
- `status`-события идут между `meta` и первой `delta` (стадии:
  `policy_checked`, `rag_search`/`rag_done`, `policy_revised`, `blocked`,
  `llm_call`, `llm_done`, `streaming_answer`; review-loop добавляет
  `review_started`, `review_round`, `review_call`, `review_done`,
  `review_complete`/`review_fallback`/`review_revise`).
- Для `deny`/`escalate` сразу после `meta` идёт `done` без `delta`.
- `done` несёт `assistant_message_id` — нужен для `/messages/{id}/reveal`.
- `error` всегда содержит `request_id` (контракт `chat.schema.json#SseError`).
- **W2.1 truncation guard:** SSE-клиент (`rubezh-web/src/api/sse.ts`)
  синтезирует `error`-event на EOF без `done`/`error` (proxy/network drop).

### `POST /api/chat/preview` (J.1)

Гейт перед отправкой во внешнюю LLM: одиночный sanitize, кэширование
результата под одноразовым `preview_token` (TTL 10 мин, owner-scoped).

```jsonc
// Запрос
{ "text": "string", "provider": "string", "document_id": "uuid?" }

// Ответ
{
  "preview_token": "hex(64)",
  "sanitized_text": "...",
  "entities": [{type, category, pseudonym, confidence, detector}],
  "risk": {level, score, classes}
}
```

Затем клиент шлёт `/api/chat` с `preview_token` — тот же sanitize
используется без повторного вычисления (детерминизм preview↔chat).

### `POST /api/chat/messages/{id}/reveal` (J.2)

Раскрывает реальные данные для записанного `allow_masked`-ответа.
Owner-only; пишет audit `response_revealed`; ответ `Cache-Control: no-store`.

```json
{ "revealed_text": "...реальные ФИО/телефоны/секреты восстановлены..." }
```

## Документы

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| POST | `/api/documents` | any | Загрузить (multipart, ≤50 МБ, pdf/docx/txt/...) |
| GET | `/api/documents` | any (свои), supervisor (все) | Список |
| GET | `/api/documents/:id` | acl | Метаданные + статус обработки worker'ом |
| GET | `/api/documents/:id/chunks` | acl | Список chunks с masked-payload |
| GET | `/api/documents/:id/masked` | acl | Скачать обезличенный текст (J.3) |
| GET | `/api/documents/:id/download` | acl + admin | Оригинал (audit) |
| DELETE | `/api/documents/:id` | acl + admin | Soft-delete |
| POST | `/api/documents/:id/retry` | acl | Перезапустить обработку (после `failed`) |

Workflow: upload → MinIO → worker (asyncpg-очередь `documents.status`)
парсит/chunk/sanitize/embed → `status=ready`. UI poll'ит через
`GET /api/documents/:id`.

## Обезличивание

Внешнего HTTP-эндпойнта `/api/sanitize/preview` в `rubezh-api` нет
(удалён вместе с устаревшей итерацией 12-плана). Гейтом для UI служит
`POST /api/chat/preview` (см. выше).

Внутренний sanitize — `POST /sanitize/preview` сервиса `rubezh-sanitizer:8001`,
вызывается из `rubezh-api` и `rubezh-worker`. Контракт —
`sanitize.schema.json`. `context` ∈ `{chat, document, system_prompt,
review_system_prompt}` (W3.1).

## RAG

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| POST | `/api/search` | any (свои документы, supervisor — все) | Семантический поиск по embeddings |

Тело: `{query, document_ids?, top_k}`. Rate-limit token-bucket
30 RPM/burst 5 per user (`UserRateLimiter`); 429 + `Retry-After` +
audit `rate_limit_exceeded` при превышении.

Параметры RAG в `/api/chat` — поле `rag` (см. выше); система ставит
retrieved chunks в system-prefix, пишет audit `rag_query`, ужесточает
severity на +1 ступень (anti-DoS cap).

## Аудит

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| GET | `/api/audit-events` | sec/comp/audit/admin | Журнал с фильтрами (период, актор, тип, решение, риск, leak-flag) |
| GET | `/api/audit-events/:id` | sec/comp/audit/admin | Полная запись для drawer |
| GET | `/api/audit-events/export` | sec/comp/audit/admin | Экспорт CSV/NDJSON (alias POST для удобства browser-export) |
| POST | `/api/audit-events/export` | sec/comp/audit/admin | Экспорт CSV/NDJSON; сам аудитируется |

`audit_events` — **append-only**, методов изменения/удаления нет
(триггер БД `rubezh_block_mutation`, миграция `000003`).

## Инциденты

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| GET | `/api/incidents` | sec/comp/audit/admin | Список (фильтры) |
| POST | `/api/incidents` | sec/admin | Создать вручную (manual-trigger) |
| GET | `/api/incidents/:id` | sec/comp/audit/admin | Карточка |
| PATCH | `/api/incidents/:id` | sec/admin | Статус/assignee/severity (нужен `If-Match` ETag) |
| GET | `/api/incidents/:id/notes` | sec/comp/audit/admin | Список заметок расследования |
| POST | `/api/incidents/:id/notes` | sec/admin (assignee) | Добавить заметку |

Авто-создание при `deny`/`escalate`/`response_leak_detected`. Уникальный
partial-index `idx_incidents_one_auto_per_event` (race-safe).

## Политики

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| GET | `/api/policies` | sec/comp/admin | Список действующих |
| POST | `/api/policies` | sec/admin | Создать (новая версия) |
| POST | `/api/policies/test` | sec/comp/admin | Прогнать на примере (sanitize → policy, audit `policy_tested`) |

Endpoint `GET /api/policies/:id` пока не реализован — детали политики
запрашиваются через список (`GET /api/policies` отдаёт content_json
актуальной версии).

## Модели (LLM-провайдеры)

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| GET | `/api/models` | any | Список: name, adapter, trust_level, endpoint, is_enabled, has_api_key, **default_model** |
| POST | `/api/models` | admin | Создать (опц. api_key, default_model) |
| PATCH | `/api/models/:id` | admin/dev | Toggle `is_enabled` и/или сменить `default_model` |
| DELETE | `/api/models/:id` | admin | Удалить (409 если ссылается история → soft-disable) |
| POST | `/api/models/:id/api-key` | admin | Обновить/сбросить API-ключ |

Поддерживаемые adapter'ы: `mock`, `openai_compatible`, `anthropic`,
`ssh_cli` (Codex/Claude/Gemini/Grok-build через SSH-bridge,
`deploy/ssh-bridge/`). См. `CLAUDE.md §«Адаптеры LLM и trust levels»`.

**`default_model`** (миграция 000019) — основной канал управления
именем модели. Приоритет в `chat.modelOrDefault`: явный `model` из
запроса → `provider.default_model` → adapter-fallback (ssh_cli) →
`provider.name`. Менять только после подтверждённого smoke
(`docs/SSH_CLI_MODELS.md`).

**Files-артефакты от LLM (ssh_cli):** при `adapter=ssh_cli` bridge
снимает diff `WORKSPACE` после CLI и возвращает `files[]` с base64;
adapter формирует Markdown `[📎 имя](data:mime;base64,...)`, UI
рендерит как download-chips в `MessageBubble`. Лимит — env
`AI_BRIDGE_FILES_MAX_*` на сервере.

## Персональные API-ключи (L)

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| GET | `/api/me/credentials` | any | Свои подключённые ключи (без plaintext) |
| POST | `/api/me/credentials` | any | Добавить личный ключ к провайдеру |
| DELETE | `/api/me/credentials/:id` | владелец | Отключить |

Personal key используется поверх org-ключа; fail-closed на ошибке
шифрования. trust_level/endpoint не меняются — masking-инвариант
сохранён.

## Служебное

| Метод | Путь | Сервис | Назначение |
|-------|------|--------|------------|
| GET | `/health` | rubezh-api | Liveness (docker HEALTHCHECK + `rubezh-api healthcheck` self-mode) |
| GET | `/live` | rubezh-worker | Liveness probe — без БД |
| GET | `/ready` | rubezh-worker | Readiness probe — БД + SELECT 1 (2s timeout); 503 при недоступности |
| GET | `/health` | rubezh-worker | Backward-compat alias /live |
| GET | `/health` | rubezh-sanitizer | Liveness |
| GET | `/metrics` | — | (пост-MVP, Prometheus exporter ещё не реализован) |

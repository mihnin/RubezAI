# REST API — Рубеж ИИ

Базовый префикс: `/api`. Формат — JSON, UTF-8.

**Аутентификация:** `Authorization: Bearer <token>` (MVP — dev-токен
`role.HMAC-SHA256`, см. `docs/design/identity.md §«MVP auth-flow»`;
после MVP — OIDC).

**Авторитетные контракты** — JSON Schema в `docs/contracts/`:

- `chat.schema.json` — чат и сессии.
- `policy.schema.json` — policy engine (вход и решение).
- `sanitize.schema.json` — обезличивание.

Если эта таблица расходится с .schema.json — **истина в schema**.

## Соглашения

- Идентификаторы — UUID v4.
- Временные метки — RFC 3339 (UTC).
- Ошибки — `{ "error": { "code": "string", "message": "string" } }`.
- Доступ по ролям указан в колонке «Роли».
- Статус «✅ реализован» / «🔄 итерация N» / «☐ запланирован».

## Аутентификация (MVP, dev-режим)

| Метод | Путь | Роли | Статус | Назначение |
|-------|------|------|--------|------------|
| POST | `/api/auth/dev-login` | — | 🔄 12 | Получить dev-токен по роли |
| POST | `/api/auth/logout` | any | 🔄 12 | Очистить серверную сессию (no-op в MVP, аудит-событие) |

`POST /api/auth/dev-login` тело:

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

Подробности — `docs/design/identity.md`.

## Чат

| Метод | Путь | Роли | Статус | Назначение |
|-------|------|------|--------|------------|
| POST | `/api/chat` | any | ✅ 8 | Отправить запрос; ответ — поток **SSE** |
| GET | `/api/chat/sessions` | any | ✅ 8 | Список сессий пользователя |
| POST | `/api/chat/sessions` | any | ✅ 8 | Создать пустую сессию |
| GET | `/api/chat/sessions/:id/messages` | владелец | 🔄 9 | История сообщений сессии (с превью обезличивания) |

`POST /api/chat` (тело) — см. `chat.schema.json#ChatRequest`:

```json
{
  "session_id": "uuid | null",
  "message": "string (1..16384)",
  "provider": "string",
  "model": "string?"
}
```

Ответ — поток **SSE** (`Content-Type: text/event-stream`):

```
event: meta   data: {"decision":"allow_masked","risk":{...},"provider":"...","reasons":[...],"request_id":"..."}
event: delta  data: {"content":"частичный ответ"}
event: delta  data: {"content":"..."}
event: done   data: {"request_id":"..."}
event: error  data: {"message":"...","request_id":"..."}
```

- `meta` — всегда первое; для `deny`/`escalate` после него идёт сразу
  `done` (без `delta`).
- `error` — терминальное; всегда содержит `request_id` (M2 правка
  Итерации 8, см. `chat.schema.json#SseError`).
- Подробности потока — `docs/design/iteration-8-chat.md`.

`GET /api/chat/sessions/:id/messages` — `chat.schema.json#ChatMessageList`.
Возвращает все сообщения сессии в порядке времени. `content` —
**псевдонимизированный** текст (см. iteration-8-chat.md §Р6: raw нигде
не персистируется). Поле `sanitization_summary` опционально и даёт
фронту минимум для отрисовки chip'ов псевдонимов без раскрытия raw.
До Итерации 9 поле может быть `null` для старых сообщений.

## Документы

| Метод | Путь | Роли | Статус | Назначение |
|-------|------|------|--------|------------|
| POST | `/api/documents` | any | ☐ 10 | Загрузить (multipart, ≤50 МБ, pdf/docx) |
| GET | `/api/documents` | any (свои), sec/audit/admin (все) | ☐ 10 | Список документов |
| GET | `/api/documents/:id` | acl | ☐ 10 | Метаданные, статус обработки |
| GET | `/api/documents/:id/chunks` | acl | ☐ 10 | Список chunks с masked-payload |
| GET | `/api/documents/:id/download` | acl + admin | ☐ 10 | Скачать оригинал (audit-event) |
| DELETE | `/api/documents/:id` | acl + admin | ☐ 10 | Soft-delete |

## Обезличивание

| Метод | Путь | Роли | Статус | Назначение |
|-------|------|------|--------|------------|
| POST | `/api/sanitize/preview` | any | ✅ 4 (внутр.) / 🔄 12 (внешн.) | Предпросмотр обезличивания |

Внешний `POST /api/sanitize/preview` (`rubezh-api`) — тонкий
аутентифицированный прокси к `POST /sanitize/preview` сервиса
`rubezh-sanitizer`. Внутренний эндпойнт реализован в Итерации 4;
внешний прокси добавляется в Итерации 12 для фронта.
Контракт — `docs/contracts/sanitize.schema.json`.

## Аудит

| Метод | Путь | Роли | Статус | Назначение |
|-------|------|------|--------|------------|
| GET | `/api/audit-events` | sec/comp/audit/admin | 🔄 9 | Журнал с фильтрами (период, актор, тип, решение, риск, leak-flag) |
| GET | `/api/audit-events/:id` | sec/comp/audit/admin | 🔄 9 | Полная запись для drawer |
| POST | `/api/audit-events/export` | sec/comp/audit/admin | 🔄 9 | Экспорт CSV/NDJSON; сам аудитируется |

`audit_events` — **append-only**, методов изменения/удаления нет
(триггер БД `rubezh_block_mutation`, миграция `000003`).

## Инциденты

| Метод | Путь | Роли | Статус | Назначение |
|-------|------|------|--------|------------|
| GET | `/api/incidents` | sec/comp/audit/admin | 🔄 9 | Список (фильтры: status, assignee, severity) |
| GET | `/api/incidents/:id` | sec/comp/audit/admin | 🔄 9 | Карточка расследования |
| PATCH | `/api/incidents/:id` | sec/admin | 🔄 9 | Изменить статус/assignee/severity |
| POST | `/api/incidents/:id/notes` | sec/admin (assignee) | 🔄 9 | Добавить заметку расследователя |

Авто-создание инцидента — при `deny` / `escalate` /
`response_leak_detected: true` в `audit_events.detail`. Удаления
инцидентов нет (только статусы `resolved` / `false_positive`).

## Политики

| Метод | Путь | Роли | Статус | Назначение |
|-------|------|------|--------|------------|
| GET | `/api/policies` | sec/comp/admin | ✅ 6 | Список действующих политик |
| GET | `/api/policies/:id` | sec/comp/admin | ✅ 6 | Детали политики |
| POST | `/api/policies` | sec/admin | ✅ 6 | Создать политику (новая версия) |
| POST | `/api/policies/test` | sec/comp/admin | ✅ 6 | Прогнать политику на примере (внутри: sanitize → policy) |

Контракт решения — `docs/contracts/policy.schema.json#PolicyDecision`.

Тест-эндпойнт внутри выполняет `sanitize` входного текста, затем
строит `PolicyInput` и вызывает policy engine. Это означает, что
`risk` и `entity_types` вычисляются автоматически — пользователь
передаёт только `text`, `model_trust`, `user_role`, `context`.
Тест **пишет audit-event** `policy_tested`.

## Модели

| Метод | Путь | Роли | Статус | Назначение |
|-------|------|------|--------|------------|
| GET | `/api/models` | any | ✅ 7 | Список провайдеров: тип, trust_level, endpoint, is_enabled |
| POST | `/api/models` | admin | ✅ 7 | Добавить провайдера (api_key зашифрован, не возвращается) |
| PATCH | `/api/models/:id` | admin | ✅ 7 | Включить/выключить, переименовать |
| DELETE | `/api/models/:id` | admin | ✅ 7 | Удалить (soft) |

Поле `api_key` в GET-ответах **никогда не возвращается** (масcкируется
до `••••` или флага наличия). Чтобы заменить ключ — отправить заново
в PATCH.

## Служебное

| Метод | Путь | Роли | Статус | Назначение |
|-------|------|------|--------|------------|
| GET | `/health` | — | ✅ 5 | Liveness/readiness |

## Что меняется в Итерации 9 (резюме)

1. **Расширение `chat.schema.json#SseError`** полем `request_id`
   (ретро-правка Итерации 8 — см. M2 ревью этапа A; коррелятор
   обязателен в любом терминальном событии).
2. **Расширение `chat.schema.json#SseMeta`** полем `request_id`
   (для возможности сообщить id ещё до получения `done`/`error`).
3. **Новый `chat.schema.json#ChatMessage` / `ChatMessageList`**
   с опциональным `sanitization_summary` — для рендера истории сессии.
4. **`GET /api/chat/sessions/:id/messages`** — реализация выше
   контракта.
5. **`GET /api/audit-events`**, `GET /api/audit-events/:id`,
   `POST /api/audit-events/export`.
6. **`GET /api/incidents`**, `GET /api/incidents/:id`,
   `PATCH /api/incidents/:id`, `POST /api/incidents/:id/notes`.
7. **Шифрованная персистентность `pseudonym_mappings`** (AES-GCM,
   `MAPPING_ENCRYPTION_KEY`) — закрывает критерий восстановления
   raw-значений только при наличии ключа (forensics-функция).

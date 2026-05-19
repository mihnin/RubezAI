# REST API — Рубеж ИИ

Базовый префикс: `/api`. Формат — JSON, UTF-8. Аутентификация — заголовок
`Authorization: Bearer <token>` (MVP: dev-токен с claim роли; позже — OIDC).

Статус: проектные контракты MVP. Тела запросов/ответов уточняются в итерациях
реализации соответствующих эндпойнтов. Межсервисные контракты Go↔Python —
в `docs/contracts/`.

## Соглашения

- Идентификаторы — UUID v4.
- Временные метки — RFC 3339 (UTC).
- Ошибки — `{ "error": { "code": "string", "message": "string" } }`.
- Доступ по ролям указан в колонке «Роли».

## Чат

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| POST | `/api/chat` | user, и выше | Отправить запрос; ответ — поток **SSE** |
| GET | `/api/chat/sessions` | user, и выше | Список сессий чата пользователя |

`POST /api/chat` (тело):

```json
{
  "session_id": "uuid | null",
  "message": "string",
  "model_provider_id": "uuid",
  "document_ids": ["uuid"]
}
```

Ответ — поток **SSE** (`Content-Type: text/event-stream`):

```
event: policy        data: {"decision":"allow_masked","reasons":["..."]}
event: token         data: {"text":"частичный ответ"}
event: token         data: {"text":"..."}
event: done          data: {"audit_event_id":"uuid","incident_id":null}
event: error         data: {"code":"string","message":"string"}
```

При решении `deny` поток содержит `event: policy` (decision=deny) и `event: done`
со ссылкой на созданный `incident_id`; токены ответа не отправляются.

## Документы

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| POST | `/api/documents` | user, и выше | Загрузить документ (`multipart/form-data`) |
| GET | `/api/documents/:id` | user (свои), security_officer, auditor, admin | Метаданные, статус обработки, найденные сущности, ссылки на audit events |

## Обезличивание

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| POST | `/api/sanitize/preview` | user, и выше | Предпросмотр обезличивания текста |

Внешний эндпойнт `POST /api/sanitize/preview` (`rubezh-api`) — тонкий
аутентифицированный прокси к внутреннему `POST /sanitize/preview` сервиса
`rubezh-sanitizer`: тела запроса и ответа идентичны. Единый контракт обоих —
`docs/contracts/sanitize.schema.json`.

## Аудит

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| GET | `/api/audit-events` | security_officer, compliance_officer, auditor, admin | Журнал аудита с фильтрами (пользователь, дата, модель, риск, решение) |

`audit_events` — **append-only**, методов изменения/удаления нет.

## Инциденты

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| GET | `/api/incidents` | security_officer, compliance_officer, auditor, admin | Список блокировок и нарушений |
| PATCH | `/api/incidents/:id` | security_officer, admin | Изменить статус: `open → investigating → resolved \| false_positive` |

## Политики

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| GET | `/api/policies` | security_officer, compliance_officer, admin | Список политик |
| POST | `/api/policies` | security_officer, admin | Создать политику (новая версия) |
| POST | `/api/policies/test` | security_officer, compliance_officer, admin | Прогнать политику на примере запроса |

Контракт решения — `docs/contracts/policy.schema.json`.

## Модели

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| GET | `/api/models` | user, и выше | Список провайдеров: тип доверия, endpoint, лимиты |
| POST | `/api/models` | admin | Добавить/обновить провайдера модели |

## Служебное

| Метод | Путь | Роли | Назначение |
|-------|------|------|------------|
| GET | `/health` | — | Healthcheck сервиса (liveness/readiness) |

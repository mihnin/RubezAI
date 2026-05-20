# Архитектурное решение: идентичность и ролевая модель

Статус: принято (архитектор), 2026-05-19. Тип: ADR-заметка.

## Контекст

«Рубеж ИИ» — on-prem контур защиты ПДн. Нужна аутентификация пользователей
и ролевая модель (6 ролей: `user`, `security_officer`, `compliance_officer`,
`admin`, `auditor`, `developer`). Рассматривался вопрос: использовать ли
Supabase / отдельный IdP, и какой именно.

## Решение

1. **MVP** — собственный dev-токен `role.HMAC-SHA256` (`internal/auth`),
   роли и пользователи — в PostgreSQL (`roles`, `users`, миграции `000002`,
   `000007`). Идентичность резолвится по роли (`UserIDForRole`). Достаточно
   для критериев MVP и демо. Никакого внешнего сервиса.

2. **После MVP** — `rubezh-api` становится **стандартным OIDC Relying
   Party (RP)**. Точка замены — auth-middleware; формат токена и таблица
   `users` уже спроектированы под это («заменяется на OIDC»).

3. **Не привязываемся к одному IdP.** Поскольку OIDC — стандарт, шлюзу
   достаточно быть RP:
   - заказчик **со своим корпоративным IdP** (AD/ADFS, Avanpost, Blitz
     Identity, Solar inRights, …) подключается через конфиг — issuer URL,
     client_id/secret, claim-маппинг;
   - заказчик **без IdP** получает в поставке **Keycloak** как
     опциональный reference-IdP (open-source Apache 2.0, self-hosted —
     on-prem сохраняется, SaaS не вводится).

4. Маппинг OIDC-claims (group/role) → 6 ролей проекта — конфигурируемый.

## Почему не Supabase

Supabase (даже self-hosted) — это BaaS-стек (GoTrue, PostgREST/Kong,
Realtime, Studio) ради задачи, которую закрывают уже имеющийся PostgreSQL
плюс одна OIDC-интеграция. Облачная Supabase к тому же выносит идентичность
во внешний SaaS — недопустимо для on-prem контура защиты ПДн (152-ФЗ).
Принцип проекта — без лишней инфраструктуры — соблюдается.

## Последствия

- MVP-код идентичности (`internal/auth`, `UserIDForRole`) — временный, с
  явной точкой расширения; межпользовательская изоляция сессий появляется
  вместе с OIDC (см. `THREAT_MODEL.md`).
- Интеграция OIDC — отдельная итерация после ядра MVP; в `docs/PLAN.md`
  планируется как пост-MVP задача.

## MVP auth-flow для Frontend (этап A, для Итерации 12)

Чтобы Итерация 12 (frontend-каркас) шла по непротиворечивому пути,
зафиксирован конкретный flow получения и хранения dev-токена.

### Решение

| Аспект | MVP | После MVP (OIDC) |
|--------|-----|-------------------|
| Хранение токена | `localStorage` ключ `rubezh.auth.token` | httpOnly SecureCookie + CSRF-токен |
| Передача в запросе | `Authorization: Bearer <token>` | `Authorization: Bearer` или Cookie |
| Получение токена | `POST /api/auth/dev-login` (см. ниже) | OIDC redirect-flow (authorization code + PKCE) |
| Logout | `localStorage.removeItem` + redirect `/login` | `POST /api/auth/logout` + invalidate session |

### Аргументация выбора

1. **httpOnly cookie + Bearer middleware несовместимы** без введения
   CSRF-токена (двойная отправка) или полной перезаписи auth-middleware
   на cookie-based. Это значительная работа на бэке ради временного
   решения.
2. **localStorage + Bearer** — стандартный паттерн для dev-инструментов,
   совместим с уже реализованным middleware (`internal/auth`), не
   требует новых таблиц или CSRF-токенов.
3. **XSS-риск localStorage** — присутствует, но MVP «Рубежа ИИ» —
   on-prem внутренний контур; внешнего периметра нет. После OIDC риск
   снимается переходом на httpOnly cookie.
4. **Точка замены — одна.** Auth-middleware читает токен из header
   `Authorization: Bearer`. Когда придёт OIDC, middleware начнёт читать
   ID-токен либо access-token из cookie/header — это локальная правка,
   фронт `Authorization`-схему **не меняет** до перехода (но получит
   токен из OIDC RP вместо `/dev-login`).

### Эндпойнт `POST /api/auth/dev-login`

Реализуется в **Итерации 12** одновременно с frontend-каркасом:

```http
POST /api/auth/dev-login
Content-Type: application/json

{ "role": "user" }
```

Ответ:

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "token": "user.a4f3...b9c2",
  "role": "user",
  "user_id": "uuid",
  "expires_at": "2026-05-21T14:32:00Z"
}
```

Поведение:

- Сервер вызывает `auth.IssueToken(role, secret)` (уже существующая
  библиотечная функция).
- `user_id` — резолвится через `UserIDForRole` (миграция `000007`
  засеивает по одному dev-пользователю на роль).
- `expires_at` — current time + `AUTH_TOKEN_TTL` (env, дефолт 24h).
  В MVP токен формата `role.HMAC-SHA256` **не** содержит exp; поле
  передаётся для UI-удобства (когда фронт авто-logout'нёт пользователя).
  Это **не криптографическая проверка** — она остаётся на стороне сервера.

Эндпойнт зарегистрирован **только в dev-режиме** (`config.DevMode` —
новая опция; по умолчанию `true` в MVP, `false` после OIDC).

### Audit-event

Каждый успешный login пишет `audit_event` `auth_login` с `user_id`,
`role`, `request_id`. Failure → `auth_login_failed` (если такой случай
вообще возможен в dev-режиме).

### CSRF

В MVP не требуется (Bearer из header не подвержен CSRF). Появится с
переходом на cookie после MVP.

### Что произойдёт с OIDC-переходом

Frontend изменит **только** страницу `/login` (redirect на OIDC IdP
вместо формы выбора роли). Все остальные экраны (Chat, Documents…)
продолжат отправлять `Authorization: Bearer <token>` без правок —
токен будет получен из OIDC RP вместо `/dev-login`. Если OIDC введёт
httpOnly cookie, фронт перестанет вручную добавлять header, но это
локальное изменение в `apiClient`.

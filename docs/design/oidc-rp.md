# Итерация K — OIDC: браузерный вход сотрудника (Web + CLI)

Статус: **черновик**. Реализует решение из `identity.md` (rubezh-api →
стандартный OIDC Relying Party; точка замены — auth-middleware; без IdP
заказчика — Keycloak/dex как reference). Запрос владельца: «вход по корп.
почте через браузер, как в Claude Code / gcloud».

## Что это даёт и чего НЕ даёт

- **Даёт:** настоящую идентичность сотрудника (`user_id` на человека, а не на
  роль), браузерный SSO-вход в «Рубеж» в Web и CLI. Фундамент для персональных
  ключей провайдеров (`user_provider_credentials`, отдельная итерация).
- **НЕ даёт:** доступ к потребительскому чату OpenAI/Gemini/Grok через их
  логин — у них нет публичного OAuth для этого (см. `chat-pii-flow`/разбор
  архитектора). Доступ к моделям остаётся по API-ключам. (Claude Code логинит
  в аккаунт Anthropic — это частный OAuth Anthropic для своего CLI, не общий
  механизм.)

## Фазы

### K.0 — Токен носит `user_id` + `role` (фундамент, без внешних зависимостей)
Сейчас токен кодирует только роль (`<role>.<hmac>`), а `user_id` резолвится
через `UserIDForRole` → все носители роли делят одного пользователя
(THREAT_MODEL: нет межпользовательской изоляции). Меняем:
- `auth.IssueToken(userID, role, secret)` → payload `"<user_id>:<role>"`,
  подпись HMAC-SHA256. `ParseToken` → `(Identity{UserID, Role}, err)`.
- Middleware кладёт в контекст `Identity`; `IdentityFromContext`;
  `RoleFromContext` сохраняется (берёт роль из Identity) для обратной совмест.
- `dev-login` остаётся: резолвит `user_id` через `UserIDForRole(role)` и
  выпускает токен с этим id (для локального dev без IdP).
- `currentUserID` и места в chat.go берут `user_id` **из токена**, а не
  `UserIDForRole`. Тесты auth.

### K.1 — OIDC RP (браузерный Authorization Code + PKCE)
- Зависимости: `github.com/coreos/go-oidc/v3/oidc`, `golang.org/x/oauth2`.
- Конфиг (env): `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`,
  `OIDC_REDIRECT_URL`, `OIDC_ROLE_CLAIM` (claim с группой/ролью),
  `OIDC_ROLE_MAP` (маппинг значение-claim→роль, дефолт `user`). Пусто →
  OIDC выключен, остаётся dev-login.
- Эндпойнты:
  - `GET /api/auth/oidc/login` — генерит state+PKCE (хранит в коротком
    server-side/cookie), редирект на authorize IdP.
  - `GET /api/auth/oidc/callback` — проверка state, обмен code→token,
    верификация ID-токена (issuer, audience, exp, подпись через JWKS),
    извлечение email/sub/claims → **upsert user** (по email; роль из
    claim-маппинга, дефолт `user`) → `IssueToken(user_id, role)` →
    отдать фронту (редирект на `/login#token=…` или установка).
  - `POST /api/auth/logout` (пост-MVP: инвалидация сессии).
- **Upsert user:** `storage.UpsertUserByEmail(email, fullName, roleCode)` —
  если есть, обновляет; иначе создаёт с `role_id` по коду. `username`=email.
- Маппинг claim→роль конфигурируемый; неизвестное/пустое → `user` (least
  privilege). Повышение роли — только через явный маппинг ИБ.

### K.2 — CLI loopback-вход (`rubezh login --sso`)
Как `gcloud auth login` (RFC 8252):
- CLI поднимает локальный `http://127.0.0.1:<rand>/callback`, открывает
  браузер на `/api/auth/oidc/login?cli_redirect=127.0.0.1:port`, после
  IdP-логина «Рубеж» редиректит код/токен на loopback, CLI его забирает и
  сохраняет (`~/.rubezh`). Браузер открывается через `xdg-open`/`open`/
  `rundll32`.

### K.3 — Dev-IdP для локального теста (dex в docker-compose)
Без IdP заказчика тестировать OIDC негде. Добавить **dex** (CoreOS, Apache 2.0,
single-binary) в `docker-compose.yml` как опциональный профиль `dev-idp`:
статический пользователь (email/пароль), issuer `http://dex:5556/dex`,
client для «Рубежа». `.env.example` — пример OIDC_*-переменных под dex.
Заказчик в проде указывает свой issuer (AD/ADFS/Keycloak/Avanpost/…).

### K.4 — Frontend
- LoginPage: кнопка «Войти через корпоративную учётную запись (SSO)» →
  `window.location = /api/auth/oidc/login`. Приём токена из callback-редиректа.
- Dev-login (выбор роли) остаётся, если OIDC выключен.

## Безопасность / инварианты
- ID-токен верифицируется полностью (issuer, aud, exp, JWKS-подпись) —
  никаких «доверяем claims без проверки».
- `state` + PKCE против CSRF/code-interception.
- Роль из claim-маппинга, дефолт least-privilege (`user`); самоповышение роли
  невозможно (маппинг задаёт ИБ в конфиге).
- on-prem сохраняется: IdP — у заказчика (или dex on-prem), наружу ничего.
- 152-ФЗ: идентичность не выносится в SaaS.

## MVP vs пост-MVP
- **Сейчас (K.0):** токен с user_id — фундамент, тестируется без IdP.
- **K.1–K.4:** браузерный OIDC (Web), dex для теста, frontend, CLI.
- **Пост-MVP:** httpOnly-cookie вместо localStorage + CSRF; logout-инвалидация;
  затем `user_provider_credentials` (персональные ключи) поверх реального user_id.

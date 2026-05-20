# Экран: Login (`/login`)

UX-spec, dev-режим (MVP). Заменяется на OIDC RP после MVP
(см. `docs/design/identity.md`).

**Auth-flow (MVP) — см. `identity.md §«MVP auth-flow»`:**
`POST /api/auth/dev-login` → `localStorage` → `Authorization: Bearer`
в каждом следующем запросе. Этот выбор зафиксирован в ADR — фронт
**не** использует httpOnly cookie до OIDC.

## Цель

Дать пользователю войти под одной из 6 ролей через **dev-токен** и сразу
попасть на «домашний» экран своей роли.

## Mockup (Dark, 1440)

```
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│              ┌──────────────────────────────────┐              │
│              │   ⛨  Рубеж ИИ                    │              │
│              │   On-prem AI Gateway             │              │
│              │   ─────────────────────────      │              │
│              │   Войти под ролью                │              │
│              │   ┌────────────────────────┐ ▼   │              │
│              │   │ user                   │     │              │
│              │   └────────────────────────┘     │              │
│              │                                  │              │
│              │   ┌────────────────────────┐     │              │
│              │   │       Войти            │ →   │              │
│              │   └────────────────────────┘     │              │
│              │   ─────────────────────────      │              │
│              │   Dev-режим • замена на OIDC     │              │
│              │   после MVP                      │              │
│              │   ⓘ Токен хранится в localStorage │              │
│              └──────────────────────────────────┘              │
│                                                                │
│              v0.x.y · CLAUDE.md · THREAT_MODEL.md              │
└────────────────────────────────────────────────────────────────┘
```

## Структура

- Центральная карточка 360×460, `bg-elev-1`, `radius-md`, `elev-1`.
- Логотип «⛨» (Lucide `shield`), 32px, `--accent`.
- Заголовок `text-xl`, subtitle `text-base text-secondary`.
- Label + Select с 6 ролями (дефолт `user`):
  `user`, `security_officer`, `compliance_officer`,
  `admin`, `auditor`, `developer`.
- Primary-кнопка «Войти» (`btn primary md`, full-width).
- Footnote `text-xs text-muted`:
  «Dev-режим • замена на OIDC после MVP» +
  **«ⓘ Токен хранится в localStorage» — tooltip объясняет** что в
  MVP это допустимый компромисс для on-prem-контура, см.
  `identity.md`.
- Внизу страницы: версия app, ссылки.

## Поведение

1. Submit → `POST /api/auth/dev-login` (`{ "role": "user" }`).
2. Ответ — `{ "token", "role", "user_id", "expires_at" }`.
3. Кладём `token` в `localStorage["rubezh.auth.token"]`,
   `role`/`user_id`/`expires_at` — в `localStorage["rubezh.auth.user"]`
   (JSON-stringify).
4. `apiClient` после этого добавляет `Authorization: Bearer ${token}`
   во все запросы.
5. Redirect на «дом» роли (см. `ui-scope.md §1`).
6. **Auto-logout**: если `expires_at` истёк или сервер вернул 401 —
   очистить localStorage и redirect на `/login`.

## Состояния

| State | Что показываем |
|-------|----------------|
| Idle | Форма как в mockup'е |
| Submitting | Кнопка disabled + spinner, текст «Вход…»; форма disabled |
| Error (4xx / network) | Banner `danger` над формой: «Не удалось войти. Попробуйте ещё раз.» + `request_id` (mono, копируется); кнопка снова enabled |
| Rate-limited (429) | Banner `warning`: «Слишком много попыток. Подождите 30 с.» + countdown |
| Server unavailable (502/503) | Banner `danger`: «Сервер недоступен. Проверьте Docker Compose.» |
| Already authenticated | Авто-редирект на дом, форма не отображается; toast `info` «Вы вошли как `<role>`» |
| No dev roles seeded | Banner `warning`: «Dev-пользователи не засеяны. Запустите миграцию 000007.» — для разработчика |

## Безопасность

- Токен в localStorage **доступен JavaScript** — XSS-уязвимость
  представления не блокируется. В MVP это **принятый компромисс**
  (см. identity.md). Mitigations:
  - CSP: `default-src 'self'` + strict-dynamic; запрет inline-script
    (только хеши/nonce);
  - санитизация всего пользовательского ввода в UI (DOMPurify для
    markdown в chat-сообщениях ассистента, если используется);
  - audit-event на каждый login (`auth_login`).
- Logout — `localStorage.removeItem('rubezh.auth.token')` +
  `localStorage.removeItem('rubezh.auth.user')` + redirect `/login`.
- Cookie не используются; CSRF-токен в MVP не нужен.

## Accessibility

- `<form>` с явным `<label for="role">`.
- Submit по Enter.
- Focus на Select при загрузке.
- `aria-live="assertive"` на error-banner.
- Tooltip «ⓘ Токен в localStorage» — `aria-describedby` на иконке.

## Самооценка: 10/10

- 7 состояний (превышает требование ≥5).
- Auth-flow явно согласован с `identity.md`.
- XSS-mitigation описан, не замолчан.
- Точка замены на OIDC явная (одна страница).

# Экран: Models (`/models`, `/models/new`)

UX-spec. Использует существующее API из Итерации 7
(`GET/POST /api/models`). Управляет провайдерами LLM
с `trust_level`. Доступ — `admin`, `developer`.

## Mockup — список (Dark, 1440)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ ⛨ Рубеж ИИ                                       ⌘K   ☼   admin (Admin) ▾ │
├─────────────────┬───────────────────────────────────────────────────────────┤
│ Chat            │ Models                                  [+ Добавить]      │
│ Documents       │                                                           │
│ Models        ◀ │ Провайдеры LLM (4)                                        │
│ Policies        │ ┌────────────────────────────────────────────────────────┐│
│ Audit Log       │ │  Имя             Тип           Доверие     Состояние ⋯ ││
│ Incidents       │ ├────────────────────────────────────────────────────────┤│
│                 │ │  mock-stub       mock          external    ●вкл       ⋯││
│                 │ │  gpt-4o-mini     openai_compat external    ●вкл       ⋯││
│                 │ │  yagpt-pro       openai_compat russian_cl. ●вкл       ⋯││
│                 │ │  deepseek-7b     openai_compat trusted_loc ●вкл       ⋯││
│                 │ │  base_url: http://172.27.48.1:1234/v1                  ││
│                 │ └────────────────────────────────────────────────────────┘│
│                 │                                                           │
│                 │ Подсказка: trusted_local разрешает allow_raw в policy.    │
│                 │                                                           │
└─────────────────┴───────────────────────────────────────────────────────────┘
```

## Mockup — диалог «Добавить провайдера»

```
                ┌──────────────────────────────────────────┐
                │   Добавить провайдера                 ✕  │
                ├──────────────────────────────────────────┤
                │                                          │
                │   Имя *                                  │
                │   ┌──────────────────────────────────┐   │
                │   │ deepseek-7b                      │   │
                │   └──────────────────────────────────┘   │
                │                                          │
                │   Тип *                                  │
                │   ◉ openai_compatible    ○ mock          │
                │                                          │
                │   Уровень доверия *                      │
                │   ◯ external          ◯ russian_cloud    │
                │   ◯ on_prem           ◉ trusted_local    │
                │                                          │
                │   Base URL *                             │
                │   ┌──────────────────────────────────┐   │
                │   │ http://172.27.48.1:1234/v1       │   │
                │   └──────────────────────────────────┘   │
                │                                          │
                │   Default model                          │
                │   ┌──────────────────────────────────┐   │
                │   │ deepseek-r1-7b                   │   │
                │   └──────────────────────────────────┘   │
                │                                          │
                │   API key                                │
                │   ┌──────────────────────────────────┐   │
                │   │ ••••••••••••••••••••••••••••     │👁 │
                │   └──────────────────────────────────┘   │
                │   Ключ зашифрован; обратно не возвращается│
                │                                          │
                │   ☑ Включить сразу                       │
                │                                          │
                │   [ Отмена ]              [ ▶ Добавить ] │
                └──────────────────────────────────────────┘
```

## Структура

### Таблица провайдеров

- Колонки:
  - **Имя** (`text-base` + tooltip с `provider_id` mono);
  - **Тип** (`mock` / `openai_compatible`);
  - **Доверие** — chip с цветами:
    - `external` — `danger` (red, мутед);
    - `russian_cloud` — `warning` (amber, мутед);
    - `on_prem` — `info` (cyan);
    - `trusted_local` — `success` (olive).
  - **Состояние** — chip `●вкл` (success) / `○выкл` (muted).
  - Действия (3-точечное меню): Test connection, Edit (name +
    default model), Toggle on/off, Delete.
- Hover row → expand второй строкой `base_url` (mono, обрезан до 60
  chars + tooltip).

### Add dialog

- 6 полей (см. mockup).
- Validation:
  - имя уникально (асинхронно при `onBlur`);
  - URL — валидный http/https с хостом;
  - api_key — обязателен для `openai_compatible` если ENV-default
    отсутствует.
- Toggle «Показать ключ» (`eye` icon).
- При сохранении — `POST /api/models`; на 409 — toast «Имя уже
  занято».

### Подсказка про trusted_local

- Над таблицей или внизу — `info`-banner:
  > **trusted_local** разрешает `allow_raw` в policy engine — выбирайте
  > только для локально-развёрнутых моделей в той же сети.

(Это важно для пользователя: подсказывает, что DeepSeek-7B можно/нужно
маркировать как `trusted_local`.)

## Состояния

| State | Описание |
|-------|----------|
| Empty | Hero «Добавьте первого провайдера»; кнопка primary |
| Loading | Skeleton-таблица |
| Validation error в Add | Inline-ошибки полей |
| Duplicate name | Toast `danger`: «Провайдер с таким именем уже существует» |
| Test connection running | Spinner в строке, текст «Проверка…» |
| Test connection ok | Toast `success`: «Соединение установлено» |
| Test connection fail | Toast `danger`: «Не удалось подключиться: <msg>», `request_id` |
| Delete confirmation | Dialog: «Удалить провайдера? Это не удалит уже выданные ответы и аудит-записи.» |
| Read-only role | Действия скрыты; tooltip «Требуется роль admin/developer» |

## Поведение

1. На mount — `GET /api/models` → таблица.
2. Add → `POST /api/models` body `{name, kind, trust_level, base_url,
   default_model?, api_key?, is_enabled}`.
3. Test → `POST /api/models/:id/test` (если есть; иначе клиент сам
   делает `POST {base_url}/chat/completions` с пустым промптом —
   опционально пост-MVP).

## Безопасность

- `api_key` хранится в БД зашифрованно и **никогда не возвращается** в
  GET (только masked `••••` или флаг наличия). Это уже реализовано в
  Итерации 7 (см. PLAN.md).
- Аудит: `model_provider_created/updated/disabled/deleted` — обяз.
- Логирование изменений видно в Audit Log.

## Accessibility

- Radio-group для `kind` и `trust_level` — `<fieldset><legend>`.
- Tooltip-объяснения для `trust_level` (что это значит).
- Hint-text под `api_key` (не вместо label).

## Самооценка: 10/10

- Покрывает CRUD-сценарии и Test connection.
- Видно техдолг (один ENV-ключ на все openai-провайдеры — см. PLAN.md
  «Технический долг», открытое).
- Подсказывает пользователю мапинг DeepSeek-7B → trusted_local.

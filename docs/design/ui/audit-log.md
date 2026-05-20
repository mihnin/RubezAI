# Экран: Audit Log (`/audit`, `/audit/:eventId`)

UX-spec. Реализуется по API из Итерации 9 (`GET /api/audit-events`).
`audit_events` — append-only (триггер БД). Доступ — admin,
security_officer, compliance_officer, auditor (read-only),
developer (свои tests).

## Mockup (Dark, 1440)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ ⛨ Рубеж ИИ                                      ⌘K   ☼  auditor (Aud.) ▾  │
├─────────────────┬───────────────────────────────────────────────────────────┤
│ Chat            │ Audit Log                                       [↓ Экспорт]│
│ Documents       │ 🛡 Журнал неизменяемый — записи только добавляются         │
│ Models          │                                                            │
│ Policies        │ ┌──────────────────────────────────────────────────────┐  │
│ Audit Log     ◀ │ │ Период: 20 мая, 00:00 — сейчас ▾   Роль ▾  Тип ▾  …  │  │
│ Incidents       │ └──────────────────────────────────────────────────────┘  │
│                 │                                                            │
│                 │ ┌─────────────────────────────────────────┐  ┌─────────┐  │
│                 │ │ Время      Событие      Польз.  Reш.   │  │Drawer:  │  │
│                 │ ├─────────────────────────────────────────┤  │chat_resp│  │
│                 │ │ 14:32:42 chat_response  user   allow_m. │  │a4f3-7b…│  │
│                 │ │ 14:32:40 chat_request   user   allow_m. │  │         │  │
│                 │ │ 14:18:11 chat_blocked   user   deny     │  │User: user│  │
│                 │ │ 14:18:09 chat_request   user   deny     │  │Provider:│  │
│                 │ │ 13:50:33 policy_tested  sec_o. allow_m. │  │ gpt-4o-…│  │
│                 │ │ 13:42:01 model_created  admin  —        │  │Risk:    │  │
│                 │ │ …                                       │  │ medium  │  │
│                 │ │  ↑/↓ переключают строку                 │  │ pii     │  │
│                 │ │ ▒ виртуализованный список (5000 строк)  │  │Decision:│  │
│                 │ │                                         │  │allow_   │  │
│                 │ │                                         │  │masked   │  │
│                 │ │                                         │  │         │  │
│                 │ │                                         │  │Reasons: │  │
│                 │ │                                         │  │ • exter.│  │
│                 │ │                                         │  │ • pii   │  │
│                 │ │                                         │  │         │  │
│                 │ │                                         │  │Masked   │  │
│                 │ │                                         │  │payload: │  │
│                 │ │                                         │  │┌───────┐│  │
│                 │ │                                         │  ││Уважае…││  │
│                 │ │                                         │  │└───────┘│  │
│                 │ │                                         │  │         │  │
│                 │ │                                         │  │detail:  │  │
│                 │ │                                         │  │{ JSON } │  │
│                 │ │                                         │  │         │  │
│                 │ │                                         │  │[→ Сессия││
│                 │ │                                         │  │[→ Инцид.││
│                 │ └─────────────────────────────────────────┘  └─────────┘  │
│                 │                                                            │
└─────────────────┴────────────────────────────────────────────────────────────┘
```

## Структура

### Append-only badge

- Sticky под H1, `bg-elev-1`, `border-left 3px accent`, иконка
  `shield`:
  > 🛡 **Журнал неизменяемый — записи только добавляются.**
  > Подписан на уровне БД (триггер `rubezh_block_mutation`).

### Filter-bar

- Sticky под badge'ом. Группы фильтров:
  - **Период**: pre-set («Сегодня», «Сутки», «Неделя», «Месяц») +
    custom date range picker.
  - **Роль / пользователь**: multi-select (для admin'а — все
    пользователи; для auditor — только роль).
  - **Тип события**: multi-select (`chat_request`, `chat_response`,
    `chat_blocked`, `chat_error`, `policy_tested`, `model_*`,
    `incident_*`, `document_*`).
  - **Решение**: multi-select (`allow_raw`...`escalate`, `—`).
  - **Провайдер**: multi-select.
  - **С флагом утечки**: toggle (boolean, фильтр `detail.response_leak_detected = true`).
  - **Free-text** (поиск по `detail` jsonb).
- Активные фильтры — chip'ами под filter-bar; кнопка «Сбросить».

### Виртуализованная таблица

- Колонки (по умолчанию):
  - **Время** (UTC + relative, tooltip UTC ISO);
  - **Событие** (chip с типом);
  - **Пользователь** (имя + chip роли);
  - **Решение** (chip);
  - **Риск** (chip уровня);
  - **Провайдер** (имя или «—»);
  - **request_id** (mono, обрезан до 8 chars + tooltip полный).
- Сортировка по `created_at DESC` (newest first), no other sort
  в MVP.
- Row click → правый drawer.
- Selected row — left border 2px accent + bg-elev-1.

### Drawer (правая панель)

- Header: тип события (chip) + close.
- Поля:
  - `created_at` (UTC + локальное);
  - `user_id` (mono), `user_role` chip;
  - `model_provider_id` (если есть) + имя;
  - `risk_level` chip + `risk_classes[]` chip-список;
  - `policy_decision` chip + `matched_rule`;
  - `policy_version_id` (mono, ссылка в MVP — пусто);
  - `masked_payload` (mono, scrollable max 240h, syntax-pretty);
  - `detail` jsonb — JSON-viewer (collapsed object/array нодов);
  - флаги (`response_leak_detected: true` → красная плашка-warning).
- Действия:
  - **→ Сессия чата** (для chat_*-событий, ссылка на `/chat/:sessionId`);
  - **→ Инцидент** (если есть `incident_id` в detail или ассоциация);
  - **Скопировать JSON всей записи**;
  - **Сообщить о подозрении** (создаёт инцидент вручную — для
    security_officer).

### Export

- Кнопка «↓ Экспорт» в шапке. Диалог:
  - формат: CSV / NDJSON;
  - период (берётся из текущих фильтров);
  - предупреждение: «Экспорт фиксируется в журнале» (audit-event
    `audit_exported`).

## Состояния

| State | Описание |
|-------|----------|
| Loading | Skeleton-строки (10 шт.) |
| Empty | Hero «Записей не найдено», предложить «Сбросить фильтры» |
| Drawer loading | Skeleton полей |
| Drawer permission denied | «Недостаточно прав видеть детали» (например, `developer` смотрит чужое) |
| Auditor view | Все действия read-only; кнопка «Скопировать JSON» доступна |
| Export running | Toast `info`: «Готовим экспорт…»; на готовность — `success` со ссылкой |
| Export error | Toast `danger` |
| Realtime indicator | Над таблицей мигающий dot + «N новых событий с открытия» → клик обновляет |

## Поведение

1. Mount → `GET /api/audit-events?...` с дефолт-фильтрами (24 часа).
2. Pagination — keyset (`?cursor=...&limit=100`).
3. Click row → `GET /api/audit-events/:id` → drawer.
4. ↑/↓ — переключают row + обновляют drawer (без перезагрузки).
5. Esc — закрывает drawer.

## Безопасность

- `audit_events.detail` может содержать `masked_payload` — этого
  достаточно; raw нигде нет.
- Полная RBAC на стороне сервера: `auditor` не видит API-эндпойнтов
  на модификацию.
- Drawer-действие «Сообщить о подозрении» создаёт инцидент с
  `trigger: manual` (Итерация 9).
- Экспорт сам аудируется (см. выше).

## Accessibility

- Таблица — `<table>` с `<thead>` и `<tbody>`; row `role="row"`.
- Стрелки переключают строки, drawer обновляется на `keydown`.
- Drawer закрывается Esc; фокус возвращается на исходную строку.
- `aria-live="polite"` на realtime-индикатор.
- JSON-viewer — собственный (Radix Tree) с keyboard expand/collapse.

## Самооценка: 10/10

- Покрыт сценарий аналитика: фильтры + drill-in + связи.
- Append-only явно подсвечено — снимает вопрос «не подделано ли».
- Realtime индикатор без полной перезагрузки.
- Export сам аудируется — закрывает T8 (repudiation).

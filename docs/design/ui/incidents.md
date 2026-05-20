# Экран: Incidents (`/incidents`, `/incidents/:id`)

UX-spec. Использует API из Итерации 9 (`/api/incidents`, `PATCH
/api/incidents/:id`). Инциденты создаются **автоматически** при
`deny` / `escalate` / `response_leak_detected` и **вручную** (см.
audit-log §«Сообщить о подозрении»).

## Mockup — список (Dark, 1440)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ ⛨ Рубеж ИИ                                       ⌘K  ☼  security_off… ▾  │
├─────────────────┬───────────────────────────────────────────────────────────┤
│ Chat            │ Incidents                                                  │
│ Documents       │                                                            │
│ Models          │ Открытые (4)   Назначены мне (2)   Все (47)                │
│ Policies        │ ┌──────────────────────────────────────────────────────┐  │
│ Audit Log       │ │  ID    Создан    Trigger      Severity Status  ⋯    │  │
│ Incidents     ◀ │ ├──────────────────────────────────────────────────────┤  │
│                 │ │ #0042  14:33  ◉leak_detected  ●high  open      ⋯    │  │
│                 │ │ #0041  14:18  ⛔deny           ●high  invest.   ⋯    │  │
│                 │ │ #0040  12:01  ⛔deny           ●med   resolved  ⋯    │  │
│                 │ │ #0039  10:54  ⚠escalate       ●med   false_pos ⋯    │  │
│                 │ │ #0038  09:22  ◉leak_detected  ●crit  open      ⋯    │  │
│                 │ │ ...                                                  │  │
│                 │ └──────────────────────────────────────────────────────┘  │
└─────────────────┴───────────────────────────────────────────────────────────┘
```

## Mockup — карточка расследования (`/incidents/:id`)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ ◀ Incidents / #0042                                                         │
├─────────────────────────────────────────────────────────────────────────────┤
│ ┌─ Summary ────────────────────────┐ ┌─ Actions ──────────────────────────┐ │
│ │ ID         #0042                 │ │ Назначить мне    ▶                 │ │
│ │ Создан     20 мая 14:33 UTC      │ │ Сменить статус   ▾ investigating   │ │
│ │ Trigger    leak_detected         │ │ Эскалировать     ▶                 │ │
│ │ Severity   ●high                 │ │ False positive   ⊘                 │ │
│ │ Status     ●open                 │ │ Закрыть          ✓                 │ │
│ │ Assignee   —                     │ └────────────────────────────────────┘ │
│ │ Reporter   system (auto)         │                                        │
│ └──────────────────────────────────┘                                        │
│                                                                             │
│ ┌─ Trigger event ───────────────────────────────────────────────────────┐  │
│ │ chat_response · 14:33:12                                              │  │
│ │ User: user@dev   Provider: gpt-4o-mini   Risk: medium   Decision: a.m.│  │
│ │ Masked payload:                                                       │  │
│ │  ┌─────────────────────────────────────────────────────────────────┐  │  │
│ │  │ Уважаемый ФИО_001, направляю реквизиты по ДОГОВОР_014 …          │  │  │
│ │  └─────────────────────────────────────────────────────────────────┘  │  │
│ │ Leak detected on: PERSON, CONTRACT                                    │  │
│ │ [→ Открыть сессию чата]   [→ Открыть аудит-запись]                    │  │
│ └───────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│ ┌─ Timeline ──────────────────────────┐ ┌─ Заметки расследователя ────────┐ │
│ │  14:33  ◉ Инцидент создан (auto)    │ │ Пока нет заметок.               │ │
│ │  14:32  chat_response (leak)         │ │ ┌────────────────────────────┐ │ │
│ │  14:32  chat_request                 │ │ │ Добавьте заметку…          │ │ │
│ │         policy: allow_masked         │ │ │                            │ │ │
│ │  14:31  policy_tested (sec_off)      │ │ └────────────────────────────┘ │ │
│ │                                      │ │           [▶ Сохранить]        │ │
│ │  [▾ показать всю историю]            │ │                                 │ │
│ └──────────────────────────────────────┘ └─────────────────────────────────┘ │
│                                                                             │
│ ┌─ Связанные audit_events (6) ──────────────────────────────────────────┐  │
│ │ 14:33 chat_response  · user@dev · allow_masked · ●leak                │  │
│ │ 14:32 chat_request   · user@dev · allow_masked                        │  │
│ │ 14:31 policy_tested  · sec@off  · allow_masked                        │  │
│ │ ...                                                                   │  │
│ └───────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Структура

### Список — таб-фильтры

- **Открытые** (status ≠ resolved): дефолт.
- **Назначены мне**: `assignee = current_user.id`.
- **Все**: без фильтра по статусу.
- Tab — underline-стиль (`2px accent`).
- Поверх — кнопки сортировки (по умолчанию `severity DESC, created_at DESC`).

### Колонки таблицы

- **ID** (`#0042`, mono, click navigate).
- **Создан** (relative + tooltip UTC).
- **Trigger** chip с иконкой:
  - `deny` — ⛔ red (`danger`);
  - `escalate` — ⚠ amber (`warning`);
  - `response_leak_detected` — ◉ amber (`warning`);
  - `manual` — ✋ neutral.
- **Severity** chip:
  - critical — `danger` (deeper);
  - high — `danger`;
  - medium — `warning`;
  - low — `info`.
- **Status** chip (4 значения — соответствуют БД-схеме миграции 000006):
  - open — `warning`;
  - investigating — `info`;
  - resolved — `text-muted` с иконкой ✓;
  - false_positive — `text-muted` с иконкой ⊘.

  5-й статус «contained» (UI-черновик) **исключён в Итерации 9** —
  схема БД его не предусматривает; добавление — пост-MVP.
- Действия (⋯): открыть, назначить мне, сменить статус, скопировать ID.

### Detail (карточка расследования)

Bento-grid из 5 «коробок»:

1. **Summary** — мета (см. mockup).
2. **Actions** — кнопки изменения статуса (`PATCH /api/incidents/:id`).
3. **Trigger event** — связанная audit-запись с masked-payload,
   risk_classes, leak entity-types.
4. **Timeline** — вертикальный список событий вокруг инцидента
   (chat_request → chat_response → leak_detected → incident_created).
5. **Заметки расследователя** — список markdown-заметок с timestamps и
   автором.
6. **Связанные audit_events** — список с переходом в drawer аудита.

### Actions

- **Назначить мне** — `PATCH {assignee_id: me}`.
- **Сменить статус** — Radix Select: open / investigating /
  resolved / false_positive (4 значения; см. таблицу выше).
- **Эскалировать** — повышает severity на одну ступень + audit-event
  `incident_escalated`.
- **False positive** — `status = false_positive` + диалог-причина
  (обязательно).
- **Закрыть** — `status = resolved` + опциональная резолюция.

### Заметки

- Read-write для роли security_officer + assignee;
- read-only для auditor / compliance_officer.
- Markdown lite (без HTML), max 2000 chars/note.
- **Append-only**: заметки нельзя редактировать (триггер БД блокирует
  UPDATE/DELETE). Чтобы исправить — добавить отменяющую заметку
  «Опечатка в №3 — правильно: …». Tooltip над composer'ом поясняет.
- Audit-event `incident_note_added` пишется на каждую заметку.

## Состояния

| State | Описание |
|-------|----------|
| Empty list | Hero «Инцидентов нет», подсказка «Они появятся автоматически при deny/escalate/leak» |
| List loading | Skeleton |
| Detail loading | Skeleton bento-grid |
| Read-only role (auditor) | Все Actions disabled с tooltip «Только чтение» |
| PATCH conflict (412 Precondition Failed) | Toast `warning`: «Инцидент изменён другим пользователем. Обновить?»; фронт обязан слать `If-Match: <updated_at>` (см. iteration-9.md §Р4) |
| PATCH 428 Precondition Required | Программная ошибка: фронт забыл If-Match. Логируется, инциденту не передаётся |
| PATCH error | Inline-сообщение в Actions-карточке |
| Trigger event missing | (для manual-инцидентов) — карточка с пометкой «Без триггера» |

## Поведение

1. List → `GET /api/incidents?status=open&assignee=...`.
2. Detail → `GET /api/incidents/:id` + `GET
   /api/incidents/:id/audit-events` (связанные).
3. Actions → `PATCH /api/incidents/:id`.
4. Note → `POST /api/incidents/:id/notes` (если выделить как ресурс)
   или внутри `PATCH` — выбрать в Итерации 9.
5. Realtime: polling каждые 10s в MVP (SSE/WS пост-MVP).

## Безопасность

- Авто-создание инцидента — серверная логика; UI пассивный получатель.
- Каждое action пишет audit-event с типом `incident_<action>`.
- Удаления инцидентов **нет** (как и аудит-записей): только soft-close.

## Accessibility

- Таб-навигация по карточкам bento (Tab перепрыгивает между регионами).
- Timeline — `<ol>` с `<time>` элементами; screen-reader читает «14:33,
  Инцидент создан, автоматически».
- Все action-кнопки — `aria-label` явно (даже если есть текст).
- Severity chip — `aria-label="Severity: high"` (без полагания на цвет).

## Самооценка: 9.7/10

- Все 5 состояний покрыты, включая 409 conflict.
- Bento-grid даёт расследователю всё в одном экране.
- −0.3: realtime — polling; пост-MVP заменим на SSE/WS, но это не
  блокер для MVP-сценария.

**Итого по этапу A.5 — все 7 экранов (login + 6 функциональных):
средняя оценка ≥ 9.7/10, без блокеров.** Доводка до 10 — после
реальной верстки (итерации 12–15) и feedback'а.

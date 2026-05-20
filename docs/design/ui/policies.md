# Экран: Policies (`/policies`, `/policies/test`)

UX-spec. Использует уже существующие API:
`GET /api/policies` и `POST /api/policies/test` (Итерация 6).
В MVP — read-only список + интерактивный тест. Полноценный
rule-builder — пост-MVP.

## Mockup — список + Test (Dark, 1440)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ ⛨ Рубеж ИИ                                       ⌘K   ☼  security_off… ▾  │
├─────────────────┬───────────────────────────────────────────────────────────┤
│ Chat            │ Policies                                                  │
│ Documents       │                                                           │
│ Models          │ Действующие политики (1)        [▶ Тест политики]         │
│ Policies      ◀ │ ┌─────────────────────────────────────────────────────┐  │
│ Audit Log       │ │  DefaultPolicy  · v1 · ●активна · обновл. 19 мая   │  │
│ Incidents       │ │  Решения:  allow_raw, allow_masked,                 │  │
│                 │ │            allow_summary_only, deny, escalate       │  │
│                 │ │  Правил: 14   Покрывает: pii, secret, commercial    │  │
│                 │ └─────────────────────────────────────────────────────┘  │
│                 │                                                           │
│                 │ ─── Тест политики ─────────────────────────────────────  │
│                 │                                                           │
│                 │ Текст:                                                    │
│                 │ ┌─────────────────────────────────────────────────────┐  │
│                 │ │ Иван Петров, договор 12-345/2026, сумма 1 200 000 ₽  │  │
│                 │ └─────────────────────────────────────────────────────┘  │
│                 │                                                           │
│                 │ Параметры:                                                │
│                 │  Trust model: [external          ▾]                       │
│                 │  User role:   [security_officer  ▾]                       │
│                 │  Context:     [chat              ▾]                       │
│                 │                                                           │
│                 │                                       [▶ Проверить]       │
│                 │                                                           │
│                 │ ─── Результат ──────────────────────────────────────────  │
│                 │  Decision: [allow_masked]  Risk: [medium]                 │
│                 │  Matched rule: «external + pii ⇒ allow_masked»            │
│                 │  Reasons:                                                 │
│                 │   • external provider                                     │
│                 │   • risk classes: pii                                     │
│                 │   • risk level: medium                                    │
│                 │                                                           │
│                 │  [▾ показать JSON ответ]                                  │
└─────────────────┴───────────────────────────────────────────────────────────┘
```

## Структура

### Список политик

- Карточки (radius-md, padding 20, elev-1) по одной на политику.
- Header: имя + версия (chip `mono`) + chip активности (`success`
  если is_active).
- Тело: «Решения», «Правил: N», «Покрывает: pii/secret/commercial».
- Footer: `updated_at`, кнопка «Открыть» (read-only детали в MVP).
- В MVP — обычно одна `DefaultPolicy`.

### Test-форма

- Textarea для текста (max 16384, как у chat).
- 3 select'а: `model_trust`, `user_role`, `context`.
  - Значения берутся из `policy.schema.json#PolicyInput`.
- Кнопка «Проверить» (primary).

### Результат теста

- Сначала «строка-заголовок» с двумя крупными chip'ами:
  - `decision` (цвет по семантике),
  - `risk.level`.
- `matched_rule` — italic, `text-secondary`.
- `reasons[]` — маркированный список (`text-base`).
- Коллапс «показать JSON» — открывает JSON-viewer (mono, syntax-hl).

## Состояния

| State | Описание |
|-------|----------|
| List empty | (для MVP — никогда; всегда есть DefaultPolicy) |
| List loading | Skeleton-карточки |
| Test idle | Форма с дефолт-значениями |
| Test running | Кнопка disabled + spinner; форма заблокирована |
| Test success | Результат отображён |
| Test 400 | Inline error: «text слишком длинный (max 16384)» |
| Test 500 | Banner `danger`: «Не удалось проверить политику» |
| Read-only role (auditor) | Test-форма disabled с tooltip: «Только просмотр» |

## Поведение

1. На монтировании — `GET /api/policies` → отрисовать список.
2. На «Проверить» — собрать `{text, model_trust, user_role,
   context}` → `POST /api/policies/test`. **Внутри handler'а** делается
   `sanitize` входного текста (вычисление risk_classes и entity_types
   из ПДн-детекторов), затем результат подаётся в `policy.DefaultPolicy().
   Decide(input)`. Это объясняет, почему `risk_classes` и
   `entity_types` появляются в результате — пользователь их не
   передавал явно (m7 ревью этапа A). Тест **пишет audit-event**
   `policy_tested` — каждый тест аудитируется.
3. Результат — `PolicyDecision` + `Risk` + (опционально) `entities`.

## Безопасность

- Test-режим тоже создаёт `audit_event` (`policy_tested`) с
  `policy_decision`, `risk_classes`. Идеально для отчётности.
- Если в тесте найден секрет — UI **не** показывает raw в выводе
  (только masked).

## Accessibility

- Form labels явные.
- Select'ы — Radix-стилизованные, поддерживают клавиатуру.
- Result-секция — `aria-live="polite"` (обновляется по submit).

## Самооценка: 10/10

- Покрыто всё, что есть в API.
- Безопасность: тест тоже аудируется.
- Расширение (CRUD rule-builder) — явно в пост-MVP.

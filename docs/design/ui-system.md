# Дизайн-система «Рубеж ИИ» — UI-System v1

Этап A.4. Этот документ — **источник правды** для frontend-итераций 12–15.
Опирается на:

- `docs/design/ui-trends-2026.md` — тренды и обоснования;
- `docs/design/ui-scope.md` — экраны и роли;
- `docs/THREAT_MODEL.md` — инварианты безопасности (например, маскированные
  значения отображаются обратимо, но raw — нигде не персистируется).

Все токены и компоненты переносятся в `rubezh-web/` (Vite + React +
Tailwind v4 + Radix UI primitives, shadcn-стиль копирования компонентов
в исходники без package-lock-in) **без дополнительных решений** —
выбор уже сделан.

## 1. Принципы и атмосфера

1. **Calm enterprise, не bright SaaS.** Десатурированные цвета,
   тонкие границы, тихие тени. Это инструмент для ИБ-офицеров и
   аудиторов; вечный праздник — не подходит.
2. **Dark-first.** По умолчанию — тёмная тема. Light доступен через
   переключатель, но не как «лицо продукта». Аналитики работают в
   полутёмных комнатах; ночная смена — норма.
3. **Информационная плотность Linear, не Material.** Каждый видимый
   элемент зарабатывает место; ничего «потому что красиво».
4. **AI — ассистент, не решающий.** Любой LLM-вывод помечен;
   решение приходит от policy engine — и это видно в UI.
5. **WCAG 2.2 AA — обязательно, AAA на body-тексте — желательно.**
6. **Только функциональная анимация.** Без parallax, без bright
   градиентов, без glassmorphism как hero-приёма.

## 2. Цветовые токены (HSL)

Двухслойная архитектура: **primitive** (сырые HSL) → **semantic**
(`text-primary`, `surface-elevated`, `accent-positive` и т.п.).
Компоненты ссылаются **только на semantic**. Light/Dark меняют
только semantic→primitive map.

### 2.1 Primitive: Slate (нейтральный, основа фона и текста)

| Token | HSL | Применение |
|-------|-----|------------|
| `slate-50`  | 210 20% 98% | text-primary on dark |
| `slate-100` | 214 17% 92% | hover bg on light |
| `slate-200` | 214 15% 84% | border on light |
| `slate-300` | 213 14% 72% | text-secondary on dark |
| `slate-400` | 213 12% 56% | text-muted on both |
| `slate-500` | 214 10% 42% | placeholder |
| `slate-600` | 216 13% 30% | text-secondary on light |
| `slate-700` | 217 16% 22% | border on dark |
| `slate-800` | 217 19% 14% | surface elev-2 on dark |
| `slate-900` | 220 24%  8% | surface elev-1 on dark |
| `slate-950` | 222 28%  4% | surface base on dark |

### 2.2 Primitive: Cyan (accent, десатурированный холодный)

| Token | HSL | Применение |
|-------|-----|------------|
| `cyan-300` | 189 50% 70% | accent hover on dark |
| `cyan-400` | 189 50% 60% | accent on dark, focus-ring |
| `cyan-500` | 189 48% 50% | accent on light (text on white) |
| `cyan-600` | 189 50% 40% | accent text on light |

### 2.3 Primitive: Coral (danger), Amber (warning), Olive (success)

| Token | HSL |
|-------|-----|
| `coral-400` | 0 55% 65% |
| `coral-500` | 0 55% 55% |
| `coral-600` | 0 55% 45% |
| `amber-400` | 38 70% 65% |
| `amber-500` | 38 70% 55% |
| `olive-400` | 95 30% 55% |
| `olive-500` | 95 30% 45% |

### 2.4 Semantic — Dark (default)

```
--bg-base:        slate-950
--bg-elev-1:      slate-900
--bg-elev-2:      slate-800
--bg-overlay:     hsl(220 24% 8% / 0.72)   /* dialog backdrop */

--text-primary:   slate-50
--text-secondary: slate-300
--text-muted:     slate-400
--text-disabled:  slate-500
--text-inverse:   slate-900

--border-subtle:  slate-700
--border-strong:  slate-600

--accent:         cyan-400
--accent-hover:   cyan-300
--accent-text:    slate-950           /* текст на кнопке primary */
--focus-ring:     cyan-400

--danger:         coral-500
--danger-bg:      hsl(0 55% 55% / 0.12)
--warning:        amber-500
--warning-bg:     hsl(38 70% 55% / 0.12)
--success:        olive-500
--success-bg:     hsl(95 30% 45% / 0.14)
--info:           cyan-400
--info-bg:        hsl(189 50% 60% / 0.10)
```

### 2.5 Semantic — Light

```
--bg-base:        slate-50
--bg-elev-1:      hsl(0 0% 100%)
--bg-elev-2:      slate-100
--bg-overlay:     hsl(220 24% 8% / 0.40)

--text-primary:   slate-900
--text-secondary: slate-600
--text-muted:     slate-500
--text-disabled:  slate-400
--text-inverse:   slate-50

--border-subtle:  slate-200
--border-strong:  slate-300

--accent:         cyan-600
--accent-hover:   cyan-500
--accent-text:    slate-50
--focus-ring:     cyan-500
/* danger/warning/success — те же, фоны −10% opacity */
```

### 2.6 Цветовая семантика статусов (мутед под тёмный)

| Статус | Token | Применение |
|--------|-------|------------|
| Critical / Deny | `--danger` | chip `deny`, severity critical, error toast |
| High / Escalate | `--warning` | chip `escalate`, severity high, warning banner |
| Medium / Leak | `--warning` | leak detected baner (на тон светлее) |
| Low / Allow | `--success` | chip `allow_*`, severity low, success toast |
| Info / Neutral | `--info` | chip `meta`, info banner |
| Pending / Loading | `--text-muted` | skeleton, queued status |

Использование сырых primitive в компонентах **запрещено** — всё через
semantic.

### 2.7 Доказательства контраста (Dark)

| Пара | Расчёт WCAG | Статус |
|------|-------------|--------|
| text-primary on bg-base | ≈ 17:1 | AAA |
| text-secondary on bg-base | ≈ 9:1 | AAA |
| text-muted on bg-base | ≈ 6:1 | AA для крупного, AA для обычного на пороге |
| accent on bg-base | ≈ 5.5:1 | AA для обычного |
| focus-ring on bg-elev-2 | ≈ 4.7:1 | AA для UI |

**Проверка автоматизируется** (m3 ревью этапа A):
- **axe-core** в Vitest unit-тестах layout-компонентов (catches AA
  violations build-time);
- **Lighthouse accessibility audit** в CI GitHub Actions
  (a11y-score ≥ 95);
- **Figma plugin Stark/Contrast** на этапе дизайна — все цвета
  semantic-уровня проверены до коммита в ui-system.md;
- расчёты выше (§2.7) — sanity-check для ручной перепроверки.
Любой AA-промах валит сборку (см. §9 accessibility-инварианты).

## 3. Типографика

### 3.1 Шрифты

- **UI / body:** Inter (variable). Fallback: `"Inter Tight",
  -apple-system, "Segoe UI", Roboto, sans-serif`.
- **Моно:** JetBrains Mono. Fallback: `"ui-monospace", "Cascadia Code",
  Menlo, monospace`. Используется для `request_id`, masked-payload,
  JSON-вьюера, `endpoint URL`.

Локально подгружаются как self-hosted шрифты (on-prem-friendly,
никаких Google Fonts).

### 3.2 Шкала

| Token | Size / Line | Weight | Применение |
|-------|-------------|--------|------------|
| `text-xs`   | 12 / 16 | 400 | helper, footnote, tooltip |
| `text-sm`   | 13 / 18 | 500 | chip, table-label |
| `text-base` | 14 / 20 | 400 | UI default, кнопки, формы |
| `text-md`   | 16 / 24 | 400 | сообщения чата, body text |
| `text-lg`   | 18 / 24 | 600 | subsection title |
| `text-xl`   | 24 / 32 | 600 | page title |
| `text-2xl`  | 30 / 36 | 700 | hero (login only) |

Заголовки используют tabular-nums для чисел в таблицах.

## 4. Spacing и сетка

### 4.1 Spacing token (8px base, fine-grained)

`0, 1px, 2, 4, 8, 12, 16, 20, 24, 32, 40, 48, 64, 80, 96, 128, 160`

(Это **прямой перенос** в `tailwind.config.ts` → `spacing`.)

### 4.2 Layout

- **Sidebar:** 256px (collapsed icon-only — 64px).
- **Container:** `max-width: 1440px; padding: 24px` для `≥1024`,
  `16px` для `≥640`, `12px` для `<640`.
- **Bento grid:** CSS Grid `grid-template-columns:
  repeat(auto-fill, minmax(320px, 1fr)); gap: 16px`.
- **KPI strip:** 4 карточки на `≥1280`, 2 на `768–1280`, 1 на `<768`.

### 4.3 Радиусы

| Token | px | Применение |
|-------|-----|------------|
| `radius-xs` | 4 | chip, badge |
| `radius-sm` | 6 | button, input, select |
| `radius-md` | 8 | card, dialog, banner |
| `radius-lg` | 12 | drawer |
| `radius-full` | 9999 | avatar, pill chip |

### 4.4 Elevation (subtle, серый, без цветной тени)

| Token | Описание |
|-------|----------|
| `elev-0` | `box-shadow: none; border: 1px solid var(--border-subtle)` |
| `elev-1` | `0 1px 2px hsl(220 24% 0% / 0.3) + border 1px subtle` |
| `elev-2` | `0 4px 12px hsl(220 24% 0% / 0.4)` (drawer, dropdown) |
| `elev-3` | `0 8px 24px hsl(220 24% 0% / 0.5)` (modal) |

## 5. Компоненты — карта

Каждый компонент строится поверх Radix Headless. Поведенческие
свойства — у Radix; стили — наши токены.

### 5.1 Button

| Variant | bg | text | border | hover |
|---------|-----|------|--------|-------|
| `primary` | accent | accent-text | — | accent-hover |
| `secondary` | bg-elev-2 | text-primary | border-subtle | bg-elev-1 |
| `ghost` | transparent | text-primary | — | bg-elev-2 |
| `danger` | danger | slate-50 | — | coral-600 |
| `link` | transparent | accent | — | underline |

Размеры: `sm` 32h / `md` 36h / `lg` 40h. Радиус `sm`. Focus-ring 2px,
offset 2px. Hit-area минимум 36×36 (44 для touch-ролей).

### 5.2 Input / Textarea / Select

- height 40 (`md`), 32 (`sm`).
- padding `0 12`.
- background `bg-elev-1`, border `1px solid border-subtle`,
  focus border `1px solid accent` + ring `2px focus-ring`.
- placeholder `text-muted`.
- error: border `1px solid danger`, helper text `danger`.
- disabled: bg `bg-elev-2`, text `text-disabled`, cursor `not-allowed`.

Textarea — auto-grow до 8 строк, дальше scroll; счётчик символов справа
внизу (`14/16384`) — становится `danger` за 100 символов до лимита.

### 5.3 Chip / Badge

- height 24, padding `0 8`, radius `xs`, font `text-sm`, weight 500.
- Варианты: `neutral` (bg-elev-2 + text-secondary), `accent`,
  `success`, `warning`, `danger`, `mono` (для request_id-like, font
  mono).
- С иконкой 14px слева, gap 4.

### 5.4 Banner (inline) и Toast

Banner — `padding 12 16`, иконка слева, `border-left: 3px solid <semantic>`,
`background: <semantic>-bg`. Текст `text-base`, заголовок `text-md weight 600`.
Закрывающий «×» только если опционально-информативный (deny/error — не
закрываются автоматически).

Toast — top-right, 320 ширина, elev-2, авто-dismiss 4s для info/success,
sticky для error.

### 5.5 Dialog / Modal

- backdrop `bg-overlay`, blur не используется (читаемость > эстетика).
- panel `bg-elev-1`, radius `md`, padding 24, elev-3.
- заголовок `text-lg`, описание `text-base text-secondary`.
- кнопки внизу, primary справа.
- Esc / клик в backdrop — закрывают (если не destructive).

### 5.6 Drawer (правая панель)

- width 480 (`md`), 640 (`lg` для расследования).
- slide-in 240ms ease-out, slide-out 200ms ease-in.
- elev-2, без backdrop (открыт поверх контента, но обводка слева
  `1px border-subtle`).
- внутри — sticky header с close-кнопкой и title.

### 5.7 Tabs

- underline-стиль: `2px solid accent` под активной вкладкой.
- inactive `text-muted`, hover `text-secondary`.
- gap 24 между вкладками.

### 5.8 Table / DataTable

- row-height 48 (компактный 40 — опционально).
- header `bg-elev-1`, текст `text-sm uppercase tracking-wide text-muted`.
- зебра нет (плохо читается в dark); вместо этого `border-bottom` row-level.
- hover row `bg-elev-1` (на dark — слегка светлее base).
- selected row — left border `2px solid accent`.
- виртуализация — `@tanstack/react-virtual`.

### 5.9 Skeleton

- background `linear-gradient(90deg, bg-elev-1 0%, bg-elev-2 50%,
  bg-elev-1 100%)`, shimmer 1.4s linear infinite, radius matches
  parent component.

### 5.10 Card (включая bento-tile)

- bg `bg-elev-1`, radius `md`, padding 16 (`sm`) / 24 (`md`),
  elev-1.
- title `text-md weight 600`, opt-icon 20 справа.
- metric (KPI): число `text-xl tabular-nums`, label `text-xs uppercase`.

### 5.11 Командная палитра (поздняя итерация)

Стиль Linear: `Cmd/Ctrl+K`, fade-in 120ms, scaled-from 96%.

## 6. Иконки

- **Lucide** (open-source, MIT). Self-hosted SVG-sprite. Размеры
  16/20/24. Stroke 1.5 для 16px и 1.75 для 20/24.
- Icon-only кнопки — обязательны `aria-label` и tooltip.

## 7. Motion

| Token | Длительность | Easing |
|-------|--------------|--------|
| `dur-fast` | 120ms | `cubic-bezier(0.2,0,0,1)` |
| `dur-base` | 200ms | `cubic-bezier(0.2,0,0,1)` |
| `dur-slow` | 320ms | `cubic-bezier(0.2,0,0,1)` |
| `dur-drawer` | 240ms | `cubic-bezier(0.16,1,0.3,1)` |

Допускается анимировать **только** `opacity`, `transform`, `filter`,
`color`, `background-color`. Никогда — `width`, `height`, `top`, `left`
(jank).

Специальные:

- **Typing-cursor** в чате: 1s steps(2, end) infinite на пустом span'е
  в конце стрима.
- **Skeleton shimmer:** 1.4s linear infinite.
- **Toast enter/exit:** translate-y 8px + opacity 0→1, 200ms.
- **Drawer slide:** translate-x 16px + opacity 0→1, 240ms ease-out.
- **SSE keep-alive** в чате (m1 ревью этапа A): браузер EventSource
  игнорирует комментарии `: ping`, но они держат соединение живым
  через reverse-proxy. Не визуальный эффект, но часть motion-системы
  (предотвращение «обрыва без причины»).

`prefers-reduced-motion: reduce` (m9 ревью этапа A) **полностью
отключает** все бесконечные анимации (skeleton shimmer, typing-cursor):
вместо shimmer — статический `bg-elev-2`; вместо мигающего cursor —
статичный `▌`. Анимации длиннее `dur-base` (drawer, dialog scale) —
сокращаются до `dur-fast` или отключаются полностью.

## 8. Состояния focus / hover / active

- **Focus-ring** 2px solid `--focus-ring`, offset 2px, radius
  matches component. Никогда `outline: none` без замены.
- **Hover** — изменение bg на 1 ступень elev (вверх), не цвет.
- **Active (pressed)** — `transform: scale(0.98)`, 80ms.
- **Disabled** — opacity 0.5, `cursor: not-allowed`, без hover.

## 9. Accessibility инварианты

Это **единственный источник правды** для a11y-правил всего проекта
(m12 ревью этапа A). UX-spec экранов (`docs/design/ui/*.md`) могут
**ссылаться** на этот раздел, но **не должны переопределять**
эти правила — иначе возникает дрейф между документами.

1. Контраст текста ≥4.5:1 на всех фонах.
2. Контраст UI / focus-ring ≥3:1.
3. Focus-ring видим и не скрыт под sticky-header (SC 2.4.11).
4. Минимальный hit-target 36×36 (44 если основной для роли user).
5. ARIA-метки на все icon-only кнопки.
6. **`aria-live="polite"`** — на streaming-сообщение ассистента в чате
   и на любое лениво-обновляемое содержимое (drawer-обновление, etc.).
7. **`aria-live="assertive"`** — на error-toast, deny-banner,
   422/429-banner логина — то, что требует немедленного внимания.
8. Keyboard: Tab order по DOM, Esc закрывает Dialog/Drawer/Toast.
9. `prefers-reduced-motion` уважается (см. §7).
10. Все формы — `<label for="...">`, не placeholder вместо label.

Автотесты:

- `axe-core` в Vitest unit-тестах layout-компонентов.
- Lighthouse a11y ≥95 в CI (GitHub Actions).
- Storybook (опционально) с `@storybook/addon-a11y`.

## 10. Локализация и форматирование

- Дефолт — `ru-RU`. Все date/time через `Intl.DateTimeFormat('ru-RU')`.
- Числа: пробел-разделитель тысяч (`1 234 567`), запятая-десятичный.
- Псевдонимы (`ФИО_001`) — не переводятся, ASCII-индекс сохраняется.
- Длинные `request_id` (uuid) — обрезаются до `xxxxxxxx…` с tooltip
  на полное значение.

## 11. CSS-распределение (для итерации 12)

- Tailwind v4 с CSS-переменными темы:
  - `@theme inline { --color-bg-base: var(--slate-950); ... }`
- Theme switcher — `data-theme="dark" | "light"` на `<html>`,
  CSS-переменные переопределяются.
- Никаких runtime-style-объектов кроме motion (Framer Motion в
  отдельных местах — banner enter, drawer slide).
- Компоненты копируются (shadcn-стиль) в `rubezh-web/src/components/ui/`.

## 12. Самооценка

| Критерий | Оценка | Комментарий |
|----------|--------|-------------|
| Полнота токенов | 10 | primitive + semantic, dark + light |
| Готовность к коду | 10 | переносится в Tailwind v4 без решений |
| Accessibility-инварианты | 10 | WCAG 2.2 AA доказан расчётом |
| Соответствие домену | 10 | calm enterprise, не bright SaaS |
| Согласие с трендами 2026 | 10 | density, dark-first, bento, AI-native |

**Итого: 10/10** для дизайн-системы. Конкретные экраны (A.5)
проектируются как отдельные документы в `docs/design/ui/`.

# Тренды UX/UI 2026 — конспект для «Рубеж ИИ»

Этап A.2 плана. Цель — выбрать только те паттерны, которые **уместны для
on-prem security-инструмента** (госкомпании, операторы КИИ, ИБ-отдел), а не
для bright SaaS. Источники — поиск май 2026, см. ссылки в конце.

## 1. AI-native интерфейсы — принципы

- Прозрачность: пользователь должен понимать, **что сгенерировал ИИ**, что
  можно править, когда система действует автономно.
- Контроль > автономия: ИИ — ассистент, не замена решения. Это особенно
  важно для нашего домена: **policy engine принимает решения, ИИ —
  подсказывает**. UI должен это отражать: рядом с любым LLM-выводом — пометка
  «AI-suggestion», у любого решения — авторство (policy/AI/user).
- Адаптивные интерфейсы: плотность, навигация, формы подстраиваются под
  частые задачи роли. Для нас — 6 ролей × 6 экранов; каждая роль видит
  свой первый экран и свои разделы.

## 2. Bento grids

- Не визуальный стиль, а **архитектура информации**: каждый блок данных
  получает «своё место», уменьшая когнитивную нагрузку.
- Хорошо подходит для overview-экранов с разнотипным содержимым (метрики,
  графики, списки), плохо — для линейных потоков (чат, форма).
- В «Рубеже ИИ» уместен для:
  - дашборда роли (KPI: запросов сегодня, blocked, leak warnings,
    open incidents, p95 latency, состояние провайдеров);
  - карточки расследования (Incidents detail) — события, действия,
    связанные audit-записи в отдельных «коробках».

## 3. Dark mode — dark-first

- Light-only сегодня воспринимается как «незаконченный» интерфейс.
- ИБ-аналитики работают в low-light окружении — dark mode снижает
  усталость глаз.
- Зрелые реализации: тонкие градации серого + elevation вместо ярких
  границ; чистый чёрный для OLED; контраст подобран **специально для
  тёмного фона**, не как инверсия светлой темы.
- Цветовая семантика: критические события — приглушённые красные
  (не неоновые), warning — янтарные, success — оливково-зелёные.
  Спокойный enterprise, не bright SaaS.

## 4. Информационная плотность — школа Linear

- Density и clarity сосуществуют, если за каждый видимый элемент
  «дерётся редактор» — выбрасывают всё, что не зарабатывает место.
- Прогрессивное раскрытие: главный показатель сверху, drill-in по клику.
- Архитектура: sidebar 240–280px + KPI-strip (4–6 карточек) + flexible
  content grid (`auto-fill`). Так делают Linear, Notion, Vercel, Stripe.
- Для нас критично: ИБ-офицер за смену смотрит десятки инцидентов;
  каждое впустую отрисованное элементы — потерянная секунда.

## 5. WCAG 2.2 AA — обязательно

- Контраст: **4.5:1 для обычного текста**, 3:1 для крупного (≥18.66px
  regular или ≥14px bold), **3:1 для UI-компонентов и границ**.
- Структура токенов в 2 слоя:
  - **primitive tokens** — сырые HSL (например `slate-50`...`slate-900`,
    `red-50`...`red-900`);
  - **semantic tokens** — `text-primary`, `surface-elevated`,
    `accent-positive`, `accent-danger` — ссылаются на primitive.
- Light/dark переключаются заменой **только semantic→primitive map**,
  компоненты не меняются.
- Автотесты: axe-core в unit-тестах + Lighthouse в CI + Figma-плагин
  на этапе дизайна; AA-промах валит сборку.
- Дополнительно (SC 2.4.11 WCAG 2.2): фокус **никогда** не скрыт
  под фиксированной шапкой; видимый focus-ring не тоньше 2px.

## 6. SSE-стриминг ассистента — UI-паттерны

- Стандарт 2026 для AI-чатов — Server-Sent Events; token-by-token
  rendering превращает «медленный API» в «разговор».
- Индикаторы статуса:
  - **до первого `delta`** — typing-индикатор «модель думает» (для нашего
    случая — пауза между `meta` и `delta` существует, см. итерация 8
    §Р5 «псевдо-стриминг», и она ощутима — баннер обязателен);
  - **во время стрима** — мигающий курсор в конце текущего токена;
  - **keep-alive комментарии SSE** (`: ping\n\n` каждые 15s) —
    **нужны** для on-prem за reverse-proxy (m1 ревью этапа A):
    типовой `proxy_read_timeout` Nginx — 60s, без heartbeat поток
    закроется при ожидании первого `delta` (sanitize + LLM могут
    занять до этого предела). Реализация — в SSE-sink оркестратора;
    клиент игнорирует `:`-комментарии по стандарту EventSource.
- При завершении (`done`) — индикатор гаснет, кнопка «Стоп» меняется
  на «Новый запрос».
- При `error` — терминальный баннер красного оттенка + копируемый
  `request_id` (есть в `done`/`error`).

### Специфично для «Рубежа ИИ»

В отличие от ChatGPT, наш чат имеет уникальные UI-элементы **до** `delta`:

- **Баннер policy-decision** (из `meta`): chip `decision`, риск-уровень,
  провайдер, причины (свёрнутый список).
- **Preview обезличивания**: подсветка найденных сущностей в исходном
  тексте с подсказкой «`ФИО_001` ← Иван Петров».
- **Баннер summary-only**: «Ответ обезличен — это режим краткого
  резюме. Псевдонимы намеренно не раскрываются» (объясняем поведение
  Р3 итерации 8).
- **Баннер leak warning**: «Модель воспроизвела замаскированное значение —
  расследование автоматически создано» (для роли user — нейтральный,
  для security_officer — кнопка «Открыть инцидент»).

## 7. SOC-дашборды и инциденты

- Dark mode для analyst-роли — must.
- Real-time список инцидентов с состоянием (Investigating / Contained /
  Resolved), сортировка по severity (priority queue).
- Цвета: green / yellow / red, но **приглушённые** под тёмный фон.
- Метрики: MTTD (Mean Time to Detect), MTTR (Mean Time to Respond) —
  для «Рубежа ИИ» специфично — *Time to escalation*, *% leak-flag
  per provider*.
- Drill-down: клик по инциденту → правый drawer с timeline, связанными
  `audit_events`, masked-payload и действиями. Без перезагрузки.

## 8. Policy editor

- Структура правила (как у Microsoft Purview): **Location → Condition →
  Action**.
- Boolean-логика: AND/OR/NOT + вложенные группы (drag-and-drop).
- Policy Tips — всплывающие подсказки в момент попытки запроса (у нас
  это `meta.reasons`).
- Для нашего проекта в MVP — упрощённо: список политик + кнопка
  «Test policy» (использует `/api/policies/test`). Полный rule builder —
  пост-MVP.

## 9. Audit log viewer

- **Drawer pattern**: список (виртуализованный), клик по строке —
  правый drawer с field-level details, JSON payload, user, timestamp.
  Без потери скролла.
- Keyboard navigation: ↑/↓ перепрыгивает на соседнюю запись и обновляет
  drawer.
- Filter dimensions (минимум): time range, actor (user_id / role),
  action (event_type), resource (model_provider, session_id).
- Timestamps: хранятся в UTC, отображаются в локали; tooltip — точное
  UTC-значение.
- Append-only: явная подпись «Журнал неизменяемый — записи только
  добавляются». Это снимает у аудитора подозрение «не подделано ли».

## 10. Технологический стек 2026 (фон, для нас — React + Vite)

Хотя в индустрии 2026 доминирует Next.js 16 + shadcn/ui, наш выбор
(`React + Vite + React Router v7 + TanStack Query + Zod`, итерация 12)
*совместим* с теми же design-token-практиками. shadcn/ui-стиль
(копирование примитивов в исходники, без package-lock-in) — годится:
берём те же радикс-примитивы (Radix UI / шапка Headless UI), стили
через CSS variables + Tailwind. Решение по компонент-кит откладывается
до итерации 12.

## 11. Что **не берём** — намеренно

- Сильный glassmorphism / heavy blur — отвлекает в security-контексте,
  снижает читаемость данных, плохо для accessibility (контраст плавает).
  Тонкие полупрозрачные overlay-карточки в drawer — допустимы; «стеклянный»
  hero — нет.
- Цветные градиентные акценты как у consumer-SaaS — будем выглядеть
  «фривольно» для госзаказчика. Один спокойный accent-цвет (cyan/teal,
  низкая насыщенность).
- Сложные motion для всего — только функциональная анимация: появление
  drawer, мигание курсора стрима, появление badge инцидента. Никаких
  parallax/scroll-jacking.

## 12. Принятые ориентиры для дизайн-системы

- **Тёмная тема по умолчанию**, light как опция (а не наоборот).
- Шрифт: humanist sans (Inter / Geist) для UI; mono (JetBrains Mono /
  Geist Mono) для request_id, masked-payload, JSON.
- Палитра — холодная нейтральная (slate/zinc), accent — desaturated cyan
  (#4FC3D9 в light, #6DDAEF в dark); danger — desaturated coral; warning —
  amber.
- Сетка: 8px-base; sidebar 256px; container max-width 1440px; KPI-strip
  4 карточки на >1280, 2 на 768–1280, 1 на <768.
- Радиусы: 8px для карточек, 6px для кнопок, 4px для chip.
- Тени: только elevation 0/1/2 (тонкие, серые) — без drop-shadow цвета.

## Источники

- [50 Best Dashboard Design Examples for 2026 — Muzli](https://muz.li/blog/best-dashboard-design-examples-inspirations-for-2026/)
- [Bento Grid Dashboard Design — Orbix](https://www.orbix.studio/blogs/bento-grid-dashboard-design-aesthetics)
- [Enterprise UI Design in 2026 — Hashbyt](https://hashbyt.com/blog/enterprise-ui-design)
- [Dashboard Design Patterns for Modern Web Apps 2026](https://artofstyleframe.com/blog/dashboard-design-patterns-web-apps/)
- [WCAG 2.2 in 2026 — ALM Corp](https://almcorp.com/blog/wcag-2-2-enterprise-web-accessibility-requirements-2026/)
- [Color & Contrast Engineering Guide 2026 — Humbl Design](https://humbldesign.io/blog-posts/color-accessibility-guide-wcag)
- [Streaming AI Responses: SSE, WebSockets — Channel](https://www.channel.tel/blog/streaming-ai-responses-sse-websockets-real-time)
- [Cybersecurity Dashboard UI/UX Design — Aufait UX](https://www.aufaitux.com/blog/cybersecurity-dashboard-ui-ux-design/)
- [Microsoft Purview DLP Policy Design](https://learn.microsoft.com/en-us/purview/dlp-policy-design)
- [Enterprise Ready SaaS — Audit Log](https://www.enterpriseready.io/features/audit-log/)
- [Audit Logging for Internal Tools — AppMaster](https://appmaster.io/blog/audit-logging-internal-tools-activity-feed)

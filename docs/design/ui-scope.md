# UI-Scope MVP — экраны, роли, состояния

Этап A.3. Опирается на:

- роли — `docs/design/identity.md`, `docs/contracts/policy.schema.json#PolicyInput.user_role`;
- решения policy — `docs/contracts/policy.schema.json#PolicyDecision.decision`;
- чат-контракт — `docs/contracts/chat.schema.json`;
- остаточные риски — `docs/THREAT_MODEL.md §6`;
- тренды — `docs/design/ui-trends-2026.md`.

Frontend-каркас — Итерация 12 (`rubezh-web`). Экраны реализуются в 13–15.

## 1. Роли и их «дом»

| Роль | Дом (1-й экран после логина) | Получает доступ к |
|------|------------------------------|-------------------|
| `user` | **Chat** | Chat, Documents (свои) |
| `security_officer` | **Incidents** | Chat, Documents, Policies, Models, Audit Log, Incidents |
| `compliance_officer` | **Audit Log** | Audit Log, Incidents, Policies (read) |
| `admin` | **Models** | Models, Policies (CRUD), Audit Log, Incidents |
| `auditor` | **Audit Log** | Audit Log, Incidents (read) — только чтение |
| `developer` | **Models** | Models, Policies, Audit Log (свои tests) |

Sidebar динамически скрывает разделы, к которым у роли нет доступа.
Прямой переход по URL без прав → 403-экран с подсказкой «свяжитесь с
администратором».

## 2. Экраны и их назначение

### 2.1 Chat (`/chat`, `/chat/:sessionId`)

**Назначение.** Безопасно отправить запрос в LLM, увидеть policy-решение,
предпросмотр обезличивания и стрим ответа.

**Ключевые элементы.**

- Левая колонка: список сессий (`GET /api/chat/sessions`), кнопка
  «Новая сессия».
- Правая колонка:
  - область сообщений (виртуализованный список);
  - **policy-banner** над пустым полем ввода — после `meta`:
    chip `decision` (цвета: `allow_*` — нейтрально-зелёный приглушённый;
    `deny` — desaturated coral; `escalate` — amber), `risk.level` chip,
    `provider` name, кнопка «причины» (свёрнутый список `reasons`);
  - селектор провайдера (из `GET /api/models`);
  - текстарея (limit `maxLength: 16384` из `chat.schema.json`;
    счётчик символов справа внизу);
  - кнопка «Отправить» (Cmd/Ctrl+Enter);
  - кнопка «Превью обезличивания» — открывает диалог с
    `POST /api/sanitize/preview` до отправки (опционально).
- Под потоком ответа — компактный footer с `request_id` (mono),
  «Скопировать» и timestamp.

**Состояния — обязательны.**

| Состояние | Что показываем |
|-----------|----------------|
| Empty (новая сессия) | Hero «Задайте вопрос…», 3 примера-чипа на роль |
| Loading (`POST /api/chat` → ждём `meta`) | Skeleton policy-banner + «Проверка политики…» |
| Streaming (`delta`) | Псевдо-typing курсор, кнопка «Стоп» |
| Done | Гасим typing, показываем footer с request_id |
| Decision `allow_summary_only` | **Жёлтый info-banner**: «Ответ обезличен. Псевдонимы намеренно не раскрываются (режим summary-only)» |
| Decision `deny` | **Красный banner**: «Запрос заблокирован», список `reasons`, нет области стрима |
| Decision `escalate` | **Amber banner**: «Запрос отправлен на эскалацию», ссылка «Открыть инцидент» (для security_officer) |
| `response_leak_detected: true` | **Жёлтый banner поверх ответа**: «Модель воспроизвела замаскированное значение; создан инцидент №…» |
| SSE error | Терминальный красный baner с `message`, кнопка «Повторить» |
| Network drop | Toast «Соединение прервано», auto-reconnect EventSource |

**Что НЕ показываем.** Сырых значений сущностей нигде, кроме исходного
ввода пользователя в текущей сессии (никогда в истории). Псевдонимы
zero-padded (`ФИО_001`), tooltip на hover: hash + «исходное значение
зашифровано» (с итерации 9).

### 2.2 Documents (`/documents`, `/documents/:id`)

**Назначение.** Загрузить документ, видеть статус обработки worker'ом
(Итерация 10), вызвать «передать в чат» (Итерация 11 RAG).

**Ключевые элементы.**

- Drop-zone сверху (drag-and-drop + кнопка «Загрузить»).
- Таблица документов: `name`, `size`, `uploaded_at`, `status` (chip),
  `processed_chunks/total`, `acl`-индикатор, действия.
- Состояния статуса: `queued`, `parsing`, `chunking`, `embedding`,
  `ready`, `failed` (chip-цвета — мутед).
- Detail-страница: метаданные, список chunks (виртуализованный),
  превью текста с маскированием.

**Состояния.**

| Состояние | Что показываем |
|-----------|----------------|
| Empty | Hero «Загрузите PDF/DOCX…», hint про лимит |
| Uploading | Прогресс-бар, чанк-counter |
| Processing | Чип статуса + progress shimmer |
| Failed | Сообщение об ошибке + reason из БД, кнопка «Повторить» |
| Permission denied | «У вас нет доступа к этому документу» (для не-владельцев) |

### 2.3 Policies (`/policies`, `/policies/test`)

**Назначение.** Просмотр действующих политик, тест политики против
ввода через `POST /api/policies/test`.

**Ключевые элементы.**

- Список политик (карточки или таблица): `name`, `version`, `is_active`,
  `created_at`. Read-only для всех в MVP (CRUD — пост-MVP).
- Test-форма: текстовое поле + селектор `model_trust` + селектор
  `user_role` + кнопка «Проверить». Результат — panel с `decision`,
  `reasons`, `matched_rule`.

**Состояния.**

| Состояние | Что показываем |
|-----------|----------------|
| Empty | (для MVP — нет; всегда есть DefaultPolicy) |
| Loading | Skeleton |
| Test result | Decision chip + reasons + matched_rule + JSON-режим (collapsed) |
| Test error | Inline error |

### 2.4 Models (`/models`, `/models/new`)

**Назначение.** Управление провайдерами LLM (`/api/models` — итерация 7).

**Ключевые элементы.**

- Таблица провайдеров: `name`, `kind` (`mock` / `openai_compatible`),
  `trust_level` (chip: `external` — red, `russian_cloud` — amber,
  `on_prem` — cyan, `trusted_local` — green), `is_enabled`, `endpoint`
  (mono, частично замаскирован).
- Кнопка «Добавить провайдера» → диалог: имя, kind, trust_level,
  base_url, api_key (масcked при вводе, не показывается обратно).
- Test connection-кнопка в строке.

**Состояния.**

| Состояние | Что показываем |
|-----------|----------------|
| Empty | Hero «Добавьте первого провайдера» + кнопка |
| Loading | Skeleton-таблица |
| Validation error | Inline-ошибки полей |
| Duplicate name | Toast «Провайдер с таким именем уже существует» |
| API key tooltip | «Ключ зашифрован и не возвращается. Чтобы изменить — введите заново.» |

### 2.5 Audit Log (`/audit`, `/audit/:eventId`)

**Назначение.** Просмотр append-only `audit_events` (итерация 9 API).

**Ключевые элементы.**

- Filter-bar (sticky top): time range, actor (user/role), event_type,
  model_provider, has_leak_flag (boolean), free-text по `detail`.
- Виртуализованная таблица: `created_at` (UTC + relative), `event_type`,
  `user`, `decision`, `risk_level`, `provider`, `request_id` (mono).
- Append-only badge сверху: «Журнал неизменяемый — записи только
  добавляются».
- Клик по строке → правый drawer с полной записью:
  `policy_decision`, `matched_rule`, `risk_classes[]`,
  `masked_payload`, `detail` (JSON-viewer), ссылки на `chat_session_id`,
  `incident_id` (если есть).
- Keyboard: ↑/↓ перепрыгивает между строками с обновлением drawer;
  Esc — закрыть drawer.

**Состояния.**

| Состояние | Что показываем |
|-----------|----------------|
| Empty | (после первого запроса всегда есть события — но фильтр может дать пусто) |
| Empty after filter | «Ничего не нашлось», кнопка «Сбросить фильтры» |
| Loading | Skeleton-строки |
| Drawer loading | Skeleton в правой панели |
| Drawer permission denied | «У вашей роли недостаточно прав видеть детали этого события» |
| Export | Кнопка «Экспорт CSV/JSON» — для compliance/security |

### 2.6 Incidents (`/incidents`, `/incidents/:id`)

**Назначение.** Список инцидентов и карточка расследования (итерация 9).

**Ключевые элементы.**

- Список (карточки или таблица): `id`, `created_at`, `severity`
  (low/medium/high/critical chip), `status` (open / investigating /
  contained / resolved), `trigger` (`deny` / `escalate` /
  `response_leak_detected`), `assignee`.
- Sort по severity по умолчанию.
- Detail-страница (карточка расследования) — bento-grid:
  - **Summary**: severity, status, trigger, created_at, assignee,
    кнопки изменения статуса (PATCH);
  - **Связанные audit_events**: timeline вертикально, клик → drawer;
  - **Trigger event**: исходный `chat_request`/`chat_response` с
    masked-payload, risk_classes, leak_flag;
  - **Заметки расследователя**: список с timestamps (read-write для
    security_officer);
  - **Действия**: «Закрыть как false-positive», «Эскалировать»,
    «Назначить мне».

**Состояния.**

| Состояние | Что показываем |
|-----------|----------------|
| Empty | Hero «Инцидентов нет», подсказка |
| Loading | Skeleton |
| Read-only role (auditor) | Все формы disabled, badge «Только чтение» |
| PATCH error | Inline-ошибка |

## 3. Глобальные элементы

- **Шапка**: логотип «Рубеж ИИ» + breadcrumbs + поиск (поздняя итерация)
  + user-меню (роль, dev-токен подсказка).
- **Sidebar**: разделы по ролям (см. §1), внизу — версия app, ссылка
  на CLAUDE.md (для dev/admin).
- **Toast-система**: success / error / info, top-right, авто-dismiss
  для info/success, sticky для error.
- **Command Palette (Cmd/Ctrl+K)** — поздняя итерация, в MVP опционально.
- **Light/Dark switcher** — но дефолт **dark** для всех ролей.
- **Login** (`/login`): простая форма с dropdown ролей (dev-режим) →
  кнопка «Войти» → дев-токен в SecureCookie. ADR `identity.md`
  предусматривает замену на OIDC.
- **403/404/500** — отдельные screen-state'ы со ссылкой «На главную».

## 4. Обязательные состояния по экранам (сводно)

Для каждого экрана **минимум 5 состояний**: empty / loading / error /
deny+escalate (где применимо) / leak warning (где применимо). Это
проверяется на этапе A.5 при генерации.

## 5. Доступность (WCAG 2.2 AA)

- Контраст текста ≥ 4.5:1 на всех фонах (тёмная тема — особое внимание
  к серым на серых).
- Контраст UI-компонентов и фокус-кольца ≥ 3:1.
- Focus-ring видим, не тоньше 2px, не скрыт под sticky-header (SC 2.4.11).
- Все интерактивы доступны с клавиатуры (Tab, Enter, Esc, стрелки в
  таблицах).
- ARIA-метки на иконке-кнопках («Сбросить фильтры», «Открыть
  расследование» и т.д.).
- Screen-reader: live-region для streaming-сообщений ассистента
  (`aria-live="polite"`), для toast — `aria-live="assertive"` (errors).

## 6. Локализация

MVP — только русский (домен — РФ). Архитектура UI допускает добавление
английского без изменения компонентов (i18n-словари). Все date/time —
русская локаль; числа — пробел-разделитель.

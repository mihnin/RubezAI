# Итерация 9 — Audit / Incidents / шифрованные mappings / история сессии

Архитектурный план **v2**. v1 — 9.1/10, доработка по 3 MAJOR + 8 MINOR.

Закрывает критерии MVP 10 («audit event создаётся»), 11 («incident
создаётся при deny»). Опирается на:

- `docs/PLAN.md`; `docs/API.md`; `docs/THREAT_MODEL.md §6`;
- контракты `docs/contracts/{chat,sanitize,policy}.schema.json`;
- `docs/design/{iteration-8-chat,identity}.md`;
- БД-схема: `migrations/000005..000007`;
- `internal/storage/audit.go`, `internal/chat/orchestrator.go`.

## 1. Цель и границы

### Эндпойнты

- `GET /api/chat/sessions/:id/messages` — история сессии с
  `sanitization_summary` (M3 этапа A).
- `GET /api/audit-events` — список с фильтрами и keyset-pagination.
- `GET /api/audit-events/:id` — полная запись.
- `POST /api/audit-events/export` — стрим CSV/NDJSON; сам аудитируется.
- `GET /api/incidents` — список с фильтрами.
- `GET /api/incidents/:id` — карточка + связанные audit-events.
- `POST /api/incidents` — manual («Сообщить о подозрении»).
- `PATCH /api/incidents/:id` — изменить статус/severity/assignee.
- `POST /api/incidents/:id/notes` — append-only заметка.

### Подсистемы

- **`internal/crypto/aesgcm`** — AES-256-GCM, env-ключ
  `MAPPING_ENCRYPTION_KEY` (32 байта base64). AAD = `sanitization_result_id`.
- **Шифрованная запись `pseudonym_mappings`** per-entity в Tx1
  оркестратора.
- **Авто-создание `incidents`** в оркестраторе после Tx2 для
  `decision ∈ {deny, escalate}` и `detail.response_leak_detected`.
- **`chat_messages.request_id`** (новая колонка) — коррелятор пары.
- **`LogValue() slog.Value`** для `PseudonymMap` — инвариант
  «никакого raw в логах» через слой типа (см. MAJOR-X в v2).

### Вне границ

- **Расшифровка mapping** (reveal-flow с дополнительным аудитом и
  approval-цепочкой) — пост-MVP.
- **Restore истории** по persisted mapping — пост-MVP, идёт вместе
  с reveal-flow.
- **Push-уведомления** новых инцидентов — polling 10s в UI (см.
  THREAT_MODEL §7 как принятое ограничение).
- **`POST /api/auth/dev-login`** — реализация в Итерации 12.
- **5-й статус `contained`** — БД (000006) имеет 4 статуса; **UI-spec
  `ui/incidents.md` правится** под БД (см. v2).
- **Manual без триггера** — `audit_event_id` в `incidents` REFERENCES
  без NOT NULL уже сейчас (000006), но в **MVP `POST /api/incidents`
  требует `audit_event_id`** (запрет manual без триггера). Это
  упрощает контракт `trigger`-поля.

## 2. Архитектурные решения

### Р1. AES-256-GCM для `pseudonym_mappings`

**Ключ.** Env `MAPPING_ENCRYPTION_KEY` — 32 байта в base64
(`base64.StdEncoding`). `config.Load` декодирует и проверяет длину 32;
**если переменная не задана или длина не 32 — сервис не стартует**
(fail-closed: без ключа Tx1 не сможет писать mappings, основной
поток встанет).

**API пакета `internal/crypto`:**

```go
type Cipher struct { gcm cipher.AEAD; rand io.Reader }
func NewCipher(key []byte) (*Cipher, error)             // len == 32
func (c *Cipher) Encrypt(plaintext, aad []byte) ([]byte, error)
func (c *Cipher) Decrypt(ciphertext, aad []byte) ([]byte, error)
```

- **`rand` — `crypto/rand.Reader`** (инжектируем для тестов
  детерминизма); ошибка `nil` → паника на старте (программная
  невозможность). Никакого `math/rand`.
- **Формат ciphertext:** `nonce(12) || ct || tag(16)`. Self-contained
  для расшифровки.
- **AAD = sanitization_result_id (16 байт uuid).** Связывает шифр с
  родительской записью — защищает от swap-атаки (если кто-то поменяет
  `raw_value_encrypted` двух разных entity местами в БД, GCM tag не
  совпадёт → ошибка decrypt). Стоимость — 5 строк кода (передать
  uuid-байты в `gcm.Seal/Open` как 4-й аргумент).

**Тесты Ф1 (TDD-red):**
1. `Encrypt`+`Decrypt` round-trip даёт исходный plaintext (с AAD).
2. Mismatched AAD при `Decrypt` → ошибка.
3. Tamper последнего байта ciphertext → GCM auth error.
4. `NewCipher` с key длины ≠ 32 → ошибка с явным сообщением.
5. Два `Encrypt` одного plaintext с одним AAD дают **разные**
   ciphertext (свежий nonce).

### Р2. Запись `pseudonym_mappings` — расширение Tx1

`storage.RecordChatRequest` (существует) принимает запись и
вставляет в `chat_messages`, `sanitization_results`,
`audit_events` в одной транзакции. **Расширение:**

```go
type PseudonymMappingInput struct {
    Pseudonym         string
    EntityType        string
    RawHash           string
    RawValueEncrypted []byte // готовый ciphertext (nonce||ct||tag)
}

// ChatRequestRecord получает новое поле:
type ChatRequestRecord struct {
    // ... существующие поля
    Mappings []PseudonymMappingInput
}
```

**Порядок выполнения (явно, закрывает MINOR-2 v1):**
1. **Вне Tx**: оркестратор формирует `[]PseudonymMappingInput`:
   для каждой entity из `preview.Entities` — `raw = pmap.toRaw[pseudonym]`,
   `ciphertext = cipher.Encrypt(raw, sanitizationResultIDPlaceholder)`.
   Поскольку `sanitization_result_id` известен **только после
   INSERT'a в `sanitization_results`** — есть тонкость с AAD.

   **Решение AAD-цикл-конфликта.** Полноценная защита от swap-атак
   требует уникального AAD **per-mapping** (не только per-session).
   Привязка к `sanitization_result_id` создаёт курицу-яйцо
   (id известен только после INSERT'а — двухфазный INSERT
   усложняет схему и нарушает append-only-инвариант).

   **AAD = SHA-256(session_id || pseudonym) — первые 16 байт.**
   - `session_id` (uuid) — известен до Tx1 (резолвится в `resolveSession`);
   - `pseudonym` (`ФИО_001` и т.п.) — известен из `preview.Entities`
     до Tx1, уникален per-sanitization_result;
   - SHA-256 над конкатенацией даёт AAD, **уникальный per-mapping**.

   Это закрывает класс swap-атак **внутри** одной сессии и **между**
   разными `sanitization_result`'ами одной сессии: tampering с
   `raw_value_encrypted` любых двух записей даст GCM auth error при
   попытке расшифровки. Стоимость — 1 sha256(~32+16 байт)/mapping
   (≈100 ns). Закрывает MINOR-M3 ревью v2.

2. **Внутри Tx**: INSERT `chat_messages` → INSERT `sanitization_results`
   → пакетный INSERT `pseudonym_mappings` (`COPY` или `unnest`-форма
   с одним SQL для N сущностей) → INSERT `audit_events`.
   Все INSERT'ы — на одном transaction-контексте.

3. **Сбой шифрования** на шаге 1 (теоретически невозможен после
   `config.Load`, но страховка) → возврат `chat_error` с
   `detail.stage = "encrypt_mappings"`; основной поток прерывается
   (fail-closed). Tx не открывается.

**Источник данных** (закрывает MINOR-1 v1): `PseudonymMappingInput[i]`
строится из пары `(preview.Entities[i], pmap.toRaw[entity.Pseudonym])`;
`PseudonymMap` остаётся узким (`map[string]string` pseudonym→raw),
расширять её под `type/hash` **не нужно** — эти поля уже есть в Entity.

### Р3. Audit-events API

#### Доступ (закрывает MAJOR-3 v1)

| Эндпойнт | Роли | Ограничение |
|----------|------|-------------|
| GET /api/audit-events | sec, comp, audit, admin | без огранич. |
| GET /api/audit-events | **developer** | **жёсткий фильтр** `user_id = current_user.id` И `event_type IN ('policy_tested')` |
| GET /api/audit-events/:id | sec, comp, audit, admin | без огранич. |
| GET /api/audit-events/:id | **developer** | проверка `user_id = current_user.id` И `event_type = 'policy_tested'` — иначе **404** (не раскрываем существование; закрывает MINOR-M2 ревью v2) |
| POST /api/audit-events/export | sec, comp, audit, admin | без огранич. |

`developer` получает доступ к **своим policy_tested** (для отладки
правил) и **только к ним**. Это согласовано с `ui/audit-log.md
§«Доступ»` ("developer — свои tests") — UI-spec в этом пункте
сохранён.

#### Фильтры GET /api/audit-events

Query-параметры:
- `from`, `to` — `RFC3339`.
- `user_id` — uuid.
- `event_type` — мультизначный (повторяющийся параметр).
- `policy_decision` — мультизначный.
- `risk_level` — мультизначный.
- `model_provider_id` — uuid.
- `has_leak` — `true`/`false` (SQL: `(detail->>'response_leak_detected') = 'true'`).
- `q` — `ILIKE` по `masked_payload`.
- `cursor` — base64-JSON `{"created_at":"...","id":"..."}`.
- `limit` — 1..200, дефолт 50.

#### Cursor-pagination (MINOR-4 v1)

**SQL форма зафиксирована** (row-comparison):

```sql
SELECT id, created_at, ...
FROM audit_events
WHERE
  ($cursor_created_at::timestamptz IS NULL
   OR (created_at, id) < ($cursor_created_at, $cursor_id::uuid))
  AND <фильтры>
ORDER BY created_at DESC, id DESC
LIMIT $limit + 1
```

Возврат N+1 → берём N, `next_cursor` строим из (N-1)-й строки.
**Запрещён** упрощённый `created_at < $1 AND id < $2` (даёт пропуски
строк с одинаковым `created_at`).

#### Export

`POST /api/audit-events/export` тело `{filters, format: "csv"|"ndjson",
include_payload: bool (default true)}`. Перед стримингом пишется
**audit-event `audit_exported`** с `detail.filters`, `detail.format`,
`detail.include_payload`. Стрим — chunks по 1000 строк через
`http.Flusher`. `Content-Disposition: attachment; filename="audit-
export-<timestamp>.<ext>"`.

### Р4. Incidents API + auto-create

#### Различение auto / manual (закрывает MAJOR-1 v1)

Миграция `000008` **добавляет**:

```sql
ALTER TABLE incidents
  ADD COLUMN reporter_id uuid REFERENCES users(id);

-- Partial unique: для одного audit_event_id может существовать
-- НЕ БОЛЕЕ ОДНОГО auto-инцидента (reporter_id IS NULL). Manual
-- может создаваться сверху (reporter_id IS NOT NULL).
CREATE UNIQUE INDEX idx_incidents_one_auto_per_event
  ON incidents(audit_event_id)
  WHERE reporter_id IS NULL;
```

`reporter_id`:
- `NULL` — auto (создан системой); **UI badge «Reporter: system (auto)»**.
- `<uuid>` — manual; ссылается на `users` записавшего.

`user_id` (существующее поле): «субъект, кого касается инцидент»
(носитель роли user из chat_session — например, тот, чей запрос
дал deny).

#### Авто-создание (в оркестраторе)

После успешного коммита Tx2 (`RecordChatTermination`):

```
trigger := ""
switch {
case outcome.Decision == "deny":            trigger = "deny"
case outcome.Decision == "escalate":        trigger = "escalate"
case detail["response_leak_detected"] == true: trigger = "response_leak_detected"
}
if trigger != "" {
    sev := severityFor(preview.Risk.Level, trigger)
    inc, err := store.CreateAutoIncident(ctx, IncidentInput{
        AuditEventID: terminationIDs.AuditEventID,
        UserID:       req.UserID,
        ReporterID:   nil, // auto
        Severity:     sev,
        Status:       "open",
        Title:        autoIncidentTitle(trigger, preview.Risk.Level),
        Summary:      buildSummary(preview, outcome, detail),
    }, AuditEvent{ // см. ниже Р4.Atomic Tx3
        UserID:    req.UserID,
        EventType: "incident_created_auto",
        Detail: map[string]any{
            "request_id":     req.RequestID,
            "trigger":        trigger,
            "audit_event_id": terminationIDs.AuditEventID,
        },
    })
    if err != nil {
        // Partial unique нарушен → инцидент уже есть → ничего, OK.
        // Иная ошибка → фиксируем как incident_create_failed
        // (отдельный event-тип, НЕ chat_error — см. ниже).
        if !errors.Is(err, storage.ErrIncidentAutoDuplicate) {
            o.recordAuditEvent(ctx, o.incidentCreateFailedEvent(
                req, terminationIDs.AuditEventID, trigger, err))
        }
    }
    // Успех: incident + audit-event записаны атомарно (Tx3) — отдельный
    // recordAuditEvent НЕ нужен. См. ниже §«Atomic Tx3».
}
```

**Что нового по сравнению с v1:**

1. **Partial unique constraint в БД** (race-safe; см. выше) — code-check
   тоже остаётся для понятных error-messages, но **БД авторитетна**.
2. **`incident_created_auto` audit-event** — закрывает дыру инварианта
   «всё аудируется» (создание инцидента было невидимо в журнале).
3. **`incident_create_failed`** — отдельный event-тип (НЕ `chat_error`).
   Семантика: «чат прошёл, audit записан, downstream-обработка
   incident-create упала». `detail.audit_event_id`, `detail.trigger`,
   `detail.error`.

**Atomic Tx3 (закрывает MINOR-M4 ревью v2).** `CreateAutoIncident`
принимает **обе** структуры (`IncidentInput` + `AuditEvent`) и
выполняет в одной транзакции:

```
BEGIN
  INSERT INTO incidents (...) RETURNING id
  INSERT INTO audit_events (event_type='incident_created_auto', ...)
COMMIT
```

Если любой из INSERT'ов падает — Tx3 откатывается полностью. Это
устраняет окно «incident создан, audit не записан». Соответствует
существующему паттерну `RecordChatRequest`/`RecordChatTermination`.
Аналогично `CreateManualIncident` записывает `incident_created_manual`
в той же Tx.

`o.recordAuditEvent(...)` (с `withDetachedTimeout`) **остаётся
валидным** только для **error-веток** (`incident_create_failed`):
основная Tx3 уже откатилась, audit-event пишется отдельно.

#### severityFor(risk_level, trigger)

| risk_level / trigger | deny | escalate | response_leak_detected |
|---|---|---|---|
| low | low | low | **high** |
| medium | medium | medium | **critical** |
| high | high | high | critical |
| critical | critical | critical | critical |

(Leak — компрометация маскирования: модель воспроизвела сырое
значение, **которого ей не давали**. Это означает либо баг детектора
sanitizer (false-negative), либо jailbreak. На любом уровне это
**серьёзнее обычного deny**: deny — «политика сработала», leak —
«политика сработала, но что-то всё равно проскочило». Поэтому low/
medium-leak повышаются сразу на две ступени, high/critical остаются
critical. Закрывает MINOR-M5 ревью v2.)

#### POST /api/incidents (manual)

Доступ: sec, admin. Тело:
```json
{
  "audit_event_id": "uuid",   // обязательный (MVP — без триггера нельзя)
  "severity": "high",
  "title": "Подозрение на инсайдер",
  "summary": "string?"
}
```
Идемпотентность MVP: если за последние 60 секунд тот же
`reporter_id+audit_event_id` создал incident — возвращаем
существующий (HTTP 200, не 201). Полноценный `Idempotency-Key` —
пост-MVP.

Audit: `incident_created_manual`.

#### PATCH /api/incidents/:id

Доступ: sec, admin. Optimistic concurrency через `If-Match`
header — содержит `updated_at` в формате RFC3339Nano. **Коды
ответов (закрывает MINOR-8 v1; RFC 7232):**

- `200` — успех;
- `204` — успех без тела;
- **`428` Precondition Required** — header отсутствует;
- **`412` Precondition Failed** — `If-Match` не совпал с текущим
  `updated_at` (другой пользователь только что изменил).

UI-spec `ui/incidents.md` правится: «PATCH conflict 409» → «412».

**Что меняется:** `status`, `severity`, `assignee_id`, `resolution`.
**`resolved`/`false_positive` → требуется непустой `resolution`**.
Каждое изменение — отдельный audit-event с `detail.field`,
`detail.from`, `detail.to`:
- `incident_status_changed`,
- `incident_severity_changed`,
- `incident_assigned`,
- `incident_resolved` (когда status=resolved или false_positive).

**Atomic PATCH (закрывает MINOR-M6 ревью v2).** UPDATE + N audit-events
выполняются **в одной транзакции** через `storage.PatchIncident(ctx,
id, patch, expectedUpdatedAt, audits []AuditEvent)`:

```sql
BEGIN
  UPDATE incidents
    SET <columns>, updated_at = now()
    WHERE id = $id AND updated_at = $expected
    RETURNING id, updated_at;            -- 0 rows → 412
  INSERT INTO audit_events (...) [N раз]
COMMIT
```

Если `RETURNING` дал 0 строк (concurrent edit изменил `updated_at`)
→ `storage.ErrIncidentConflict` → handler возвращает 412. Audit-events
не вставляются. Сравнение через равенство `updated_at = $expected`
устойчиво к µs-точности — `If-Match` приходит из предыдущего GET в
RFC3339Nano, в БД хранится как timestamptz, обратная сериализация
детерминирована.

#### Чтение `incident_notes` — матрица прав (MINOR-M7)

| Роль | Чтение notes (GET /api/incidents/:id) | Запись POST /:id/notes |
|------|---------------------------------------|------------------------|
| security_officer | да | да |
| admin | да | да |
| compliance_officer | да | нет |
| auditor | да | нет |
| assignee (любой) | да | да |
| developer / user | нет (нет доступа к /api/incidents) | нет |

#### POST /api/incidents/:id/notes

Доступ: sec, admin, **assignee**. Тело `{content: 1..2000 chars}`.
Append-only (UPDATE/DELETE заблокировано триггером БД — как у
`audit_events`). Audit: `incident_note_added`.

#### GET /api/incidents/:id — связанные audit-events

JOIN `audit_events ON audit_events.id = incidents.audit_event_id`
для триггерного события + LATERAL SUBQUERY для других events этого
session_id (если incident — по chat). Это уже **на втором уровне
сложности**, без блокировки реализации — детали SQL в Ф4.

### Р5. История сессии `GET /api/chat/sessions/:id/messages`

Доступ: владелец сессии (`chat_sessions.user_id == current_user.id`).
Возвращает `ChatMessageList` (см. `chat.schema.json#ChatMessageList`).

#### Whitelist полей (закрывает MINOR-7 v1)

SQL возвращает `sanitization_results.entities jsonb`. Этот jsonb
**содержит `start`, `end`** (хранится по схеме Итерации 4). Эти
поля **не должны** утечь в API.

**Реализация (явная):**
1. SQL читает `sanitization_results.entities` как `[]json.RawMessage`.
2. Go проходит по слайсу, разбирает каждый entity в **строгую
   локальную DTO** с белым списком полей:
   ```go
   type historyEntityDTO struct {
       Type      string `json:"type"`
       Category  string `json:"category"`
       Pseudonym string `json:"pseudonym"`
       RawHash   string `json:"raw_hash"`
   }
   ```
3. JSON-сериализация только этого DTO. Поля `start`/`end`
   физически невозможно случайно вернуть — их нет в типе.

**Тест Ф4 (явный, обязательный):**
- Засеять `sanitization_results.entities` с `start=10, end=15`.
- Запросить `GET /api/chat/sessions/:id/messages`.
- Проверить, что **в JSON-ответе нет ключей `"start"` и `"end"`** ни
  на одном уровне `sanitization_summary.entities[].*`.

### Р6. Колонка `chat_messages.request_id`

Миграция `000008`: `ALTER TABLE chat_messages ADD COLUMN request_id text`.
Nullable (старые сообщения существующих сессий — `NULL`; контракт
`ChatMessage.request_id: ["string", "null"]` это допускает).

`storage.RecordChatRequest` пишет `request_id` для user-message;
`storage.RecordChatTermination` — для assistant-message. **Обе
записи имеют один и тот же `request_id`** (из `req.RequestID`).

**Тест Ф2 (явный, закрывает MINOR-5 v1):**
- Создать сессию, выполнить `RecordChatRequest`+`RecordChatTermination`
  с `req.RequestID = "r-1"`.
- Прочитать `chat_messages` сессии: оба сообщения должны иметь
  `request_id = "r-1"`.
- В `audit_events.detail.request_id` для пары — также `"r-1"`.

### Р7. Инвариант «никакого raw в логах» — `LogValuer`

Закрывает критический пропуск v1. `PseudonymMap` хранит raw в памяти
во время запроса (`map[string]string` pseudonym → raw). Любой
случайный `slog.Info("...", "pmap", pmap)` сольёт сырые ПДн.

**Реализация:** `PseudonymMap` реализует `slog.LogValuer`:

```go
// LogValue гарантирует, что raw-значения никогда не попадают в логи.
// Возвращает только агрегированное число записей.
func (m PseudonymMap) LogValue() slog.Value {
    return slog.GroupValue(
        slog.Int("entries", len(m.toRaw)),
        slog.String("redacted", "raw values redacted by design"),
    )
}
```

Аналогично — для `PseudonymMappingInput` (поле
`RawValueEncrypted` — это `[]byte` шифротекст, не страшен, но для
единообразия LogValuer возвращает agg-инфо без content).

Покрытие в `slog.Default()` ленивое: достаточно, что **тип реализует
`LogValuer`** — slog вызовет `LogValue()` сам.

**Тест Ф3 (явный, обязательный):**
- Создать `PseudonymMap` с raw-значениями.
- Залогировать через `slog.New(slog.NewJSONHandler(buf, nil)).Info("...", "pmap", pmap)`.
- Парсить вывод и убедиться, что raw-значений в нём **нет**, а есть
  только `"entries": N` и `"redacted": "..."`.

### Р8. Контракты

#### `docs/contracts/audit.schema.json`

`$defs`:
- `AuditEventType` — enum (MVP, расширяемый):
  - `chat_request`, `chat_response`, `chat_blocked`, `chat_error`;
  - `policy_tested`;
  - `model_created`, `model_updated`, `model_disabled`, `model_deleted`;
  - `incident_created_auto`, `incident_created_manual`,
    `incident_status_changed`, `incident_severity_changed`,
    `incident_assigned`, `incident_resolved`, `incident_note_added`;
  - `incident_create_failed`;
  - `audit_exported`;
  - `auth_login`, `auth_login_failed` (для Итерации 12).

  Закрывает MAJOR-2 v1. Список **открыт к расширению** в следующих
  итерациях — но MVP-набор зафиксирован, фронт может строить
  фильтры и chip-маппинг.

- `AuditEventSummary` — облегчённое для list.
- `AuditEventDetail` — полная запись.
- `AuditEventList` — `{events, next_cursor}`.
- `AuditExportRequest` — `{filters, format, include_payload}`.

#### `docs/contracts/incidents.schema.json`

`$defs`:
- `IncidentTrigger` enum: `deny`, `escalate`, `response_leak_detected`,
  `manual`. Поле computed: для auto — из `detail.trigger` ассоциированного
  audit-event; для manual — `manual` (т.к. `reporter_id IS NOT NULL`).
  **Для будущих manual без audit_event** — `["string", "null"]`, но в
  MVP `audit_event_id` обязателен в `POST /api/incidents` (см. Р4) →
  `trigger` всегда строка.
- `Incident` — полные поля.
- `IncidentList` — `{incidents, next_cursor}`.
- `IncidentPatch` — `{status?, severity?, assignee_id?, resolution?}`.
- `IncidentCreate` — `{audit_event_id, severity, title, summary?}`.
- `IncidentNote` / `IncidentNoteCreate`.

`additionalProperties: false` везде.

### Р9. Согласование UI-spec под БД (правка артефактов этапа A)

В рамках Итерации 9 правится:

- **`docs/design/ui/incidents.md`**:
  - Убрать статус `contained` из mockup'а и таблицы статусов
    (БД-схема 000006 имеет 4 статуса; добавление 5-го — пост-MVP).
  - PATCH conflict код **409 → 412** (RFC 7232), плюс **428** при
    отсутствии If-Match. Текст состояния в таблице состояний.
  - Дописать «Reporter: system (auto) / username (manual)» — это
    уже есть в mockup'е, но добавить пояснение в §«Структура».
  - Добавить tooltip «Заметки нельзя редактировать — добавьте
    отменяющую запись» (append-only).
- **`docs/design/ui/audit-log.md`**:
  - Без изменений в части developer-доступа (план Итерации 9
    реализует «свои policy_tested» — `ui-spec` уже это говорит).

## 3. Миграция `000008`

```sql
-- 000008_audit_incidents_indexes_and_notes.up.sql

-- 1. Индексы для фильтров audit_events.
CREATE INDEX idx_audit_events_decision
  ON audit_events(policy_decision)
  WHERE policy_decision IS NOT NULL;
CREATE INDEX idx_audit_events_provider_created
  ON audit_events(model_provider_id, created_at)
  WHERE model_provider_id IS NOT NULL;
CREATE INDEX idx_audit_events_risk_level
  ON audit_events(risk_level)
  WHERE risk_level IS NOT NULL;
CREATE INDEX idx_audit_events_detail_gin
  ON audit_events USING GIN (detail);

-- 2. Колонка request_id в chat_messages.
ALTER TABLE chat_messages ADD COLUMN request_id text;
CREATE INDEX idx_chat_messages_request_id
  ON chat_messages(request_id)
  WHERE request_id IS NOT NULL;

-- 3. Доп. поля incidents.
ALTER TABLE incidents
  ADD COLUMN reporter_id uuid REFERENCES users(id),
  ADD COLUMN assignee_id uuid REFERENCES users(id),
  ADD COLUMN closed_at   timestamptz;

CREATE INDEX idx_incidents_severity ON incidents(severity);
CREATE INDEX idx_incidents_assignee
  ON incidents(assignee_id)
  WHERE assignee_id IS NOT NULL;

-- Partial unique: один auto-incident на audit_event (race-safe).
CREATE UNIQUE INDEX idx_incidents_one_auto_per_event
  ON incidents(audit_event_id)
  WHERE reporter_id IS NULL;

-- CHECK: новый incident всегда status='open' (resolved/false_positive
-- допустимы только через UPDATE — проверяется приложением;
-- БД-CHECK на INSERT нерациональна, т.к. не выразить «при INSERT»).
-- Вместо этого — проверка в storage.CreateIncident +
-- триггер для closed_at (см. ниже).

-- Триггер для closed_at при resolved/false_positive.
CREATE OR REPLACE FUNCTION incidents_set_closed_at()
RETURNS trigger AS $$
BEGIN
  IF NEW.status IN ('resolved', 'false_positive')
     AND OLD.status NOT IN ('resolved', 'false_positive') THEN
    NEW.closed_at := now();
  ELSIF NEW.status NOT IN ('resolved', 'false_positive')
        AND OLD.status IN ('resolved', 'false_positive') THEN
    NEW.closed_at := NULL;
  END IF;
  RETURN NEW;
END $$ LANGUAGE plpgsql;

CREATE TRIGGER incidents_closed_at BEFORE UPDATE ON incidents
  FOR EACH ROW EXECUTE FUNCTION incidents_set_closed_at();

-- 4. Таблица incident_notes (append-only).
CREATE TABLE incident_notes (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  incident_id uuid NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  author_id   uuid NOT NULL REFERENCES users(id),
  content     text NOT NULL CHECK (char_length(content) BETWEEN 1 AND 2000),
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_incident_notes_incident ON incident_notes(incident_id);

CREATE TRIGGER incident_notes_append_only
  BEFORE UPDATE OR DELETE ON incident_notes
  FOR EACH ROW EXECUTE FUNCTION rubezh_block_mutation();
```

`down.sql` — DROP в обратном порядке.

## 4. Файлы и пакеты

| Файл | Назначение | Бюджет (строк) |
|------|------------|----------------|
| `internal/crypto/aesgcm.go` | Cipher + Encrypt/Decrypt + AAD | ~100 |
| `internal/crypto/aesgcm_test.go` | unit (5 кейсов + AAD) | ~160 |
| `internal/storage/mapping.go` | insert per-tx (батч `unnest`) | ~120 |
| `internal/storage/mapping_test.go` | integration | ~150 |
| `internal/storage/incidents.go` | Create/Get/List/Patch/AddNote | ~300 |
| `internal/storage/incidents_test.go` | integration | ~300 |
| `internal/storage/audit.go` | + List/Get с фильтрами и keyset | ~270 |
| `internal/storage/audit_test.go` | расширение | ~280 |
| `internal/storage/chat.go` | + ListMessages с sanitization | ~280 |
| `internal/storage/chat_test.go` | расширение | ~220 |
| `internal/api/audit.go` | 3 handler | ~300 |
| `internal/api/audit_test.go` | http + filters + cursor + export | ~280 |
| `internal/api/incidents.go` | 5 handler | ~350 |
| `internal/api/incidents_test.go` | http + 412 + 428 | ~280 |
| `internal/api/history.go` | 1 handler | ~120 |
| `internal/api/history_test.go` | http + защита start/end | ~150 |
| `internal/chat/auto_incident.go` | severityFor + сборка | ~100 |
| `internal/chat/orchestrator.go` | расширение | +60 |
| `internal/chat/pseudonym.go` | +LogValue() метод | +25 |
| `internal/chat/pseudonym_test.go` | +тест LogValue() | +40 |
| `internal/chat/types.go` | расширение Store interface | +25 |
| `internal/config/config.go` | `MAPPING_ENCRYPTION_KEY` | +25 |
| `cmd/rubezh-api/main.go` | проброс cipher | +20 |
| `migrations/000008_*.up/down.sql` | миграция | ~90 / 40 |
| `docs/contracts/audit.schema.json` | контракт | ~170 |
| `docs/contracts/incidents.schema.json` | контракт | ~180 |
| `docs/design/ui/incidents.md` | правки v9 | +30 |
| `docs/THREAT_MODEL.md` | §7 новые риски | +40 |
| `docs/PLAN.md` / `CLAUDE.md` | финал | в конце |

Все файлы ≤500 строк, функции ≤60.

## 5. План по фазам (TDD: тест-коммит → реализация-коммит)

- **Ф1.** `internal/crypto`:
  - тесты red (5 кейсов + AAD);
  - реализация green;
  - `cmd/rubezh-api/main.go` строит `*Cipher` из `cfg.MappingEncryptionKey`.

- **Ф2.** Миграция `000008` + storage:
  - применение миграции (тест на наличие таблиц/колонок/индексов);
  - `storage.mapping.go` — insert per-tx;
  - `storage.incidents.go` — Create/Get/List/Patch/AddNote, проверка
    partial-unique race;
  - `storage.audit.go` — List/Get с фильтрами и keyset (row-comparison
    SQL форма);
  - `storage.chat.go` — ListMessages с JOIN;
  - тесты red → реализация green;
  - проверка append-only `incident_notes` (UPDATE/DELETE rejected).

- **Ф3.** Оркестратор:
  - `internal/chat/pseudonym.go` — `LogValue()` метод + тест на
    redaction;
  - `internal/chat/auto_incident.go` — `severityFor`, `autoIncidentTitle`,
    `buildSummary`;
  - расширение Tx1 mappings (с шифрованием AAD=session_id) —
    добавить mock cipher в тестах;
  - вызов CreateAutoIncident после Tx2 — fakeStore;
  - audit-event `incident_created_auto` пишется на успех;
  - audit-event `incident_create_failed` пишется на сбой (отдельный
    event-тип, НЕ chat_error);
  - тесты red → реализация green;
  - проверка: при сбое CreateAutoIncident основной поток не валится;
    SSE done всё равно отправляется (audit_response уже есть, чат
    дошёл до пользователя).

- **Ф4.** HTTP:
  - контракты `audit.schema.json`, `incidents.schema.json` — JSON
    Schema валидация структуры;
  - `internal/api/audit.go`, `incidents.go`, `history.go` — handler'ы;
  - role-permission тесты (auditor RO, developer scoped, manual только
    sec/admin);
  - cursor-pagination тесты (стабильность к INSERT'ам);
  - PATCH тесты (412 Precondition Failed, 428 Precondition Required);
  - history тест — отсутствие start/end в JSON-ответе;
  - export тест (streaming, audit-event `audit_exported`);
  - реализация green;
  - QA-агент: функциональные тесты (создать сессию → отправить
    запрос с deny → проверить incident создан + 2 audit-event'а
    (`chat_blocked`, `incident_created_auto`) → PATCH → note →
    audit-log фильтрация).

Одно итоговое ревью архитектора по завершении Ф1–Ф4; доводка до 10/10.

## 6. Согласование с существующими типами

- `chat.Orchestrator` — конструктор получает `*crypto.Cipher`;
  `cmd/rubezh-api/main.go` строит cipher из `cfg.MappingEncryptionKey`.
- `chat.Store` — интерфейс расширяется:
  - `RecordChatRequest(ctx, rec)` — `rec` теперь содержит `Mappings []PseudonymMappingInput`;
  - новые: `CreateAutoIncident`, `CreateManualIncident`, `ListIncidents`,
    `GetIncident`, `PatchIncident` (плюс `ListAuditEvents`, `GetAuditEvent`,
    `ListChatMessages`).
- `storage.ErrIncidentAutoDuplicate` — новая sentinel-ошибка для
  partial-unique нарушения (распознаётся в оркестраторе как «уже
  есть, всё ОК»).
- `config.Config` — новое поле `MappingEncryptionKey []byte` (32 байта,
  декодированный из base64; ошибка `Load` если длина ≠ 32 или env пустой).
- `auth` — без изменений.

## 7. Изменения в `docs/THREAT_MODEL.md §7 (новые остаточные риски Итерации 9)`

1. **Без расшифровки mapping (в MVP).** Запись зашифрована и хранится;
   чтение в forensics — пост-MVP (требует reveal-эндпойнта с
   approval-flow и audit `mapping_revealed`).
2. **Ротация ключа не поддерживается.** Смена `MAPPING_ENCRYPTION_KEY`
   делает все ранее зашифрованные mappings нечитаемыми. Процедура
   key-rotation (двойное чтение, миграция) — пост-MVP, идёт вместе с
   reveal-flow.
3. **Idempotency manual-инцидентов — упрощённая.** В MVP проверка
   «дубль за 60 секунд по `audit_event_id+reporter_id`»; полноценный
   `Idempotency-Key` — пост-MVP.
4. **Push-уведомления о новых инцидентах отсутствуют.** UI polling 10s;
   `severity=critical` имеет до 10 секунд задержки визуализации.
5. **`incident_notes` append-only.** Исправления невозможны — ошибочную
   заметку нужно дополнить новой. Это сделано намеренно: forensics-
   цепочка действий следователя не должна редактироваться.
6. **`assignee_id` семантически = роль в MVP.** `users` содержит одного
   dev-пользователя на роль (миграция 000007); поэтому «Назначить мне»
   назначает роль, не конкретного человека. Реальное назначение
   появляется с OIDC (см. иттерация 8 остаточный риск).
7. **PATCH 412 при concurrent edit** возможен при ровно совпадающем
   `updated_at` (µs-точность); вероятность мала (<10⁻⁶), но не нулевая.
   Полноценный `version int` — пост-MVP.
8. **Manual без `audit_event_id` не поддерживается в MVP** (закрывает
   MINOR-M1 ревью v2). Инвариант поддерживается **в коде**
   (handler `POST /api/incidents` требует поле); CHECK constraint в БД
   не добавлен ради совместимости с будущим reveal-flow (где manual
   без триггера может стать допустимым). Compromise зафиксирован
   осознанно.
9. **Сбой шифрования mapping'а в Tx1** → пользовательский запрос
   теряется (chat_error, ничего не записано в `chat_messages`).
   Это **сознательное fail-closed-решение**: лучше потерять запрос,
   чем сохранить его с пропавшим mapping'ом (forensics невозможен).

## 8. Ответы на вопросы архитектора (v1 ревью, §«Открытые»)

1. **`auditor` для export** — да (read-only download). Каждый export
   аудитируется (`audit_exported`).
2. **PATCH conflict** — `If-Match: <updated_at>`, ответы 428/412
   (RFC 7232); см. Р4.
3. **`masked_payload` в Export** — по умолчанию **включаем**; флаг
   `include_payload` в `AuditExportRequest` для отключения.

## 9. Дельта v2 → v2.1 (закрытие 7 MINOR'ов ревью v2)

| # | Закрыто в | Суть |
|---|-----------|------|
| M1 | §7 #8 | Инвариант «manual ⇒ audit_event_id» поддерживается в коде; зафиксирован как принятое MVP-ограничение |
| M2 | §Р3 | GET :id для developer вне scope → 404 (не 403) — RFC 7231, нераскрытие |
| M3 | §Р1 AAD | AAD = SHA-256(session_id ‖ pseudonym)[:16] — защита от swap внутри сессии |
| M4 | §Р4 Atomic Tx3 | `CreateAutoIncident` принимает `(IncidentInput, AuditEvent)`, INSERT incidents+audit_events в одной Tx |
| M5 | §Р4 severityFor | low/leak → high (не medium); medium/leak → critical (не high). Leak — компрометация |
| M6 | §Р4 Atomic PATCH | `PatchIncident` принимает `(id, patch, expected, audits[])`, UPDATE+audit_events в одной Tx; 0 affected → 412 |
| M7 | §Р4 матрица notes | RW: sec/admin/assignee; RO: comp/audit; нет: developer/user |

## 10. Самооценка v2.1: 9.8/10

Закрыты все 3 MAJOR'а + 8 MINOR'ов v1 + добавлен критический инвариант
«никакого raw в логах» через `LogValuer`. AAD в AES-GCM добавлен.
Авто-инциденты теперь оставляют audit-след (`incident_created_auto`).
Race-safety инвариант auto-incident'а вынесен на уровень БД (partial
unique). Конфликт PATCH соответствует RFC 7232. UI-spec будет
синхронизирован с БД в рамках Ф4.

Минус 0.3 — реалистичная оценка плотности итерации: 9 эндпойнтов + 3
контракта + миграция + 30+ тестов — это около 6 часов фокусной работы.
Однако приоритет качества выше скорости, потому план не сокращён.

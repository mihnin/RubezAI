# Рубеж ИИ — Живой план реализации

> **Рабочий документ.** Обновляется в конце каждой итерации.
> Зачёркнутый пункт = реализован **И** подтверждён независимым архитектором с оценкой **≥ 9.5/10** (цель — 10/10).

## Легенда статусов

- `☐` — не начато
- `🔄` — в работе
- `✅` — готово, архитектор подтвердил (≥ 9.5/10) → заголовок пункта зачёркивается
- `⚠️` — готово, но архитектор вернул на доработку (< 9.5/10)

## Архитектурная оценка плана: **9.5 / 10**

(после доработки первой версии 8/10 — см. историю в первом ответе ассистента)

## Зафиксированные решения

| # | Решение | Обоснование |
|---|---------|-------------|
| 1 | Go-сервис собирается и тестируется только в Docker | Локальный Go SDK не установлен; multi-stage сборка даёт воспроизводимость для on-prem |
| 2 | Frontend-роутер — React Router v7 | Зрелый, простой, достаточный для 6 экранов MVP |
| 3 | LLM-streaming — SSE, не WebSocket | Поток токенов однонаправленный; SSE проще, авто-reconnect, дружелюбен к reverse-proxy, легко аудируется |
| 4 | Очередь задач worker'а — на PostgreSQL (`FOR UPDATE SKIP LOCKED`) | Для MVP-объёмов достаточно; ноль лишней инфраструктуры (без Redis/Kafka/NATS) |
| 5 | Policy Engine — пакет Go (`internal/policy`) | Детерминированное решение в синхронном пути запроса |
| 6 | `pseudonym_mappings` шифруется с итерации 4 (AES-GCM) | Обратимая псевдонимизация — актив утечки |
| 7 | Python — 3.12 в контейнерах (локально 3.14) | Совместимость wheels (pydantic-core и др.) |
| 8 | Python lock — `uv` + `uv.lock` | Зрелый стандарт, хеши, воспроизводимость |
| 9 | Контракты Go↔Python — JSON Schema в `docs/contracts/` | Защита от дрейфа контрактов, контрактные тесты |
| 10 | Enum режимов доверия моделей зафиксирован заранее (4 значения), MVP реализует подмножество | Стабильность контракта; LLM Router MVP — mock + OpenAI-совместимый адаптер |

## Карта итераций → критерии готовности MVP

| Критерий MVP | Закрывается в итерации |
|---|---|
| 1. Поднимается через Docker Compose | прогрессивно, финал — 16 |
| 2. Frontend открывается локально | 12 |
| 3. Backend healthcheck работает | 5 |
| 4. PostgreSQL миграции применяются | 1 |
| 5. Можно отправить chat-запрос | 8 |
| 6. Можно загрузить документ | 10 |
| 7. Sanitizer находит и маскирует ПДн/секреты | 2, 3, 4 |
| 8. Policy engine принимает решение | 6 |
| 9. Mock LLM возвращает ответ | 7 |
| 10. Audit event создаётся | 9 |
| 11. Incident создаётся при deny | 9 |
| 12. Базовые unit/integration tests | каждая итерация |
| 13. Все зависимости зафиксированы | 0 и далее каждая |
| 14. Нет файлов > 500 строк | каждая итерация (проверка) |
| 15. README объясняет запуск и архитектуру | 0, финал — 16 |

---

## Итерации

### ~~Итерация 0 — Скелет репозитория~~ ✅ Принято 9.5/10

- **Цель:** документация, инфраструктурный `docker-compose`, конвенции, живой план. Без бизнес-логики.
- **Файлы:** `docs/{ARCHITECTURE,THREAT_MODEL,SECURITY_CHECKLIST,API}.md`, `docs/contracts/*.schema.json`, `docs/PLAN.md`, `docker-compose.yml`, `.env.example`, `Makefile`, `make.ps1`, `CLAUDE.md`, `README.md`, `.gitignore`.
- **Тесты:** `docker compose config` валиден; `docker compose up -d postgres minio` поднимается; healthcheck'и зелёные.
- **Закрывает критерии:** частично 1, 15.
- **Самооценка:** 9/10 — скелет полон, инфраструктура поднимается, контракты валидны и однозначны после доработки.
- **Архитектор:** ревью 1 — 8/10 «на доработку»; замечания устранены; ревью 2 — **9.5/10 ПОДТВЕРЖДЕНО**.
- **Что улучшить:** строгость JSON Schema (инвариант границ спана, пересмотр корневого `oneOf`) — перенесено в Итерацию 4.

### ~~Итерация 1 — Схема БД и миграции~~ ✅ Принято 9/10

- **Цель:** миграции всех MVP-сущностей (источник истины по списку — `docs/ARCHITECTURE.md` §9), расширение `pgvector`, миграционный контейнер в Compose.
- **Файлы:** `rubezh-api/migrations/*.sql`, обновление `docker-compose.yml` (сервис `migrate`).
- **Тесты:** миграции применяются и откатываются; `audit_events` append-only (триггер запрета UPDATE/DELETE); все таблицы имеют `created_at`/`updated_at` где применимо.
- **Закрывает критерии:** 4.
- **Самооценка:** 9/10.
- **Архитектор:** ревью 1 — 7.5/10 «на доработку» (3 MAJOR: каскад уничтожал forensics, аудит без версии политики, доказуемость TDD); устранено; ревью 2 — **9/10 ПОДТВЕРЖДЕНО**.

### ~~Итерация 2 — Sanitizer: детекторы ПДн~~ ✅ Принято 9/10

- **Цель:** regex-детекторы (ФИО, телефон, email, паспорт, СНИЛС, ИНН, КПП, ОГРН, БИК, счёт), FastAPI-скелет, `/health`.
- **Файлы:** `rubezh-sanitizer/app/{detectors,domain,api}/`, `tests/`, `pyproject.toml`, `uv.lock`, `Dockerfile`.
- **Тесты (TDD):** unit-тесты на каждый regex-детектор — корректные спаны и типы, валидация контрольных сумм (ИНН/СНИЛС).
- **Закрывает критерии:** частично 7.
- **Самооценка:** 9/10.
- **Архитектор:** ревью 1 — 7.5/10 (3 MAJOR в детекторах); ревью 2 — 8.5/10 (MINOR-N1, КПП региона 04); ревью 3 — **9/10 ПОДТВЕРЖДЕНО**.

### ~~Итерация 3 — Sanitizer: секреты и коммерческие данные~~ ✅ Принято 9.6/10

- **Цель:** детекторы секретов (API keys, JWT, OAuth, пароли, DSN, connection strings), коммерчески чувствительных данных, risk scoring.
- **Тесты (TDD):** unit-тесты детекторов; тест на отсутствие raw-секретов в логах.
- **Закрывает критерии:** частично 7.
- **Самооценка:** 9.6/10.
- **Архитектор:** ревью 1 — **9.6/10 ПОДТВЕРЖДЕНО** (5 MINOR перенесены в Итерацию 4). QA-агент усилил покрытие до 108 тестов; добавлен CI (GitHub Actions).

### ~~Итерация 4 — Sanitizer: маскирование, NER-интерфейс, `/sanitize/preview`~~ ✅ Принято 9.6/10

- **Цель:** обратимая псевдонимизация (`ФИО_001`, `ДОГОВОР_014`) с таблицей маппинга, NER/LLM-review-интерфейс с mock (фильтр 2/3 — малая русскоязычная LLM, подключается позже), эндпойнт `/sanitize/preview`, шифрование mapping'ов (AES-GCM).
- **Тесты (TDD):** round-trip псевдонимизации; контрактный тест по `docs/contracts/sanitize.schema.json`.
- **Перенесено из ревью Итерации 0:** добавить в `sanitize.schema.json` инвариант границ спана (`start < end`, `end ≤ len(text)`); пересмотреть корневой `oneOf` в пользу ссылок на конкретные `$defs` из контрактных тестов.
- **Перенесено из ревью Итерации 3:** ограничить захват значения пароля (`[^\s;'"]`, MINOR-1 — до маскирования); тесты «known limitation» границ спанов amount/contract (MINOR-2/3); вынести поле `detector` из хардкода (MINOR-5); сквозной контрактный тест ответа `/sanitize/preview` против `SanitizeResponse` (MINOR-4).
- **Закрывает критерии:** 7.
- **Самооценка:** 9.6/10.
- **Архитектор:** ревью 1 — **9.6/10 ПОДТВЕРЖДЕНО**. QA-агент нашёл и помог устранить 2 бага (каскадная подмена в `restore`, потеря сущностей в цепочке пересечений). 4 MINOR → бэклог.

### ~~Итерация 5 — Go API: скелет~~ ✅ Принято 9.6/10

- **Цель:** `config`, structured logging (`slog`), `/health`, storage-слой (`pgx`), роутер `chi`, auth-middleware с ролями.
- **Файлы:** `rubezh-api/cmd/`, `rubezh-api/internal/{config,api,auth,storage}/`, `go.mod`, `go.sum`, `Dockerfile`.
- **Тесты:** unit-тесты config/auth; healthcheck-тест.
- **Закрывает критерии:** 3.
- **Самооценка:** 9.6/10.
- **Архитектор:** ревью 1 — **9.6/10 ПОДТВЕРЖДЕНО**. QA-агент усилил тесты безопасности auth (подмена роли, подделка подписи, edge-cases Bearer). 4 MINOR → бэклог.

### ~~Итерация 6 — Go: Policy Engine~~ ✅ Принято 10/10

- **Цель:** `internal/policy`, движок решений, эндпойнты `/api/policies`, `/api/policies/test`.
- **Тесты (TDD):** decision-table тесты `(model_trust, risk, entity_types) → decision`.
- **Закрывает критерии:** 8.
- **Самооценка:** 10/10.
- **Архитектор:** ревью 1 — **9.6/10 ПОДТВЕРЖДЕНО**; 4 MINOR (валидация enum, маппинг DTO, контрактный тест от схемы, временные метки) устранены — итерация доведена до **10/10**. QA-агент (5/10) помог найти дефекты: недостижимый `allow_summary_only`, маскировка ошибок БД под 409.

### ~~Итерация 7 — Go: LLM Router~~ ✅ Принято 10/10

- **Цель:** `internal/llm`, mock-провайдер + OpenAI-совместимый адаптер, режимы доверия, `/api/models`.
- **Тесты:** unit-тесты роутинга, mock- и OpenAI-провайдеров, конкурентности `Router`; интеграционные тесты `/api/models` (создание, валидация, дубликат, отсутствие утечки ключа).
- **Закрывает критерии:** 9.
- **Самооценка:** 10/10.
- **Архитектор:** ревью 1 — **9.3/10 НА ДОРАБОТКУ** (9 MINOR); ревью 2 — **9.7/10 ПОДТВЕРЖДЕНО** (M1–M9 устранены); 2 оставшихся MINOR (валидация `http://` без host, отклонение хвостовых данных в `decodeJSON`) устранены — итерация доведена до **10/10**. QA-агент (6/10) помог найти дефекты на этапе реализации (+24 теста).

### ~~Итерация 8 — Go: чат-оркестрация~~ ✅ Принято 10/10

- **Цель:** `/api/chat` — sanitize → policy → route → SSE-стрим → проверка ответа → audit; `/api/chat/sessions`.
- **Архитектурный план:** `docs/design/iteration-8-chat.md` (v3) — принят архитектором 9.6/10 после 3 ревью (6 → 8.5 → 9.6).
- **Фазы (TDD):** Ф1 sanitizer-клиент → Ф2 storage (chat/audit/users + миграция dev-users) → Ф3 оркестратор (Prepare/Stream/Handle) → Ф4 HTTP/SSE + контракт `chat.schema.json`.
- **Закрывает критерии:** 5.
- **Самооценка:** 10/10.
- **Архитектор:** ревью реализации 1 — **9.2/10 НА ДОРАБОТКУ** (3 MAJOR + 7 MINOR); ревью 2 — **9.6/10 ПОДТВЕРЖДЕНО** (все MAJOR/MINOR закрыты, остались 3 косметических); 3 косметические правки внесены → **10/10**. ADR идентичности — `docs/design/identity.md`; THREAT_MODEL §6 расширен остаточными рисками. **Ретро-правка после ревью этапа A (M2):** `SseError`/`SseMeta` получили `request_id` — коррелятор во всех терминальных событиях; контрактные и SSE-тесты обновлены.

### ~~Этап A — UX/UI дизайн перед frontend-итерациями~~ ✅ Принято 9.7/10

- **Цель:** дизайн-система и hi-fi UX-спецификации перед Итерациями 12–15 (frontend). Stitch MCP в сессии оказался недоступен → fallback: textual hi-fi spec.
- **Артефакты:**
  - `docs/design/ui-trends-2026.md` — тренды (bento, dark-first, AI-native, density, WCAG 2.2 AA, SSE-стрим UX, SOC-дашборды, DLP-editor, audit-log).
  - `docs/design/ui-scope.md` — матрица «6 ролей × 6 экранов × состояния», доступ.
  - `docs/design/ui-system.md` — двухслойные токены (primitive HSL + semantic), типографика, сетка, компоненты, motion, accessibility.
  - `docs/design/ui/{login,chat,documents,policies,models,audit-log,incidents}.md` — hi-fi spec каждого экрана.
- **Архитектор:** ревью 1 — **8.7/10 НА ДОРАБОТКУ** (5 MAJOR); ревью 2 — **9.7/10 ПРИНЯТО**. MAJOR'ы закрыты:
  - **M1** auth-flow: localStorage+Bearer (точка замены — OIDC); ADR в `identity.md`.
  - **M2** `SseError`/`SseMeta` получили `request_id` (ретро-правка Итерации 8); тесты зелёные.
  - **M3** `GET /api/chat/sessions/:id/messages` + `$defs.ChatMessage`/`ChatMessageList` в контракте.
  - **M4** `docs/API.md` переписан под фактические контракты (schema-as-source-of-truth).
  - **M5** `ui/chat.md` уточнён: длина hash в tooltip, источник entities при reload, 4 состояния диалога «Превью».
- **Открытый техдолг этапа A (MINOR, не блокирует Итерацию 9):** заметки архитектора в задаче A.6 (m1–m12 первого ревью, см. истории ревью).

### ~~Итерация 9.5 — Per-provider зашифрованный API-key~~ ✅ Принято (закрытие техдолга Итерации 7)

- **Цель:** убрать единый `LLM_API_KEY` env-key, каждый
  openai_compatible-провайдер хранит свой шифрованный ключ.
- **Файлы:** миграция `000009_model_provider_api_key.up/down.sql`;
  расширение `storage/models.go` (`APIKeyEncrypted`,
  `UpdateModelProviderAPIKey`, `GetModelProvider`, `HasAPIKey()`,
  `LogValue()`); расширение `api/models.go` (`createModelHandler`
  с `cipher` + RBAC, новый `updateModelAPIKeyHandler`);
  main.go `buildRouter` использует per-provider key с fallback на env.
- **Архитектор:** ревью 1 — 8.5/10 НА ДОРАБОТКУ (MAJOR-1 RBAC, MAJOR-2
  silent-fallback, MINOR-1 AAD по name); доводка:
  - **RBAC**: POST `/api/models` и POST `/api/models/:id/api-key`
    требуют admin/developer (auth.RoleAdmin/RoleDeveloper) — user
    → 403.
  - **fail-closed fallback**: `resolveProviderKey` при ошибке
    Decrypt НЕ маскирует мисконфиг env-ключом, а **возвращает (",
    false)** — провайдер не регистрируется в router (пропускается).
    Это правильный fail-closed: лучше «провайдер недоступен» чем
    «провайдер работает с непредсказуемым ключом».
  - **AAD = id (UUID)** вместо name — иммутабельный, не ломается
    при rename. CREATE использует 2-фазный flow: INSERT (без ключа)
    → RETURNING id → Encrypt(plaintext, AAD=id) → UPDATE.
- **Тесты:** TestCreateModelForbiddenForUser, TestUpdateAPIKey-
  ForbiddenForUser, TestCreateModelWithAPIKey/WithoutAPIKey,
  TestUpdateModelAPIKey/EmptyClears, TestModelsResponseDoesNotLeakApiKey.
- **Архитектор повторное ревью:** 9.8/10 — все 3 MAJOR/MINOR закрыты
  проверяемо; rebust 2-фазный CREATE; orphan-state наблюдаем и
  восстановим; новых дыр нет. Указана документационная регрессия:
  5 устаревших комментариев `AAD=name` (миграция 000009 + 4 в коде).
- **Финальная доводка** до **10/10** (косметический коммит):
  4 комментария в коде обновлены на `AAD=id`; миграция **000010**
  обновляет `COMMENT ON COLUMN model_providers.api_key_encrypted` в БД
  (DBA в `\d+` теперь видит актуальное описание).
- **Итог: ✅ Принято 10/10.**

### ~~Итерация 9 — Go: Audit / Incidents / шифрованные mappings / история~~ ✅ Принято 9.75/10

- **Цель:** append-only Audit API, Incidents API с авто-инцидентом при `deny`/`escalate`/`response_leak_detected`, шифрованная персистентность `pseudonym_mappings` (AES-256-GCM), история сессии `GET /api/chat/sessions/:id/messages`. Подробно — `docs/design/iteration-9.md` (v2.1).
- **Архитектурный план:** v1 — 8.7/10 на доработку (3 MAJOR + 8 MINOR); v2 — 9.65/10 принят к реализации; v2.1 — все 7 новых MINOR закрыты в плане.
- **Фазы (TDD), все ✅ закрыты:**
  - **Ф1 AES-GCM crypto** — `ce6ec58` red → `f4a225c` green; 17 sub-тестов; AAD-поддержка.
  - **Ф2a миграция 000008** — `fd1561c`; reporter_id/assignee_id/closed_at, partial unique idx_incidents_one_auto_per_event, incident_notes append-only, индексы audit, chat_messages.request_id; verify_schema PASSED.
  - **Ф2b storage.mapping** — `ef39b1b`; InsertPseudonymMappings (batch unnest); 7 тестов; LogValuer-redaction.
  - **Ф2c storage.incidents** — `5dbe161`; CreateAuto/Manual atomic Tx3, PatchIncident с If-Match→412, AddIncidentNote append-only, FindManualIncidentForReporter; 13 тестов.
  - **Ф2d storage.audit** — `e9d00c7`; ListAuditEvents/GetAuditEvent с keyset cursor row-comparison; 5 тестов; jsonb GIN-фильтр has_leak.
  - **Ф2e storage.chat** — `2ad7633`; request_id+Mappings в Tx1/Tx2; ListChatMessages с JOIN+whitelist (start/end не утекают); 5 тестов.
  - **Ф3 оркестратор** — `d531752`; PseudonymMap.LogValue() (никакого raw в логах), MappingAAD=SHA-256(session_id‖pseudonym), auto_incident.go (severityFor leak +2 ступени), Cipher в Orchestrator, расширение Store interface, config MAPPING_ENCRYPTION_KEY fail-closed, main проброс Cipher.
  - **Ф4 HTTP** — `e5b9fd5`; 9 эндпойнтов (`/api/audit-events*`, `/api/incidents*`, `/api/chat/sessions/:id/messages`), 2 контракта (`audit.schema.json`, `incidents.schema.json`); 12 API-тестов (включая критический тест на отсутствие start/end в JSON истории; 412/428 PATCH; developer scope 404).
- **Самооценка реализации:** 9.7/10 — все архитектурные решения плана реализованы; критические инварианты безопасности доказаны тестами; 10 пакетов green.
- **Архитектор:** ревью 1 — 9.4/10 НА ДОРАБОТКУ (3 MAJOR + бюджет + MINOR-10); доработка `510402c` → ревью 2 — 9.5/10 НА ДОРАБОТКУ (MAJOR-A graceful shutdown, MAJOR-B тест-дыра export filters); доводка `f13907b` → ревью 3 — **9.75/10 ПРИНЯТО К ЗАКРЫТИЮ**.
- **Закрывает критерии:** 10, 11.
- **Техдолг (3 косметических MINOR):** двойная пустая строка в audit.go:17-18; `TestChatEndpointFullFlow` без `orch.Wait()` в Cleanup; `TestExportAuditEventsCSV` не проверяет marker в CSV-строке. Не блокируют MVP.

### ~~Итерация 10 — Worker: документы~~ ✅ Все 7 фаз реализованы

- **Цель:** парсинг (PDF/DOCX), chunking, DB-очередь, MinIO, embeddings-интерфейс (mock), `/api/documents`. **Закрывает критерий MVP 6.**
- **Архитектурный план:** v2 9.6/10 принят (после ревью v1 9.2/10 + доводка 3 MAJOR + 5 MINOR).
- **Фазы реализованы (TDD):**
  - Ф1 `e65c411` миграция 000011 + worker skeleton (healthy на :8002)
  - Ф2 `d77ab94` `app/queue.py` (FOR UPDATE SKIP LOCKED + heartbeat + idempotency) — 8 тестов
  - Ф3 `10b528a` парсеры PDF/DOCX — 8 тестов
  - Ф4 `b477a7f` chunking (tiktoken cl100k_base) — 6 тестов
  - Ф5 `428a401` sanitizer-client + Embedder/MockEmbedder — 5 тестов
  - Ф6a+Ф6b `46c18b6` Go-storage + API (6 эндпойнтов) + MinIO Go-клиент — все 10 пакетов green
  - Ф7 `fe8f58b` контракты documents + audit event_types
  - Processor pipeline + queue-loop в `_queue_loop` — worker полностью обрабатывает очередь
- **Тесты:** worker ~27 unit/integration green; Go-стороне 10 пакетов green; docker compose worker healthy.
- **Архитектор:** ревью 1 — 9.7/10 (m1 неполный read upload, m2 orphan-MinIO при сбое CreateDocument); фикс — `+5` строк io.ReadAll + LimitReader + Remove в ветке ошибки. **Итог: 10/10 ✅.**

### ◐ Итерация 11 — Базовый RAG (Ф0+Ф1 закрыты, Ф2–Ф5 в работе)

- **Цель:** поиск по `pgvector` с учётом ACL.
- **Ф4a ✅ (2026-05-26):** `internal/chat/rag.go` — `Retriever` interface,
  `ChatRetriever`, `BuildRAGSystemPrompt` (delimitered `<rag_source>` блоки +
  директива анти-injection), `escapeRAGContent` (control-tokens escape),
  `DetectSuspiciousPattern` (en/ru regex), `FilterHighRiskForExternal`
  (high/critical drop для external-LLM), `stripSourceEchoes` (post-LLM
  regex strip эхо-тегов), `TruncateByBudget`. **24/24 unit-тестов** зелёные.
  Самооценка 9.5/10 (ревью архитектора Ф4a отложено — Ф4b/Ф4c интеграция
  в orchestrator тестируется в составе общего Ф4).
- **Ф3 ✅ Принято 9.5/10 (2026-05-26):** `internal/api/ratelimit.go`
  (UserRateLimiter token-bucket per user, 30 RPM/burst 5, audit
  `rate_limit_exceeded` one-per-window); расширенный `searchHandler`
  (валидация query 1..2000 рун, audit `search_performed` с
  `top_document_ids/chunk_ids/scores_summary/rag_mode/latency_ms/embedder_model`,
  audit `acl_violation_attempt` через `storage.FilterAccessibleDocuments` —
  диагностика BLOCKER B1 без false-positive при limit clamp);
  `docs/contracts/rag.schema.json` (новый); расширение `audit.schema.json`
  на 7 новых event_types. **7 unit + 11 handler** тестов зелёные.
  Ревью архитектора: 8.5 → 9.5/10 ПРИНЯТО после доработки P0 (false-positive)
  + P1 (matrix-тест ACL). Техдолг: P2 (golden contract против
  rag.schema.json), P3 (fuzz audit-grep с escape).
- **Ф2 ✅ Принято 10/10 (2026-05-26):** обновлённый `SearchChunks` —
  обязательный `embedderName` (fail-closed `ErrEmbedderNameRequired`,
  §Р9), документIDs filter поверх ACL (BLOCKER B1), snippet truncation
  512 рун UTF-8 safe, JOIN sanitization_results для risk_level. Миграция
  000016 — композитный индекс `idx_sanitization_results_doc_created`.
  Updated `search.go` handler. **5 unit (truncateRunes) + 14 integration**
  тестов с живой БД. Per-test уникальная ось в 1024-векторе через FNV-1a
  hash от prefix — защита от поллюции dev-БД. Ревью архитектора: 9/10 →
  **10/10** после доводки (миграция, fuzz-тест от B1, комментарий-якорь
  про FNV, fail-closed embedderName).
- **Ф1 ✅ Принято 10/10 (2026-05-26):** интерфейс `llm.Embedder`
  (`Embed`/`Name`/`Dim`); `OpenAICompatibleEmbedder` (LM Studio /
  vLLM / Ollama, fail-closed на dim≠1024/5xx/empty/context cancel);
  фабрики `cmd/rubezh-api/main.go::buildEmbedder` + Python
  `app.embeddings.build_embedder`; env `EMBEDDER_KIND/URL/MODEL/API_KEY/
  TIMEOUT_SECONDS`; cross-language symmetry test (golden 16 компонент
  идентичны байт-в-байт между Go и Python); `Deps.Embedder` —
  **обязательное** поле (fail-closed panic в `NewRouter` при nil,
  никаких тихих деградаций). 30 новых тестов (Go+Python), все зелёные.
  **Bug Ф0 пойман и пофикшен:** делитель MockEmbedder был `2^32-1`,
  Python — `2^32` → векторы жили в разных пространствах; cross-language
  ranking был бы бесполезен. Симметрия восстановлена. CHANGELOG.md
  создан с разделом Breaking + SQL для миграции dev-БД. Ревью
  архитектора: 9/10 → 10/10 после доводки (убран nil-fallback,
  добавлен CHANGELOG).
- **Ф0 уже в коде (зафиксировано аудитом 2026-05-26):**
  `internal/storage/search.go` (`SearchChunks` c ACL-фильтром в SQL для
  не-supervisor); `internal/api/search.go` (`POST /api/search`, sanitize
  query, embed, audit `search_performed`); `internal/llm/embedder.go`
  (`MockEmbedder` SHA-256 dim=1024, бинарно-совместим с
  `rubezh-worker/app/embeddings/mock.py`); миграция `000004` уже
  включает `embeddings vector(1024)` + HNSW-индекс
  `idx_embeddings_vector USING hnsw (embedding vector_cosine_ops)`.
- **План v2.2 — `docs/design/iteration-11-rag.md`** (заменяет v1
  `iteration-11.md`, помечен superseded). Доводит до полного DoD:
  реальный embedder через DI (Ф1), ACL/snippet/embedder-name guard
  regression-тесты на storage (Ф2), `/api/search` обвязка — rate-limit
  + расширенный audit + контракт `rag.schema.json` (Ф3), интеграция в
  chat orchestrator за явным флагом `rag.enabled` + policy-revision
  после retrieval + post-LLM strip RAG-source-эха (Ф4), frontend toggle
  + источники в MessageBubble (Ф5).
- **История ревью плана:**
  - v2 — самооценка 9.5/10; независимое ревью архитектора **8.5/10**:
    BLOCKER B1 (ACL bypass через `document_ids`), MAJOR M1
    (prompt-injection через содержимое чанков), MAJOR M2 (DoS через
    policy revision), MAJOR M3 (rate-limit bypass через restart),
    MAJOR M4 (audit-схема), 7 MINOR.
  - v2.1 — закрытие B1 + M1–M4 + m4-m7 (14 новых DoD/тестов).
    Самооценка 9.7/10; ревью **9.6/10 ПРИНЯТО**, 3 MINOR (m8 post-LLM
    strip, m9 XML rationale, m10 sync.Map GC) → v2.2.
  - v2.2 — закрытие m8 (`stripSourceEchoes` + D14), m9, m10.
    Самооценка **10/10**, **архитектор разрешил старт Ф1**.
- **Закрывает критерий:** «поиск по pgvector с учётом ACL» из карты MVP.

### ~~Итерация 12 — Frontend: каркас~~ ✅ Закрыта де-факто (внутри G.1)

- **Цель:** Vite, роутинг (React Router v7), API-клиенты + Zod-типы, TanStack Query, auth-контекст, ESLint/Prettier.
- **Реализовано:** `rubezh-web/` (Vite 6 + React 19 + RR v7 + TanStack Query + Tailwind v4); `api/{client,schemas,sse}.ts`; auth-контекст; 9 страниц (см. Итерации 13–15). Контракт Go↔Zod проверяется автоматом — G.1 (`contract.test.ts` + golden-фикстуры).
- **Тесты (Vitest + RTL):** `schemas.test.ts`, `sse.test.ts`, `client.test.ts`, `LoginPage.test.tsx`, `contract.test.ts` (G.1).
- **Закрывает критерий:** 2.
- **Замечание:** формального отдельного ревью архитектора по самой итерации 12 не было — каркас закрылся попутно в G.1 (`contract.test.ts` + CI-джоба `web`). Дальнейшие правки фронта проходили ревью в составе J/K/L.

### ~~Итерация 13 — Frontend: экран Chat~~ ✅ Закрыта де-факто (внутри J)

- **Цель:** ввод, загрузка файла, выбор модели, индикатор политики, предпросмотр обезличивания, SSE-стрим.
- **Реализовано:** `pages/ChatPage.tsx` + RTL-тесты (`ChatPage.test.tsx`): SSE-стрим, picker cloud/local (J.4), CloudGate-диалог предпросмотра по `preview_token` (J.1/J.2), кнопка «Показать реальные данные» (J.2 reveal), attach документа в чат (J.3).
- **Архитектор:** ревью прошло как часть фазы J (`docs/design/chat-pii-flow.md` v2, все MAJOR закрыты).
- **Закрывает критерий:** 5 (в части UX).

### ~~Итерация 14 — Frontend: Documents и Policies~~ ✅ Закрыта де-факто

- **Цель:** экраны списка документов и управления политиками с тестом политики.
- **Реализовано:** `pages/DocumentsPage.tsx` (статусы, скачать оригинал / обезличенную версию — J.3), `pages/PoliciesPage.tsx`.
- **Тесты:** компонент-тесты в составе G.2/J; контракты Documents/Policies проверены G.1.

### ~~Итерация 15 — Frontend: Models, Audit Log, Incidents~~ ✅ Закрыта де-факто (внутри G.2)

- **Цель:** экраны провайдеров, журнала аудита, инцидентов с карточкой расследования.
- **Реализовано:** `pages/{ModelsPage,AuditLogPage,IncidentsPage}.tsx` + RTL-тесты `ModelsPage.test.tsx` (toggle/delete), `IncidentsPage.test.tsx` (If-Match, ResolutionDialog, 412).
- **Архитектор:** ревью прошло в составе G.2.

### ~~Итерация 16 — Интеграция и финализация~~ ✅ Закрыта

- **Цель:** полный `docker compose up`, e2e smoke-тест, базовая проверка
  prompt injection, тесты на утечку логов, финальный README.
- **Реализовано:** Все 6 сервисов поднимаются `docker compose up` без
  моков на критическом пути; e2e подтверждён вживую через rubezh-web ↔
  rubezh-api ↔ rubezh-sanitizer ↔ rubezh-worker ↔ Postgres ↔ MinIO
  (включая внешние модели через SSH-CLI bridge). Prompt-injection защита:
  fixed-template detector в sanitizer + RBAC system_prompt; нет утечек
  в логах (golden-тест на slog.LogValuer для PseudonymMap и ModelProvider).
- **Закрывает критерии:** 1, 12, 14, 15.

### ~~Волны post-MVP W1/W2/W3~~ ✅ Закрыты

После внешнего аудита (8 finding'ов P1–P3) — три волны точечных фиксов:

**W1 — Security P1 (sanitize + RBAC для system_prompt, document body в allow_raw):**
- `system_prompt` и `review.system_prompts` теперь только admin/developer
  (403 иначе); проходят тот же sanitize-pipeline; audit `chat_request.detail`
  содержит `system_prompt_sha256` + `system_prompt_masked`, raw НЕ хранится.
- Документ-flow `allow_raw` восстанавливает тело из `pmap.Restore(SanitizedText)`
  при наличии `preview_token` — раньше LLM получала только плейсхолдер.
- Тесты: 4 новых GREEN; live smoke 4/4 (psql подтвердил отсутствие raw).

**W2 — UX/Stability P2 (SSE truncation, review files, dev-DB cleanup, /live vs /ready):**
- SSE-клиент синтезирует error-event при EOF без `done`/`error`.
- Review-loop видит файлы-артефакты от primary: text-форматы — preview
  ≤2KB после pmap.Remask (защита от PII), бинарные — только metadata.
- Worker: `/live` (no-DB) для liveness, `/ready` (DB + 2s timeout) для
  readiness; `/health` — alias `/live`.
- testdb cleanup расширен (withkey-, updkey-, ...); одноразовая чистка
  387 dev-провайдеров; rubezh-api startup лог чистый.
- preview_token_miss audit (новая стадия chat_error).
- Sanitize review-prompt graceful fallback (degrade open, fail closed для PII).
- Ревью архитектора: 8/10 → закрыты 2 MAJOR (PII через TextPreview,
  /ready timeout).

**W3 — Contract sync + docs + W2 backlog:**
- `sanitize.schema.json` расширен 4 контекстами (chat/document/
  system_prompt/review_system_prompt); Pydantic + Go orchestrator
  используют отдельные метки для лучшей telemetry.
- preview_token_miss throttle (5/мин per user, anti-spam).
- MN-1..MN-5 из ревью W2: AbortError detection через signal.aborted,
  base64 newlines в regex, size limit на attachment block, bool
  вместо string в audit fallback.
- Документация (README, API.md, ARCHITECTURE.md, PLAN.md, CLAUDE.md)
  синхронизирована с фактической реализацией.

### ~~Итерация H.3 — LLM-обезличивание (фильтр 2/3) + усиление фильтра 1~~ ✅ Реализовано

- **Цель:** подключить локальную русскоязычную LLM (LM Studio / DeepSeek-7B)
  как фильтр 2/3 через интерфейс `Detector`; закрыть пропуски на
  `testdata/fake_contract.docx` (паспорт `4501 № 234567`, банковская карта,
  ИНН физлица, СНИЛС, пароль во фразе).
- **Фильтр 1 (rules-first):** `bank_card_luhn` (16 цифр + Луна) и
  `bank_card_grouped` (формат 4-4-4-4); паспорт со знаком `№`; `inn_labeled` /
  `snils_labeled` — детекция по контекстной метке даже при невалидной
  контрольной сумме; `password` допускает уточняющие слова перед разделителем.
  `EntityType.BANK_CARD` + префикс `КАРТА` + синхронизация контракта.
- **Фильтр 2/3 (LLM-assisted):** `app/llm_review/` — `LLMReviewClient`
  (Protocol + `MockLLMReviewClient`-fallback), `OpenAILLMReviewClient`
  (OpenAI-совместимый, `response_format=json_schema`, fail-open, robust-парсинг
  reasoning-моделей), `LLMReviewDetector` (адаптер к `Detector`). Env
  `SANITIZER_LLM_URL/MODEL/KEY/TIMEOUT` (опциональны). Проводка через lifespan +
  DI в `/sanitize/preview`. LLM **не принимает** решений allow/deny.
- **Проверено вживую:** `docker compose` + реальная DeepSeek-7B
  (LM Studio `172.27.48.1:1234`, из контейнера `host.docker.internal`).
  Договор обезличивается **полностью детерминированно** (LLM — бэкап).
- **Тесты:** +33 (детекторы карт/паспорта/контекстных ИНН-СНИЛС, модуль
  LLM-review, парсер ответов, fail-open). Всего 178 в санитайзере, ruff/mypy чисты.

### ~~Итерация G.1 — Контрактные тесты Go ↔ TypeScript~~ ✅ Реализовано

- **Цель:** автоматически ловить рассинхрон Go-DTO ↔ Zod-схем (4 бага E.2).
- Go golden-тест `contract_export_test.go` рефлексией 9 DTO генерирует
  нормализованные формы в `rubezh-web/src/test/contracts/*.json`; TS
  `contract.test.ts` сверяет их с формой Zod-схем (`_def`): поля, типы,
  nullability. CI-джобы `web` + `contract-sync`. README + design-doc.
- **Тесты:** Vitest 38/38 (было 21; +9 контрактных).

### ~~Итерация G.2 — UI-управление провайдерами + RTL~~ ✅ Реализовано

- **G.2a/b:** `PATCH /api/models/:id` (toggle is_enabled), `DELETE
  /api/models/:id` (с FK-защитой → 409 + soft-disable), RBAC, hot-reload
  Router. Storage + handler-тесты; живой smoke 200/204/404/403.
- **G.2c:** UI toggle/delete в ModelsPage + RTL-тесты ModelsPage (3),
  IncidentsPage (3: If-Match, ResolutionDialog, 412), ChatPage (2: picker).

### ~~Итерация J — Чат с контролируемым выводом ПДн за контур~~ ✅ Закрыта

План — `docs/design/chat-pii-flow.md` (ревью архитектора 7.5→v2, все MAJOR
закрыты). Реализовано и протестировано:

- **J.0/J.1** ✅ `POST /api/chat/preview` + `preview_token` (RAM-кэш,
  owner-bound, одноразовый) — единый sanitize переиспользуется в `/api/chat`
  (детерминизм preview↔chat; MAJOR-1).
- **J.2** ✅ ре-маскирование ответа `allow_masked` (Remask, не auto-restore);
  `POST /api/chat/messages/{id}/reveal` (детерминированно, owner-only,
  AAD-расшифровка, `no-store`, raw не логируется, audit `response_revealed`,
  fail-closed); SSE `done` несёт `assistant_message_id`.
- **J.4 (frontend)** ✅ кнопка «Показать реальные данные»; гейт CloudGate
  перед облаком (по `preview_token`) + индикатор обработки; picker с
  разделением «Облачные ⚠ / Локальные 🛡». RTL: гейт→подтверждение→стрим→reveal.
- **J.3 (часть)** ✅ обезличенная выгрузка `GET /api/documents/{id}/masked`
  (.txt из sanitized-чанков) + кнопки в DocumentsPage.
- **Дизайн:** визуальный макет (PNG, рендер прототипа) + HTML-прототип +
  UX-спека. Stitch-макеты — после перезапуска CC.

---

## Технический долг

Все 8 MINOR из ревью Итераций 4–5 **устранены** (коммиты `9161fbd`, `1044630`):

- ~~NER-фильтр замещал regex~~ → `pipeline.sanitize(ner=...)` дополняет фильтр 1.
- ~~`resolve_overlaps` O(n²)~~ → `bisect`, O(n log n).
- ~~enum `Entity.detector` без контрактного теста~~ → контрактный тест добавлен.
- ~~`cipher` инициализировался на import-time~~ → FastAPI lifespan (`app.state`).
- ~~healthcheck и сервер читали порт раздельно~~ → единый `config.HTTPPort()`.
- ~~`healthcheck()` без теста~~ → `main_test.go` (`healthcheckAt`, `logLevel`).
- ~~`requestLogger` без статус-кода~~ → `status` + `request_id` (chi RequestID).
- ~~`cfg.LogLevel` не применялся~~ → прокинут в `slog.HandlerOptions`.

**~~Открыто (Итерация 7)~~ ЗАКРЫТО Итерацией 9.5 (коммит см. ниже):**

- ~~**Единый ключ для всех `openai_compatible`-провайдеров.**~~ → реализована
  Итерация 9.5: миграция `000009` добавила `model_providers.api_key_encrypted`
  (AES-256-GCM, AAD=name; шифруется тем же `MAPPING_ENCRYPTION_KEY` —
  один app-key, разделение ключей mapping/api_key — пост-MVP). API:
  `POST /api/models` принимает опц. `api_key`, `POST /api/models/:id/api-key`
  обновляет/очищает. DTO содержит `has_api_key: bool`, plaintext не
  возвращается никогда. main.go `buildRouter` использует per-provider key
  с fallback на `LLM_API_KEY` env (backward compat для существующих
  deployments).

**3 косметических MINOR из 3-го ревью Итерации 9 (закрыты `30c462b`):**

- ~~Двойная пустая строка в `audit.go:17-18`~~ → убрана.
- ~~`TestChatEndpointFullFlow` без `orch.Wait()` в `t.Cleanup`~~ →
  `t.Cleanup(orch.Wait)` добавлен.
- ~~`TestExportAuditEventsCSV` не проверяет marker в CSV~~ →
  `seedAuditWithMarker` пишет marker в `masked_payload`; тест
  проверяет наличие marker и id seed-события в CSV-body.

**12 MINOR из 1-го ревью этапа A (m1-m12, закрыты):**

- ~~m1 SSE keep-alive `: ping`~~ → `ui-trends-2026.md §6`, `ui-system.md §7`.
- ~~m2 login states (rate-limit 429, no-roles-seeded)~~ → уже в `ui/login.md`.
- ~~m3 axe-core / Lighthouse в §2.7~~ → `ui-system.md §2.7`.
- ~~m4 documents polling back-off~~ → `ui/documents.md` (exp back-off).
- ~~m5 audit-log event_type enum из контракта~~ → `ui/audit-log.md`.
- ~~m6 incidents polling без push~~ → `THREAT_MODEL §7 #6`.
- ~~m7 policies sanitize note~~ → `ui/policies.md` (raw_classes derived).
- ~~m8 chat preview-диалог states~~ → уже в `ui/chat.md`.
- ~~m9 skeleton reduced-motion~~ → `ui-system.md §7`.
- ~~m10 audit-log compliance_officer/auditor filter rules~~ → `ui/audit-log.md`.
- ~~m11 год в audit-log tooltip~~ → `ui/audit-log.md`.
- ~~m12 aria-live single-source-of-truth~~ → `ui-system.md §9`.
- ~~m13 ui/models.md отстаёт от Итерации 9.5 (403/2-фазный/key-broken)~~ →
  `ui/models.md`: добавлены §«Индикатор состояния ключа» (chip danger
  для broken-key), §«2-фазное создание провайдера с ключом» (warning-
  toast + «Перешифровать api-key» в меню), 3 новых State (403,
  key-encrypt-failed, key-broken). После Итерации 9.5 backend RBAC
  и fail-closed теперь имеют UX-отражение.

**Открыто (на 2026-05-26):**

- **Stitch-макеты к Итерации J.** Перенесены из секции J — после
  перезапуска CC; не блокирует MVP (HTML-прототип + PNG-рендер есть).

- **RAG UX: deep-link с chip → конкретный chunk документа** (пост-MVP).
  SSE `rag_hits` сейчас несёт только метаданные; чтобы пользователь
  мог раскрыть полное содержимое источника, фронт должен делать
  `GET /api/documents/:id/chunks` с навигацией по `chunk_index`.
  В Итерации 11 Ф5 chip-list реализован без deep-link'а (только
  filename + relevance в tooltip). Зафиксировано как scope-cut
  ревью архитектора Итерации 11 (Q7).

- **RAG: интеграционный автотест `TestEndToEnd_PythonEmbedGoSearch`**
  (пост-MVP). Сейчас регресс ABI Go↔Python embedder'ов защищён:
  (1) golden 16 компонент в `internal/llm/mock_symmetry_test.go` ≡
  `rubezh-worker/tests/test_embeddings.py::GOLDEN_HELLO_FIRST16`,
  (2) одноразовый e2e через docker-compose в Итерации 16. Полный
  автотест с testcontainers-go отложен как over-engineering для
  on-prem MVP. Scope-cut ревью архитектора Итерации 11 (Q8).

- **SSH-CLI / Review Mode: отложенный техдолг после live-проверок.**
  Сейчас не блокирует MVP, но должно быть поднято при следующей
  стабилизационной итерации:
  - длинные SSE-стримы иногда могут давать `network error`; нужна
    отдельная диагностика keep-alive/reconnect/backpressure/timeout
    для длинных ответов и многошаговой ревизии;
  - e2e mock-SSH тест для `deploy/ssh-bridge/ai-bridge.py`: fake remote
    command, forced-command контракт stdin/stdout JSON, `files[]`,
    exit-code/stderr cases;
  - переход на MinIO-backed attachments, если inline `data:`/base64
    упрётся в текущий лимит около 5 MB: bridge должен складывать файл
    в MinIO и отдавать UI signed/download endpoint вместо тяжёлого
    Markdown data-link.

**~~Закрыто 2026-05-26:~~**

- ~~**Поллюция dev-БД интеграционными тестами.**~~ → реализован пакет
  `internal/testdb` с `Cleanup(dsn, prefixes)` + `TestNameUnique(t, kind)`.
  Каждый `go test`-процесс получает собственный префикс `itest_<pid>_`;
  глобальный `TestMain` в пакетах `storage` и `api` после `m.Run()`
  чистит свой pid + legacy-префиксы. Защита от prod-БД: host-allowlist
  (`postgres`, `localhost`, `127.0.0.1`, `::1`, `db`) + env-override
  `TESTDB_ALLOW_HOST`. `model_providers` soft-disable (FK от
  append-only `audit_events`); `policies` DELETE; `user_provider_credentials`
  DELETE до model_providers. Ревью архитектора 1 — 7/10 (3 MAJOR);
  ревью 2 — **9.5/10 ПРИНЯТО**; доводка до **10/10** (env-override
  TESTDB_ALLOW_HOST + KNOWN LIMITATION про k8s short-hostnames в
  комментариях). KNOWN LIMITATION: имена `postgres`/`db` могут
  совпадать с k8s-Service short-name — при появлении k8s-staging
  сузить allowlist через `TESTDB_ALLOW_HOST` override.

Новые пункты добавляются сюда по мере появления из ревью.

## История ревью архитектора

| Итерация | Дата | Оценка | Вердикт |
|---|---|---|---|
| 0 — ревью 1 | 2026-05-19 | 8/10 | На доработку — 3 MAJOR по контрактам |
| 0 — ревью 2 | 2026-05-19 | 9.5/10 | ПОДТВЕРЖДЕНО — все MAJOR/MINOR устранены |
| 1 — ревью 1 | 2026-05-19 | 7.5/10 | На доработку — 3 MAJOR (forensics-каскад, аудит без версии политики, TDD) |
| 1 — ревью 2 | 2026-05-19 | 9/10 | ПОДТВЕРЖДЕНО |
| 2 — ревью 1 | 2026-05-19 | 7.5/10 | На доработку — 3 MAJOR (детекторы) |
| 2 — ревью 2 | 2026-05-19 | 8.5/10 | На доработку — MINOR-N1 (КПП региона 04) |
| 2 — ревью 3 | 2026-05-19 | 9/10 | ПОДТВЕРЖДЕНО |
| 3 — ревью 1 | 2026-05-19 | 9.6/10 | ПОДТВЕРЖДЕНО — 5 MINOR перенесены в Итерацию 4 |
| 4 — ревью 1 | 2026-05-19 | 9.6/10 | ПОДТВЕРЖДЕНО — 4 MINOR в бэклог |
| 5 — ревью 1 | 2026-05-19 | 9.6/10 | ПОДТВЕРЖДЕНО — 4 MINOR в бэклог |
| Техдолг 4–5 | 2026-05-19 | 9.7 → 10/10 | ПОДТВЕРЖДЕНО — 8 MINOR устранены |
| 6 — ревью 1 | 2026-05-19 | 9.6/10 | ПОДТВЕРЖДЕНО; 4 MINOR устранены → 10/10 |
| 7 — ревью 1 | 2026-05-19 | 9.3/10 | На доработку — 9 MINOR (M1–M9) |
| 7 — ревью 2 | 2026-05-19 | 9.7/10 | ПОДТВЕРЖДЕНО; 2 MINOR устранены → 10/10 |
| 8 — план, ревью 1 | 2026-05-19 | 6/10 | На доработку — 2 BLOCKER + 4 MAJOR |
| 8 — план, ревью 2 | 2026-05-19 | 8.5/10 | На доработку — 2 MAJOR-NEW + 3 MINOR |
| 8 — план, ревью 3 | 2026-05-19 | 9.6/10 | ГОТОВ К РЕАЛИЗАЦИИ — план принят |
| 8 — реализация, ревью 1 | 2026-05-20 | 9.2/10 | На доработку — 3 MAJOR + 7 MINOR |
| 8 — реализация, ревью 2 | 2026-05-20 | 9.6/10 | ПОДТВЕРЖДЕНО; 3 косметических MINOR устранены → 10/10 |
| A — дизайн, ревью 1 | 2026-05-20 | 8.7/10 | На доработку — 5 MAJOR (auth-flow, SseError.request_id, history endpoint, API.md sync, chat.md uncertainties) |
| A — дизайн, ревью 2 | 2026-05-20 | 9.7/10 | ПРИНЯТО К РЕАЛИЗАЦИИ — все 5 MAJOR закрыты, контракт+код+тесты+UX-spec симметричны |
| 9 — план, ревью 1 | 2026-05-20 | 9.1/10 | На доработку — 3 MAJOR (reporter_id миграция, event_type enum, developer access) + 8 MINOR |
| 9 — план, ревью 2 | 2026-05-20 | 9.65/10 | Принят к реализации (3 MAJOR + 8 MINOR закрыты); рекомендованы правки 7 новых MINOR до Ф1 |
| 9 — план, v2.1 | 2026-05-20 | ожид. ≥9.7 | 7 MINOR закрыты в тексте плана (AAD per-mapping, atomic Tx3, severityFor leak, atomic PATCH, 404 developer, notes-RW матрица) |
| 9 — реализация, ревью 1 | 2026-05-20 | 9.4/10 | На доработку — 3 MAJOR (export filters игнорируются, latency регрессия от auto-incident, бюджет incidents.go) + MINOR-10 |
| 9 — реализация, ревью 2 | 2026-05-20 | 9.5/10 | На доработку — MAJOR-A (auto-incident at shutdown), MAJOR-B (тест-дыра по export filters) |
| 9 — реализация, ревью 3 | 2026-05-20 | **9.75/10** | **ПРИНЯТО К ЗАКРЫТИЮ** — graceful shutdown с orch.Wait(), усиленный test export-filters; 3 косметических MINOR → техдолг |
| 9 — реализация, ревью 4 | 2026-05-20 | **10/10** | **ЗАКРЫТА** — 3 косметических MINOR закрыты (`30c462b`); regression-shields добавлены (orch.Wait в Cleanup, marker в CSV) |
| A — дизайн, ревью 3 | 2026-05-20 | **9.9/10** | Найден m13: ui/models.md отстаёт от Итерации 9.5 (RBAC/2-фазный/key-broken) |
| A — дизайн, ревью 4 | 2026-05-20 | **10/10** | **ЗАКРЫТО** — m13 устранён (`32dc96d`); все 13 MINOR'ов закрыты |
| 9.5 — реализация, ревью 1 | 2026-05-20 | 8.5/10 | На доработку — RBAC, fail-closed, AAD=id |
| 9.5 — реализация, ревью 2 | 2026-05-20 | 9.8/10 | Принято; 5 устаревших комментариев AAD=name (документ. регрессия) |
| 9.5 — реализация, ревью 3 | 2026-05-20 | **10/10** | **ЗАКРЫТА** — миграция 000010 + 4 комментария обновлены (`2bcd346`) |

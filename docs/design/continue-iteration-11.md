# Промпт для продолжения Итерации 11 в новой сессии

Скопируй текст ниже в новое окно Claude Code. Контекст ниже — самодостаточный
бриф; CLAUDE.md и PLAN.md уже содержат всю накопленную историю.

---

## Промпт

```
Продолжаю автономную работу над Итерацией 11 «Базовый RAG» в проекте
«Рубеж ИИ» (C:\dev\RubezAI). Работаю по правилам:

- Полный автоном без подтверждений (см. CLAUDE.md §«Рабочий процесс»).
- TDD: red → green отдельными коммитами.
- После каждой фазы — независимое ревью архитектора через subagent Plan;
  порог приёмки ≥ 9.5/10, цель 10/10. При <9.5 — доработка и повторное
  ревью того же шага.
- Оцениваю свою работу 1-10 после каждой фазы и сообщаю пользователю.

КОНТЕКСТ — что уже сделано (см. docs/PLAN.md §«Итерация 11»):

Дизайн v2.2 (docs/design/iteration-11-rag.md) — 10/10 принят.
Ф0 — Embedder mock + storage.SearchChunks (базовый) + /api/search
  handler уже в коде до начала работ.
Ф1 ✅ 10/10 — Embedder DI (Embedder interface, OpenAICompatibleEmbedder
  в Go и Python, фабрики, env EMBEDDER_KIND, cross-language symmetry
  с golden 16-компонент, fail-closed dim mismatch, baghi Ф0 поймана
  и пофикшена: делитель 2^32-1→2^32 — см. CHANGELOG.md breaking).
Ф2 ✅ 10/10 — SearchChunks обновлён: обязательный embedderName guard
  (план §Р9), document_ids фильтр поверх ACL (BLOCKER B1), snippet
  truncation 512 рун UTF-8 safe, JOIN sanitization_results для
  risk_level. Миграция 000016 (композитный индекс). 19 новых тестов
  (5 unit + 14 integration с живой БД).
Ф3 ✅ 9.5/10 — /api/search обвязка: UserRateLimiter (30 RPM, burst 5,
  audit one-per-window), расширенный audit search_performed (8 полей),
  audit acl_violation_attempt через storage.FilterAccessibleDocuments
  (без false-positive при limit clamp). docs/contracts/rag.schema.json
  + 7 новых event_types в audit.schema.json. 18 новых тестов.
Ф4a ✅ 9.5/10 (ревью отложено) — internal/chat/rag.go:
  Retriever interface, ChatRetriever, BuildRAGSystemPrompt (delimitered
  <rag_source> блоки + анти-injection директива), escapeRAGContent
  (control-tokens escape), DetectSuspiciousPattern (en/ru regex),
  FilterHighRiskForExternal (high/critical drop для external-LLM),
  stripSourceEchoes (post-LLM strip эхо-тегов), TruncateByBudget.
  24/24 unit-тестов зелёные. Готов к интеграции в orchestrator.

ОСТАЛОСЬ:

Ф4b — Интеграция RAG в chat.Orchestrator (БОЛЬШАЯ).
  Файлы: internal/chat/{types.go,orchestrator.go}.
  План v2.2 §3.Р4:
  - Добавить Request.RAG *RAGParams (из rag.go).
  - NewOrchestrator принимает дополнительно retriever Retriever (nil
    глобально отключает RAG).
  - Stream() врезает retrieval между sink.Meta() и runLLM():
    1) если req.RAG.Enabled и outcome.Decision permits RAG → embed
       sanitizedText, SearchChunks с эмbedderName guard;
    2) FilterHighRiskForExternal для external-LLM (audit
       rag_chunk_dropped_high_risk per chunk);
    3) DetectSuspiciousPattern по каждому оставшемуся (audit
       rag_chunk_suspicious_pattern);
    4) TruncateByBudget по 5×1024 рун;
    5) sink.RagHits(hits-метаданные без snippet) — новый event;
    6) BuildRAGSystemPrompt(hits) → инъекция в LLM messages;
    7) Policy re-evaluation: если retrieved chunks повышают risk →
       policy.Decide пересчитывается с severity cap (max +1 ступень
       в шкале allow_raw<allow_masked<allow_summary_only<escalate<deny),
       чтобы исключить DoS «низкорисковый запрос + critical в моём ACL
       → guaranteed escalate». Downgrade → audit policy_revised_after_rag
       (deduplicated через partial unique в incidents + rate-limit
       10/час per user; превышение → policy_revised_after_rag_throttled).
    8) runLLM получает messages с RAG-system-message + user query.
    9) Каждая delta из LLM → stripSourceEchoes → sink.Delta (D14 MINOR m8).
    10) audit rag_query (один на стрим, detail с query_hash, top IDs,
        rag_mode='chat_integrated', latency_ms).
  Тесты (план v2.2 §4.Ф4, 14 штук):
  TestStream_RagRetrievalCalled, _RagHitsEmittedBeforeDelta,
  _LLMContextContainsSnippets, _RagDisabledWhenDeny,
  _RagPolicyRevision, _PolicyRevisionRespectsSeverityCap,
  _PolicyRevisionAuditDeduplicated, _RagDropsHighRiskChunksForExternalLLM,
  _RagChunkSuspiciousPatternDetected, _RagChunkContentEscaped,
  _LLMEchoesRagSourceTagsStripped, _TopKBudgetTruncation,
  _AuditRagQueryWritten, _EndToEnd_PythonEmbedGoSearch (integration).

Ф4c — Контракт SSE event rag_hits + chat.schema.json.
  Файлы: docs/contracts/chat.schema.json, internal/api/chat.go (sseSink),
  internal/chat/event_sink.go.
  - В chat.schema.json добавить ChatRequest.rag (ref на
    rag.schema.json#RagRequestParams) и SseRagHits ({request_id, hits[]}).
  - В EventSink добавить метод RagHits([]RAGHit) error.
  - sseSink (HTTP layer) — JSON-сериализация без snippet'а
    (только метаданные источника).
  - В internal/api/chat.go ChatRequestDTO принимает поле rag, парсит в
    chat.RAGParams.
  - Golden Go↔Zod — пересборка contract_export_test.go +
    rubezh-web/src/test/contract.test.ts.

Ф5 — Frontend RAG toggle + источники.
  Файлы: rubezh-web/src/{pages/ChatPage.tsx, api/{schemas,sse}.ts,
  components/MessageBubble.tsx (если есть)}.
  - ChatPage: state useRag + localStorage.rubezh.chat.useRag, Switch
    «🔎 Искать по документам» над input'ом.
  - api/schemas.ts: RagHitSchema, RagParamsSchema, расширение
    ChatRequestSchema/ChatEventSchema (discriminated union для rag_hits).
  - api/sse.ts: парсер event rag_hits.
  - MessageBubble: footer «Источники:» с chip-list (filename + relevance%).
  - RTL-тесты: toggle виден, click → useRag в localStorage, request
    содержит rag.enabled, SSE rag_hits → chip'ы в bubble.

Итерация 16 — Финализация + e2e smoke.
  - docker compose up — все 6 сервисов healthy.
  - e2e smoke главного MVP-сценария (login → upload doc → chat с RAG →
    reveal → audit log → incident).
  - Базовая проверка prompt injection через RAG-чанк.
  - Сквозной тест на отсутствие raw в логах (grep app-логов после прогона).
  - Финальный README + чек-лист критериев MVP.

КОМАНДЫ ОКРУЖЕНИЯ:

Go тесты (только в Docker):
  docker run --rm -v c:/dev/RubezAI:/repo -v rubezh-go-cache:/go/pkg/mod \
    -w /repo/rubezh-api golang:1.25-bookworm go test -race ./...

Integration с живой БД (compose должен быть запущен):
  docker run --rm --network rubezh-ai_rubezh \
    -e TEST_DATABASE_URL=postgres://rubezh:rubezh@postgres:5432/rubezh?sslmode=disable \
    -v c:/dev/RubezAI:/repo -v rubezh-go-cache:/go/pkg/mod \
    -w /repo/rubezh-api golang:1.25-bookworm go test -race ./...

Worker (Python) тесты:
  docker run --rm -v c:/dev/RubezAI/rubezh-worker:/work -w /work \
    python:3.12-slim sh -c "pip install --quiet uv && uv sync --quiet && \
    uv run pytest && uv run ruff check app tests && uv run mypy app"

Frontend (TS) тесты:
  docker run --rm -v c:/dev/RubezAI/rubezh-web:/work -w /work \
    node:20-alpine sh -c "npm test"

Применить новую миграцию:
  docker compose run --rm migrate

ВАЖНЫЕ ИНВАРИАНТЫ (не нарушать):

1. raw ПДн НИКОГДА не пишется в application logs (LogValue redaction).
2. Внешние LLM (trust_level=external) получают ТОЛЬКО masked text.
3. Embedder cross-language symmetry — golden test должен оставаться
   зелёным. При смене алгоритма — обновлять обе golden-константы.
4. audit_events — append-only (триггер БД). Никаких UPDATE/DELETE.
5. ACL-предикат в SearchChunks ВСЕГДА первый и обязательный для
   не-supervisor — document_ids фильтр идёт AND ПОВЕРХ.
6. embedderName в SearchChunks ОБЯЗАТЕЛЕН (fail-closed
   ErrEmbedderNameRequired). Тесты должны явно передавать.
7. api.Deps.Embedder — ОБЯЗАТЕЛЬНОЕ поле. nil → panic в NewRouter.

СТАРТ С: Ф4b (интеграция RAG в orchestrator.Stream). Это самая большая
часть. Внутри Ф4b — сначала добавить Request.RAG + Retriever в
NewOrchestrator (без изменения Stream), запустить тесты — убедиться
что ничего не сломалось. Потом TDD на каждый из 14 тестов плана v2.2.

После Ф4b — Ф4c (контракт + SSE) — Ф5 (frontend) — Итерация 16.
```

---

## Дополнительно — для контекста новой сессии

- **LM Studio для real-семантики (опц.):** в LM Studio загрузить embedding-
  модель (`bge-m3` или `nomic-embed-text`), выставить env
  `EMBEDDER_KIND=openai_compatible EMBEDDER_URL=http://host.docker.internal:1234
  EMBEDDER_MODEL=bge-m3`. Без этого RAG работает на mock-семантике (тесты
  проходят, но релевантность в чате на live-документах слабая).
- **Compose-postgres должен быть запущен** для integration-тестов.
  Проверить: `docker ps --filter name=postgres`. Если нет — `docker
  compose up -d postgres minio`.
- **Сейчас в БД:** есть мусор от прежних прогонов тестов; пакет
  `internal/testdb` чистит artifacts с префиксом `itest_<pid>_` после
  каждого `go test ./...`.

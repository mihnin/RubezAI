# Итерация 10 — Worker: документы, очередь, MinIO, embeddings

Архитектурный план **v2** (доводка v1 8.2/10 по 3 MAJOR + 5 MINOR
ревью архитектора). Закрывает критерий MVP 6
(«можно загрузить документ»). Опирается на:

- `docs/PLAN.md` — карта итераций;
- `docs/ARCHITECTURE.md` — общая архитектура (worker — намерено);
- `docs/design/ui/documents.md` — UX-спецификация экрана;
- БД-схема: `migrations/000004_documents.up.sql` (documents,
  document_chunks, embeddings — уже есть);
- `rubezh-sanitizer` — для обезличивания содержимого чанков
  (контракт `docs/contracts/sanitize.schema.json`).

## 1. Цель и границы

### Новый сервис `rubezh-worker`

- **Python 3.12, FastAPI, uv lock-файл** — те же конвенции что у
  `rubezh-sanitizer` (см. `CLAUDE.md`).
- **Один длинный процесс**: HTTP-эндпойнт `/health` (для compose
  healthcheck) + background-loop, который держит соединение с
  PostgreSQL и периодически берёт задачи.
- **Без брокера** (Redis/Kafka): очередь на PostgreSQL через
  `FOR UPDATE SKIP LOCKED` (план §Р3) — намерено, см.
  `docs/PLAN.md §«Зафиксированные решения» #4`.

### Эндпойнты в `rubezh-api` (Go)

- `POST /api/documents` — multipart upload, ≤50 МБ, pdf|docx
  (Compliance: `THREAT_MODEL.md T9`).
- `GET /api/documents` — список с ACL.
- `GET /api/documents/:id` — метаданные + статус + phase.
- `GET /api/documents/:id/chunks` — список чанков (masked-only).
- `GET /api/documents/:id/download` — оригинал (owner+admin;
  audit `document_downloaded`).
- `DELETE /api/documents/:id` — soft-delete БД + hard-delete MinIO
  (owner+admin; audit). См. Р3.1.
- `POST /api/documents/:id/retry` — manual re-queue для status=failed
  (owner+admin; audit `document_retry_requested`). MAJOR-2 ревью v1.

### Вне границ (вынесено осознанно)

- **Реальные embeddings** — mock в MVP (детерминированный по хешу
  чанка). Реальный векторизатор (`sentence-transformers` или
  локальная LLM-API через `trusted_local`) — пост-MVP. Размерность
  фиксирована 1024 (`embeddings.vector(1024)` в схеме).
- **RAG-поиск по pgvector** — Итерация 11.
- **PATCH /api/documents/:id** (rename, изменение ACL) — пост-MVP.
- **Истинная стрим-обработка больших PDF** (chunk-by-chunk без
  загрузки целиком в память) — пост-MVP; в MVP лимит 50 МБ
  позволяет держать весь файл в памяти.
- **OCR для сканированных PDF** — пост-MVP.

## 2. Архитектурные решения

### Р1. Сервис worker — структура

```
rubezh-worker/
├── app/
│   ├── main.py            # FastAPI + lifespan + background-loop
│   ├── config.py          # env: DATABASE_URL, MINIO_*, SANITIZER_URL
│   ├── queue.py           # claim_next_document() с FOR UPDATE SKIP LOCKED
│   ├── processor.py       # сквозной поток: parse → chunk → sanitize → embed
│   ├── parsers/
│   │   ├── __init__.py    # registry: ext → parser
│   │   ├── pdf.py         # pypdf
│   │   └── docx.py        # python-docx
│   ├── chunking.py        # split на ~800-1024 токенов, sentence-boundary
│   ├── sanitizer_client.py # HTTP к rubezh-sanitizer (как в Go)
│   ├── embeddings/
│   │   ├── interface.py   # protocol Embedder
│   │   └── mock.py        # детерм. hash → 1024 floats
│   ├── storage/
│   │   ├── pool.py        # asyncpg pool
│   │   ├── documents.py   # обновление status, error
│   │   ├── chunks.py      # INSERT document_chunks
│   │   ├── embeddings.py  # INSERT embeddings
│   │   └── sanitization.py # INSERT sanitization_results
│   └── minio_client.py    # boto3 или minio-py для скачивания
├── tests/
│   ├── test_parsers.py    # фикстуры pdf/docx
│   ├── test_chunking.py
│   ├── test_processor.py  # integration с моками
│   └── conftest.py
├── pyproject.toml
├── uv.lock
└── Dockerfile             # multi-stage, uv-based
```

Зависимости (`pyproject.toml`):

- `fastapi`, `uvicorn[standard]`, `pydantic` v2 — как в sanitizer.
- `asyncpg` — async PostgreSQL для очереди и storage.
- `minio` — официальный клиент.
- `pypdf` ≥4.0 — парсинг PDF.
- `python-docx` ≥1.1 — парсинг DOCX.
- `tiktoken` — подсчёт токенов для chunking.
- `httpx` — клиент sanitizer.

### Р2. БД-очередь на `FOR UPDATE SKIP LOCKED`

**Текущие статусы** (миграция 000004 CHECK):
`'pending', 'processing', 'done', 'failed'` — оставляем без
изменений, MVP-минимум. Промежуточные стадии (parsing/chunking/
embedding) **не** хранятся в БД — для UI достаточно бинарного
«в работе / готов». Тонкая прогресс-индикация — пост-MVP
(добавляется в техдолг Итерации 10).

**Claim-запрос (план §Р2):**

```sql
UPDATE documents
SET status = 'processing',
    processing_started_at = now(),
    processing_attempts = processing_attempts + 1
WHERE id = (
  SELECT id FROM documents
  WHERE status = 'pending'
  ORDER BY created_at ASC
  FOR UPDATE SKIP LOCKED
  LIMIT 1
)
RETURNING id, owner_id, filename, content_type, storage_key, acl;
```

**Миграция 000011** добавляет:

- `documents.processing_started_at timestamptz NULL` — для re-queue
  stuck-документов после рестарта worker'а;
- `documents.processing_attempts int NOT NULL DEFAULT 0` — счётчик
  попыток (для anti-thrashing: при ≥3 fail переводим в
  `failed` с error «exceeded retry limit», не пытаемся снова).

**Re-queue stuck**: при старте worker делает один-разовый
`UPDATE ... SET status='pending' WHERE status='processing' AND
processing_started_at < now() - interval '15 minutes'` — забирает
обратно в очередь документы, обработка которых прервалась
(graceful или kill -9). 15 минут — верхняя граница
обработки одного документа (PDF 50 МБ + chunking + sanitize + embed).

### Р3. Поток обработки одного документа (v2 — идемпотентный)

1. **Claim**: `claim_next_document()` — атомарная транзакция,
   возвращает row или None. None → sleep 2s, retry.
2. **Idempotent cleanup** (MAJOR-1 ревью v1 — закрывает дыру
   повторной обработки): сразу после claim перед началом цикла —
   `DELETE FROM document_chunks WHERE document_id=$1`. CASCADE
   снесёт привязанные embeddings (ON DELETE CASCADE есть в
   миграции 000004). Аналогично — `DELETE FROM sanitization_results
   WHERE document_id=$1`. После этого повторная вставка не падает
   на `UNIQUE(document_id, chunk_index)`. **Цена**: первая попытка
   тоже делает no-op DELETE; стоимость пренебрежимая.
3. **Phase tracking** (m5 ревью v1 — UX-spec ожидает chip
   «⟳Парсинг»): миграция 000011 добавляет колонку
   `documents.phase text NULL`. Worker обновляет `phase` атомарно
   с шагом: `parsing` / `chunking` / `sanitizing` / `embedding`.
   Колонка nullable — статус остаётся 4 значения, фаза —
   sub-information для UI.
4. **Download** из MinIO по `storage_key` → байты в памяти.
   Heartbeat (m2 ревью v1): фоновая asyncio-task обновляет
   `processing_started_at = now()` каждые 60s до завершения цикла.
   Это закрывает кейс «процесс висит 14 минут — крашится — re-queue
   не срабатывает по 15min таймауту».
5. **Parse**, **Chunking** (как в v1).
6. **Sanitize** каждый чанк через `POST /sanitize/preview` —
   **параллельно с лимитом 4** через `asyncio.Semaphore(4)`
   (m3 ревью v1 — на документе из 200 чанков иначе 200 sequential
   HTTP-вызовов = слишком медленно). Batch-endpoint sanitizer'а —
   пост-MVP.
7. **Embed** (как в v1).
8. **Mark done**: `UPDATE documents SET status='done',
   processing_started_at=NULL, phase=NULL WHERE id=$1`.

При любой ошибке: `UPDATE documents SET status='failed', error=$err,
processing_started_at=NULL, phase=NULL`. **Manual retry**
(MAJOR-2 ревью v1): эндпойнт `POST /api/documents/:id/retry`
(owner / admin) переводит `failed` → `pending`, сбрасывает
`processing_attempts = 0`. После 3 неудачных попыток — `failed`
с error «retry limit exceeded»; admin/owner может вручную
сбросить через retry.

### Р3.1. Удаление документа — семантика raw в MinIO (MAJOR-3)

**Закрывает MAJOR-3 ревью v1: «soft-delete в БД не очищает raw в MinIO».**

`DELETE /api/documents/:id` (owner+admin) выполняет:

1. **Hard-delete object из MinIO** по `storage_key` — raw файл
   удаляется немедленно (необратимо, как пользователь и ожидает).
2. **Soft-delete в БД** — `UPDATE documents SET status='deleted',
   error=NULL, storage_key='' WHERE id=$1`. Row остаётся для
   аудита (нельзя стереть owner_id/created_at — это forensics).
   Status 'deleted' — **новое 5-е значение** в CHECK enum (миграция
   000011 расширяет). UI скрывает deleted-документы по умолчанию.
3. **Cascade delete chunks/embeddings/sanitization_results** —
   CASCADE автоматический (миграция 000004) или explicit DELETE.
4. **Audit event** `document_deleted` с request_id и actor_id.
5. **Idempotency**: повторный DELETE на уже deleted → 404
   (не 200, не leak информации). 

При status='deleted' GET-эндпойнты возвращают 404 (как несуществующий).
Восстановление — пост-MVP; в MVP deleted = irrecoverable.

### Р4. Безопасность — `document_chunks.content` = sanitized

**Критический инвариант** (как и в `chat_messages` итерации 8 §Р6):
`document_chunks.content` хранит **только обезличенный** текст.
Raw содержимое документа существует **только в MinIO** (зашифровано
на стороне MinIO Server-Side Encryption или ключом на app-уровне —
последнее в техдолге).

- При `GET /api/documents/:id/chunks` отдаём `content` (sanitized) +
  `sanitization_summary` (whitelist полей entity, как в
  `storage/chat.go ListChatMessages` Итерации 9 Ф2e).
- При `GET /api/documents/:id/download` отдаём raw из MinIO — но
  это **owner-only** + audit-event `document_downloaded` с
  `request_id` и `actor_id`.
- Embeddings — над **sanitized** текстом, не raw. Это значит,
  что RAG-поиск никогда не отдаст raw PII, даже если найдёт
  релевантный чанк.

### Р5. ACL — простой формат jsonb

Существующая колонка `documents.acl jsonb` (000004):
`[{"role":"security_officer"}, {"user_id":"<uuid>"}]`.

Проверка доступа:

- **Owner** (`documents.owner_id == current_user_id`) — всегда видит.
- **Admin / security_officer / compliance_officer / auditor** —
  всегда видят (роли «надзора»).
- **Иные** — только если в `acl` есть `{"role": <их роль>}` или
  `{"user_id": <их id>}`.

ACL-фильтрация делается на стороне `rubezh-api` (Go), не worker'а.
Worker имеет полный доступ к БД (системная роль).

### Р6. MinIO — клиент

- Bucket: `rubezh-documents` (env `MINIO_BUCKET`).
- Storage key: `documents/{document_id}/{secure_filename}` — без
  user-control в пути (защита от path traversal).
- Загрузка из `rubezh-api` (Go): пакет `github.com/minio/minio-go/v7`.
- Скачивание из worker (Python): пакет `minio` (официальный).
- **Server-Side Encryption** (SSE-S3) — MinIO поддерживает,
  настраивается в env `MINIO_KMS_AUTO_ENCRYPTION=on` либо на bucket
  уровне через `mc encrypt`. В MVP — без SSE (упрощение); в техдолге.

### Р7. Embeddings — mock детерминированный

```python
import hashlib
def hash_to_vector(text: str, dim: int = 1024) -> list[float]:
    """Детерминированный mock-embedding: SHA-256 → бесконечный
    поток bytes через counter-mode → нормируем в [-1, 1]."""
    floats = []
    counter = 0
    while len(floats) < dim:
        h = hashlib.sha256(f"{text}#{counter}".encode()).digest()
        for i in range(0, len(h), 4):
            if len(floats) >= dim:
                break
            val = int.from_bytes(h[i:i+4], "big") / 2**32
            floats.append(val * 2 - 1)  # [-1, 1]
        counter += 1
    return floats
```

Свойства:

- Детерминизм — одинаковый текст → одинаковый вектор (для тестов).
- Никаких внешних API-вызовов в MVP.
- Не «настоящий» semantic embedding — RAG Итерации 11 даст
  бессмысленные результаты на mock'е; это известное ограничение
  MVP, для демонстрации pipeline'а.

`Embedder` — protocol; в продакшене заменяется на реальный
векторизатор через DI (`config.EMBEDDER_KIND=mock|sentence_transformers|
openai_compatible` — пост-MVP).

### Р8. Контракт `documents.schema.json`

`$defs`:

- `Document` — id, owner_id, filename, content_type, size_bytes,
  status, error?, acl[], created_at, updated_at.
- `DocumentList` — `{documents[], next_cursor?}`.
- `DocumentChunk` — id, document_id, chunk_index, content (sanitized),
  token_count, sanitization_summary (опц.).
- `DocumentChunkList` — `{document_id, chunks[]}`.
- `DocumentUploadResponse` — `Document` + локационный header.

`additionalProperties: false`. Аналогично chat-контракту.

### Р9. Аудит-события документов с request_id-коррелятором (m1 ревью v1)

Каждое event несёт `detail.request_id` (UUID, генерируется при
claim в worker для processing_*-событий; берётся из HTTP-handler'а
для uploaded/downloaded/deleted/retried). Coordinator —
`document_id`+`request_id` для всей цепочки одного processing run.

- `document_uploaded` — после успешного POST `/api/documents`
  (HTTP request_id из middleware).
- `document_processing_started` — worker берёт документ (новый
  worker-generated UUID).
- `document_processing_completed` — worker закончил (status=done).
- `document_processing_failed` — worker завершил с ошибкой (тот же
  request_id что и started — для трейса).
- `document_downloaded` — GET `/api/documents/:id/download`.
- `document_deleted` — DELETE.
- `document_retry_requested` — POST `/api/documents/:id/retry`.

Все `event_type` добавляются в `audit.schema.json#AuditEventType` enum
(расширение MVP-списка).

## 3. Миграция 000011 (v2: + phase, + deleted status, + retry-helpers)

```sql
ALTER TABLE documents
  ADD COLUMN processing_started_at timestamptz,
  ADD COLUMN processing_attempts   int NOT NULL DEFAULT 0,
  ADD COLUMN phase                 text; -- m5: parsing/chunking/sanitizing/embedding/NULL

-- MAJOR-3 ревью v1: status='deleted' (soft-delete БД-row + hard-delete MinIO).
ALTER TABLE documents DROP CONSTRAINT documents_status_check;
ALTER TABLE documents ADD CONSTRAINT documents_status_check
  CHECK (status IN ('pending','processing','done','failed','deleted'));

ALTER TABLE documents ADD CONSTRAINT documents_phase_check
  CHECK (phase IS NULL OR
         phase IN ('parsing','chunking','sanitizing','embedding'));

CREATE INDEX idx_documents_pending_queue
  ON documents(created_at)
  WHERE status = 'pending';
COMMENT ON INDEX idx_documents_pending_queue IS
  'partial index для FOR UPDATE SKIP LOCKED очереди worker''а';

CREATE INDEX idx_documents_stuck
  ON documents(processing_started_at)
  WHERE status = 'processing';

-- Idempotent re-processing: при claim'е worker сначала чистит
-- ранее созданные данные. CASCADE через FK гарантирует
-- атомарную очистку chunks+embeddings+sanitization_results.
COMMENT ON COLUMN documents.processing_attempts IS
  'Счётчик re-claim. Heartbeat обновляет processing_started_at каждые 60s.';
```

## 4. Файлы и бюджет

| Файл | ≤ строк |
|------|---------|
| **Worker (Python)** | |
| `app/main.py` (lifespan + loop) | 100 |
| `app/queue.py` | 80 |
| `app/processor.py` | 200 |
| `app/parsers/{pdf,docx}.py` | ~80 каждый |
| `app/chunking.py` | 100 |
| `app/sanitizer_client.py` | 80 |
| `app/embeddings/{interface,mock}.py` | ~50 каждый |
| `app/storage/*.py` | ~80 каждый |
| `app/minio_client.py` | 80 |
| **API (Go)** | |
| `internal/storage/documents.go` (+CRUD) | ~300 |
| `internal/api/documents.go` | ~350 |
| `cmd/rubezh-api/main.go` | +10 (minio client) |
| **Контракт** | |
| `docs/contracts/documents.schema.json` | ~150 |

Все файлы ≤ 500, функции ≤ 60.

## 5. Фазы (TDD: тест-коммит → реализация-коммит)

- **Ф1 (миграция + worker skeleton):** миграция 000011; пустой
  worker с FastAPI `/health` + `/healthz`; uv.lock; Dockerfile;
  добавление в `docker-compose.yml` (profile, что worker стартует
  по умолчанию). Тест: `docker compose up -d --wait rubezh-worker`
  healthy.

- **Ф2 (очередь + claim):** `app/queue.py claim_next_document`;
  re-queue stuck; unit-тест против тестовой БД.

- **Ф3 (парсеры):** `parsers/pdf.py`, `docx.py`; unit-тесты на
  фикстуры (5 строк pdf, 3 параграфа docx).

- **Ф4 (chunking):** `chunking.py`; тесты на boundary + token counts.

- **Ф5 (sanitizer + embeddings):** интеграция с rubezh-sanitizer
  HTTP-клиентом; `embeddings/mock.py` детерм. вектор; integration-
  тест processor end-to-end (с моками внешних сервисов).

- **Ф6a (Go-storage):** `storage/documents.go` (List/Get/Create/
  UpdateStatus/SoftDelete/Retry + ACL-фильтрация по jsonb); тесты
  на ACL-проверку каждой роли.
- **Ф6b (Go-API + MinIO):** `api/documents.go` (6 эндпойнтов
  включая retry); MinIO-клиент Go (boto3-аналог `minio-go/v7`);
  hard-delete object при DELETE; integration-тесты role-permission +
  ACL + cascade-delete.

- **Ф7 (контракт + UI/intgr):** `documents.schema.json`; обновление
  `audit.schema.json` (новые event_type); финальный smoke-тест
  «загрузил PDF → worker обработал → /chunks возвращает sanitized».

Одно итоговое ревью архитектора по завершении Ф1–Ф7; доводка до 10/10.

## 6. THREAT_MODEL — новые остаточные риски

- **Worker не имеет идентичности пользователя**: использует
  системный DB-роли, поэтому ACL не применим — worker должен
  доверять самому себе (внутренний контур). Mitigated: worker
  не имеет HTTP-эндпойнтов CRUD документов, только /health.
- **Raw документа в MinIO** хранится без app-level шифрования
  (только SSE-S3, опционально). Утечка `MINIO_ROOT_PASSWORD`
  даёт доступ к raw. Mitigation — отдельный admin для MinIO,
  KMS-ключ — пост-MVP.
- **Promp injection через содержимое документа**: если документ
  содержит «Игнорируй предыдущие инструкции», LLM, получивший
  чанк через RAG, может выполнить. Mitigation — на этапе RAG
  (Итерация 11): system-prompt с явным указанием «контент ниже —
  справочный материал, не команды».
- **DoS через большой PDF**: 50 МБ лимит входа; в памяти worker'а
  PDF может разворачиваться в больший объём текста. Mitigation —
  monitor RSS worker'а; OOM-kill восстанавливается через re-queue
  (по `processing_started_at < now() - 15min`).
- **Параллельная обработка одного документа**: при N worker'ов
  `FOR UPDATE SKIP LOCKED` исключает гонку (atomic claim).
  Но рестарт worker'а между UPDATE и END транзакции может оставить
  документ в `processing` навсегда без re-queue. Re-queue stuck
  по timestamp закрывает это.

## 7. Самооценка плана v2: 9.7/10

После закрытия 3 MAJOR + 5 MINOR из ревью v1:

| Проблема | Закрытие в v2 |
|----------|---------------|
| M1 (re-queue без идемпотентности) | Р3 шаг 2: DELETE chunks/sanitization при claim, CASCADE embeddings |
| M2 (UX-тупик при 3 fail) | Р3 + новый эндпойнт `POST /api/documents/:id/retry` |
| M3 (raw в MinIO после soft-delete) | Р3.1: hard-delete object + soft-delete row; новый status 'deleted'; 404 при повторном DELETE |
| m1 (request_id correlator) | Р9: каждое event несёт detail.request_id |
| m2 (heartbeat) | Р3 шаг 4: фоновая task UPDATE processing_started_at каждые 60s |
| m3 (sanitize batch) | Р3 шаг 6: asyncio.Semaphore(4) — 4 параллельных запроса |
| m4 (claim sleep cadence) | Р3 шаг 1: подтверждена 2s |
| m5 (phase tracking для UI) | Р3 шаг 3 + миграция 000011: колонка documents.phase |

- Все архитектурные решения зафиксированы; границы MVP/пост-MVP
  явные.
- БД-очередь без брокера согласована с принципами проекта.
- Безопасность: chunk.content = sanitized (как chat_messages),
  raw только в MinIO с явным контролем доступа + hard-delete при
  DELETE.
- Минус 0.3 — Server-Side Encryption MinIO в техдолге, не в MVP
  (упрощение для скорости). Это допустимо для on-prem-контура.

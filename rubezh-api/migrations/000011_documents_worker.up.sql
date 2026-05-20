-- Итерация 10 — миграция 000011: worker очередь + phase + deleted-status.
--
-- Закрывает MAJOR-3 ревью архитектора плана (soft-delete БД-row +
-- hard-delete raw в MinIO; status='deleted' — новое значение enum)
-- и m5 (phase для UX-spec ⟳Парсинг chip).
--
-- Расширения:
-- 1. processing_started_at + processing_attempts для FOR UPDATE
--    SKIP LOCKED очереди + re-queue stuck.
-- 2. phase text NULL с CHECK enum (parsing/chunking/sanitizing/embedding).
-- 3. status enum расширен на 'deleted'.
-- 4. partial-индексы для быстрой работы очереди и stuck-recovery.

ALTER TABLE documents
  ADD COLUMN processing_started_at timestamptz,
  ADD COLUMN processing_attempts   int NOT NULL DEFAULT 0,
  ADD COLUMN phase                 text;

-- status: добавлено 'deleted' (soft-delete БД-row после hard-delete
-- raw в MinIO; DELETE-эндпойнт см. iteration-10.md §Р3.1).
ALTER TABLE documents DROP CONSTRAINT documents_status_check;
ALTER TABLE documents ADD CONSTRAINT documents_status_check
  CHECK (status IN ('pending','processing','done','failed','deleted'));

-- phase: sub-information для UI (chip «⟳Парсинг» / «⟳Чанкинг» / ...).
-- NULL = нет активной фазы (pending/done/failed/deleted).
ALTER TABLE documents ADD CONSTRAINT documents_phase_check
  CHECK (phase IS NULL OR
         phase IN ('parsing','chunking','sanitizing','embedding'));

-- Partial index для очереди: FOR UPDATE SKIP LOCKED по pending.
CREATE INDEX idx_documents_pending_queue
  ON documents(created_at)
  WHERE status = 'pending';
COMMENT ON INDEX idx_documents_pending_queue IS
  'partial index для FOR UPDATE SKIP LOCKED очереди worker''а (Итерация 10)';

-- Partial index для stuck-recovery (worker умер с status=processing).
CREATE INDEX idx_documents_stuck
  ON documents(processing_started_at)
  WHERE status = 'processing';
COMMENT ON INDEX idx_documents_stuck IS
  're-queue stuck документов: worker умер, processing_started_at < now()-15min';

COMMENT ON COLUMN documents.processing_started_at IS
  'Heartbeat: worker обновляет каждые 60s; NULL = не в обработке';
COMMENT ON COLUMN documents.processing_attempts IS
  'Счётчик re-claim. После 3 неудач → failed; manual retry эндпойнт сбрасывает в 0';
COMMENT ON COLUMN documents.phase IS
  'Sub-stage обработки: parsing/chunking/sanitizing/embedding (для UX-chip)';

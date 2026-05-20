-- Откат миграции 000011.
DROP INDEX IF EXISTS idx_documents_stuck;
DROP INDEX IF EXISTS idx_documents_pending_queue;

ALTER TABLE documents DROP CONSTRAINT IF EXISTS documents_phase_check;
ALTER TABLE documents DROP CONSTRAINT documents_status_check;
ALTER TABLE documents ADD CONSTRAINT documents_status_check
  CHECK (status IN ('pending','processing','done','failed'));

ALTER TABLE documents
  DROP COLUMN IF EXISTS phase,
  DROP COLUMN IF EXISTS processing_attempts,
  DROP COLUMN IF EXISTS processing_started_at;

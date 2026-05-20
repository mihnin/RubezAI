-- Откат миграции 000008.

-- 4. incident_notes.
DROP TRIGGER IF EXISTS incident_notes_append_only ON incident_notes;
DROP TABLE IF EXISTS incident_notes;

-- 3. Расширение incidents.
DROP TRIGGER IF EXISTS incidents_closed_at ON incidents;
DROP FUNCTION IF EXISTS incidents_set_closed_at();

DROP INDEX IF EXISTS idx_incidents_one_auto_per_event;
DROP INDEX IF EXISTS idx_incidents_assignee;
DROP INDEX IF EXISTS idx_incidents_severity;

ALTER TABLE incidents
  DROP COLUMN IF EXISTS closed_at,
  DROP COLUMN IF EXISTS assignee_id,
  DROP COLUMN IF EXISTS reporter_id;

-- 2. chat_messages.request_id.
DROP INDEX IF EXISTS idx_chat_messages_request_id;
ALTER TABLE chat_messages DROP COLUMN IF EXISTS request_id;

-- 1. Индексы audit_events.
DROP INDEX IF EXISTS idx_audit_events_detail_gin;
DROP INDEX IF EXISTS idx_audit_events_risk_level;
DROP INDEX IF EXISTS idx_audit_events_provider_created;
DROP INDEX IF EXISTS idx_audit_events_decision;

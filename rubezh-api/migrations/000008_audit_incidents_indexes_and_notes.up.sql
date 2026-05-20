-- Итерация 9 — миграция 000008: индексы аудита, инциденты v2, заметки.
--
-- Закрывает план iteration-9.md §3. Изменения:
--  1. Индексы для фильтров /api/audit-events (decision, risk_level,
--     provider+created_at, GIN по detail для has_leak-фильтра).
--  2. chat_messages.request_id — коррелятор пары user+assistant
--     (контракт chat.schema.json#ChatMessage.request_id).
--  3. incidents:
--     a) reporter_id NULL ⇔ auto (план §Р4);
--     b) assignee_id для UI «Назначить мне» (план §Р4);
--     c) closed_at + триггер автозаполнения при resolved/false_positive;
--     d) partial unique index на (audit_event_id) WHERE reporter_id IS NULL
--        — race-safe защита от дублей auto-инцидентов (план MAJOR-1 v1).
--  4. incident_notes — append-only заметки расследователя.

-- ---------------------------------------------------------------------------
-- 1. Индексы для фильтров audit_events.
-- ---------------------------------------------------------------------------
CREATE INDEX idx_audit_events_decision
  ON audit_events(policy_decision)
  WHERE policy_decision IS NOT NULL;

CREATE INDEX idx_audit_events_provider_created
  ON audit_events(model_provider_id, created_at)
  WHERE model_provider_id IS NOT NULL;

CREATE INDEX idx_audit_events_risk_level
  ON audit_events(risk_level)
  WHERE risk_level IS NOT NULL;

-- GIN по detail для фильтра has_leak ((detail->>'response_leak_detected')='true')
-- и для возможных аналогичных queries по jsonb.
CREATE INDEX idx_audit_events_detail_gin
  ON audit_events USING GIN (detail);

-- ---------------------------------------------------------------------------
-- 2. chat_messages.request_id — коррелятор пары.
-- ---------------------------------------------------------------------------
ALTER TABLE chat_messages ADD COLUMN request_id text;

-- Индекс частичный: для старых сообщений (до Итерации 9) request_id = NULL
-- и в индекс не попадают. Для новых — позволяет быстро найти пару user+assistant.
CREATE INDEX idx_chat_messages_request_id
  ON chat_messages(request_id)
  WHERE request_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 3. Расширение incidents.
-- ---------------------------------------------------------------------------
ALTER TABLE incidents
  ADD COLUMN reporter_id uuid REFERENCES users(id),
  ADD COLUMN assignee_id uuid REFERENCES users(id),
  ADD COLUMN closed_at   timestamptz;

-- Семантика: reporter_id IS NULL ⇔ auto-инцидент;
-- reporter_id IS NOT NULL ⇔ manual (создан пользователем).
COMMENT ON COLUMN incidents.reporter_id IS
  'NULL = auto (создан системой); UUID = manual (создан пользователем)';
COMMENT ON COLUMN incidents.assignee_id IS
  'Назначенный расследователь (NULL = не назначен)';
COMMENT ON COLUMN incidents.closed_at IS
  'Время закрытия: автоматически при resolved/false_positive';

CREATE INDEX idx_incidents_severity ON incidents(severity);
CREATE INDEX idx_incidents_assignee
  ON incidents(assignee_id)
  WHERE assignee_id IS NOT NULL;

-- Partial unique: один auto-инцидент на audit_event_id. Race-safe
-- защита от дублей при параллельных вставках (план MAJOR-1 v1).
-- Manual-инциденты могут добавляться сверху того же audit_event_id.
CREATE UNIQUE INDEX idx_incidents_one_auto_per_event
  ON incidents(audit_event_id)
  WHERE reporter_id IS NULL;

-- Триггер автозаполнения closed_at при переходе в/из терминального статуса.
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

-- ---------------------------------------------------------------------------
-- 4. incident_notes — append-only заметки.
-- ---------------------------------------------------------------------------
CREATE TABLE incident_notes (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  incident_id uuid NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  author_id   uuid NOT NULL REFERENCES users(id),
  content     text NOT NULL
    CHECK (char_length(content) BETWEEN 1 AND 2000),
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_incident_notes_incident
  ON incident_notes(incident_id, created_at);

-- Append-only: триггер rubezh_block_mutation определён в миграции 000003.
CREATE TRIGGER incident_notes_append_only
  BEFORE UPDATE OR DELETE ON incident_notes
  FOR EACH ROW EXECUTE FUNCTION rubezh_block_mutation();

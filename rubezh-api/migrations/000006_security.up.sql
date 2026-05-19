-- Итерация 1 — миграция 000006: mapping псевдонимов, аудит, инциденты.

-- Обратимые псевдонимы. raw-значение хранится ТОЛЬКО зашифрованным (AES-GCM,
-- шифрует приложение — итерация 4). Колонок с raw-значением в открытом виде нет.
CREATE TABLE pseudonym_mappings (
  id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  sanitization_result_id uuid REFERENCES sanitization_results(id) ON DELETE SET NULL,
  pseudonym              text NOT NULL,
  entity_type            text NOT NULL,
  raw_hash               text NOT NULL,
  raw_value_encrypted    bytea NOT NULL,
  created_at             timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_pseudonym_mappings_result ON pseudonym_mappings(sanitization_result_id);
CREATE INDEX idx_pseudonym_mappings_hash ON pseudonym_mappings(raw_hash);

-- Журнал аудита — append-only. Хранит риск-классы и masked-представление.
CREATE TABLE audit_events (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id           uuid REFERENCES users(id),
  event_type        text NOT NULL,
  model_provider_id uuid REFERENCES model_providers(id),
  risk_level        text,
  risk_classes      text[] NOT NULL DEFAULT '{}',
  policy_decision   text,
  -- Ссылка на неизменяемую версию политики — решение воспроизводимо во времени.
  policy_version_id uuid REFERENCES policy_versions(id),
  matched_rule      text,
  masked_payload    text,
  detail            jsonb NOT NULL DEFAULT '{}',
  created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_events_user ON audit_events(user_id);
CREATE INDEX idx_audit_events_created ON audit_events(created_at);
CREATE INDEX idx_audit_events_type ON audit_events(event_type);

-- Append-only: UPDATE и DELETE запрещены триггером.
CREATE TRIGGER audit_events_append_only
  BEFORE UPDATE OR DELETE ON audit_events
  FOR EACH ROW EXECUTE FUNCTION rubezh_block_mutation();

CREATE TABLE incidents (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  -- Инцидент обычно порождается audit-событием; ручное создание без него допустимо.
  audit_event_id uuid REFERENCES audit_events(id),
  user_id        uuid REFERENCES users(id),
  severity       text NOT NULL DEFAULT 'medium'
    CHECK (severity IN ('low','medium','high','critical')),
  status         text NOT NULL DEFAULT 'open'
    CHECK (status IN ('open','investigating','resolved','false_positive')),
  title          text NOT NULL,
  summary        text,
  resolution     text,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_incidents_status ON incidents(status);
CREATE INDEX idx_incidents_user ON incidents(user_id);
CREATE INDEX idx_incidents_audit_event ON incidents(audit_event_id);

CREATE TRIGGER incidents_set_updated_at BEFORE UPDATE ON incidents
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

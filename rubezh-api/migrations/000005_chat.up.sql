-- Итерация 1 — миграция 000005: чат-сессии, сообщения, результаты обезличивания.

CREATE TABLE chat_sessions (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    uuid NOT NULL REFERENCES users(id),
  title      text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_chat_sessions_user ON chat_sessions(user_id);

CREATE TABLE chat_messages (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id        uuid NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
  role              text NOT NULL CHECK (role IN ('user','assistant','system')),
  content           text NOT NULL,
  model_provider_id uuid REFERENCES model_providers(id),
  created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_chat_messages_session ON chat_messages(session_id);

-- Результат обезличивания: сущности хранятся без raw-значений.
-- Это часть доказательной базы (forensics) и переживает удаление источника:
-- FK на сообщение/документ — ON DELETE SET NULL, сама запись сохраняется.
-- Наличие хотя бы одной ссылки при создании — инвариант приложения.
CREATE TABLE sanitization_results (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  message_id   uuid REFERENCES chat_messages(id) ON DELETE SET NULL,
  document_id  uuid REFERENCES documents(id) ON DELETE SET NULL,
  risk_level   text NOT NULL CHECK (risk_level IN ('low','medium','high','critical')),
  risk_score   real NOT NULL CHECK (risk_score >= 0 AND risk_score <= 1),
  risk_classes text[] NOT NULL DEFAULT '{}',
  entities     jsonb NOT NULL DEFAULT '[]',
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_sanitization_results_message ON sanitization_results(message_id);
CREATE INDEX idx_sanitization_results_document ON sanitization_results(document_id);

CREATE TRIGGER chat_sessions_set_updated_at BEFORE UPDATE ON chat_sessions
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

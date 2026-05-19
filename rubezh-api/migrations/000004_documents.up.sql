-- Итерация 1 — миграция 000004: документы, чанки, embeddings.

CREATE TABLE documents (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id     uuid NOT NULL REFERENCES users(id),
  filename     text NOT NULL,
  content_type text,
  size_bytes   bigint CHECK (size_bytes IS NULL OR size_bytes >= 0),
  storage_key  text NOT NULL,
  status       text NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','processing','done','failed')),
  error        text,
  -- ACL: JSON-массив элементов {"role":"..."} | {"user_id":"..."} с доступом.
  acl          jsonb NOT NULL DEFAULT '[]',
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_documents_status ON documents(status);
CREATE INDEX idx_documents_owner ON documents(owner_id);

CREATE TABLE document_chunks (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  chunk_index integer NOT NULL CHECK (chunk_index >= 0),
  content     text NOT NULL,
  token_count integer CHECK (token_count IS NULL OR token_count >= 0),
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (document_id, chunk_index)
);
CREATE INDEX idx_document_chunks_document ON document_chunks(document_id);

-- Размерность вектора для MVP — 1024 (mock-embeddings, см. docs/PLAN.md).
CREATE TABLE embeddings (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  chunk_id   uuid NOT NULL UNIQUE REFERENCES document_chunks(id) ON DELETE CASCADE,
  model      text NOT NULL,
  dim        integer NOT NULL CHECK (dim = 1024),
  embedding  vector(1024) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_embeddings_vector ON embeddings
  USING hnsw (embedding vector_cosine_ops);

CREATE TRIGGER documents_set_updated_at BEFORE UPDATE ON documents
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Итерация 1 — миграция 000003: провайдеры моделей и политики.

CREATE TABLE model_providers (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name               text NOT NULL UNIQUE,
  trust_level        text NOT NULL
    CHECK (trust_level IN ('external','russian_cloud','on_prem','trusted_local')),
  adapter            text NOT NULL CHECK (adapter IN ('mock','openai_compatible')),
  endpoint           text,
  max_tokens         integer CHECK (max_tokens IS NULL OR max_tokens > 0),
  rate_limit_per_min integer CHECK (rate_limit_per_min IS NULL OR rate_limit_per_min > 0),
  is_enabled         boolean NOT NULL DEFAULT true,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE policies (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name            text NOT NULL UNIQUE,
  description     text,
  is_active       boolean NOT NULL DEFAULT true,
  -- Указатель на актуальную policy_versions.version; целостность поддерживается
  -- приложением (создание политики предшествует созданию её первой версии).
  current_version integer NOT NULL DEFAULT 1 CHECK (current_version >= 1),
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Версии политик неизменяемы — отсюда только created_at.
CREATE TABLE policy_versions (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  policy_id  uuid NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
  version    integer NOT NULL CHECK (version >= 1),
  rules      jsonb NOT NULL DEFAULT '{}',
  created_by uuid REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (policy_id, version)
);
CREATE INDEX idx_policy_versions_policy ON policy_versions(policy_id);

CREATE TRIGGER model_providers_set_updated_at BEFORE UPDATE ON model_providers
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER policies_set_updated_at BEFORE UPDATE ON policies
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

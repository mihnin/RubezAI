-- Итерация L — персональные API-ключи провайдеров на пользователя.
-- Сотрудник подключает СВОЙ ключ к org-провайдеру; в чате используется его
-- ключ. Ключ шифруется AES-256-GCM (AAD = user_id + credential_id). Плейнтекст
-- наружу не отдаётся. trust_level/endpoint остаются от org-провайдера
-- (персональный ключ меняет только идентичность перед вендором, не masking).
CREATE TABLE user_provider_credentials (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id           uuid NOT NULL REFERENCES users(id),
  provider_id       uuid NOT NULL REFERENCES model_providers(id) ON DELETE CASCADE,
  api_key_encrypted bytea NOT NULL,
  label             text,
  is_enabled        boolean NOT NULL DEFAULT true,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  last_used_at      timestamptz,
  UNIQUE (user_id, provider_id)
);

CREATE INDEX idx_user_provider_credentials_user
  ON user_provider_credentials(user_id);

CREATE TRIGGER user_provider_credentials_set_updated_at
  BEFORE UPDATE ON user_provider_credentials
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

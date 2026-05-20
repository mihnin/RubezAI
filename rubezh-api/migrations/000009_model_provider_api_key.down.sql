-- Откат миграции 000009.
ALTER TABLE model_providers DROP COLUMN IF EXISTS api_key_encrypted;

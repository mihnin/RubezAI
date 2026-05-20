-- Итерация 9.5 — миграция 000009: per-provider зашифрованный api_key.
--
-- Закрывает техдолг Итерации 7 (PLAN.md «Технический долг»):
-- единый LLM_API_KEY env-key больше не нужен — каждый openai_compatible
-- провайдер хранит свой ключ в зашифрованной форме.
--
-- Шифрование — AES-256-GCM из internal/crypto с тем же ключом
-- MAPPING_ENCRYPTION_KEY (один app-level key — простой MVP-подход;
-- разделение ключей mapping/api_key — пост-MVP). Формат:
-- nonce(12) || ciphertext || GCM-tag(16).
-- AAD = []byte("model_provider_api_key:" || provider.name) — уникальная
-- per-провайдер привязка; при переименовании ключ становится нечитаемым
-- (требуется ре-ввод — это намеренно, переименование редкое действие).

ALTER TABLE model_providers ADD COLUMN api_key_encrypted bytea;

COMMENT ON COLUMN model_providers.api_key_encrypted IS
  'Зашифрованный API-ключ (AES-256-GCM, internal/crypto, AAD=name). NULL = ключ не задан, провайдер использует env LLM_API_KEY (deprecated, для backward compat). НЕ возвращается в DTO.';

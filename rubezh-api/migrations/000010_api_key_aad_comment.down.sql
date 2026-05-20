-- Откат миграции 000010: возврат старого описания (миграция 000009).
COMMENT ON COLUMN model_providers.api_key_encrypted IS
  'Зашифрованный API-ключ (AES-256-GCM, internal/crypto, AAD=name). NULL = ключ не задан, провайдер использует env LLM_API_KEY (deprecated, для backward compat). НЕ возвращается в DTO.';

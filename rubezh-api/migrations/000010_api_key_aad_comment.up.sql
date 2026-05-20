-- Итерация 9.5 доводка — миграция 000010: исправление комментария
-- api_key_encrypted после перехода на AAD=id.
--
-- Миграция 000009 описывала "AAD=name; при переименовании ключ
-- становится нечитаемым" — это устарело: в финальной реализации
-- (MINOR-1 ревью архитектора) AAD = id (UUID, иммутабельный).
-- Эта миграция обновляет только COMMENT ON COLUMN (без изменения
-- данных или схемы) — DBA в `\d+ model_providers` увидят актуальное
-- описание.

COMMENT ON COLUMN model_providers.api_key_encrypted IS
  'Зашифрованный API-ключ (AES-256-GCM, internal/crypto, AAD=id UUID — иммутабельный, rename провайдера НЕ ломает ключ). NULL = ключ не задан, провайдер использует env LLM_API_KEY (deprecated, backward-compat). НЕ возвращается в DTO.';

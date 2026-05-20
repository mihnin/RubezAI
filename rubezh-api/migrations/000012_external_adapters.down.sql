-- Откат: возвращаем ограничение к двум адаптерам. Если в БД есть anthropic-
-- провайдеры, миграция упадёт — придётся сначала удалить такие записи.
ALTER TABLE model_providers DROP CONSTRAINT IF EXISTS model_providers_adapter_check;
ALTER TABLE model_providers
  ADD CONSTRAINT model_providers_adapter_check
  CHECK (adapter IN ('mock', 'openai_compatible'));

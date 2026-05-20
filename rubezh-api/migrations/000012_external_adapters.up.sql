-- Итерация H: внешние провайдеры (Claude, ChatGPT, Gemini, Grok, DeepSeek cloud).
-- Расширяет CHECK constraint adapter'ов на 'anthropic'. Grok/Gemini/DeepSeek
-- используют существующий 'openai_compatible' (их API совместимы с OpenAI).

ALTER TABLE model_providers DROP CONSTRAINT IF EXISTS model_providers_adapter_check;
ALTER TABLE model_providers
  ADD CONSTRAINT model_providers_adapter_check
  CHECK (adapter IN ('mock', 'openai_compatible', 'anthropic'));

-- Итерация H: seed внешних провайдеров для chat (без api_key — вводится
-- через UI /api/models/:id/api-key или env).
--
-- Все 5 моделей помечены is_enabled = FALSE: включаются только после
-- ввода ключа, чтобы /api/chat не падал на 401 у непрописанного.
--
-- Endpoint'ы:
-- - OpenAI / DeepSeek / Grok / Gemini — OpenAI-совместимые (adapter=openai_compatible)
-- - Claude — собственный Messages API (adapter=anthropic, миграция 000012)
--
-- ON CONFLICT DO NOTHING — идемпотентность (повторный run миграций не падает).

INSERT INTO model_providers (id, name, trust_level, adapter, endpoint, max_tokens, is_enabled)
VALUES
  (gen_random_uuid(), 'openai-gpt', 'external', 'openai_compatible',
    'https://api.openai.com/v1', 4096, FALSE),
  (gen_random_uuid(), 'anthropic-claude', 'external', 'anthropic',
    'https://api.anthropic.com', 4096, FALSE),
  (gen_random_uuid(), 'google-gemini', 'external', 'openai_compatible',
    'https://generativelanguage.googleapis.com/v1beta/openai', 4096, FALSE),
  (gen_random_uuid(), 'xai-grok', 'external', 'openai_compatible',
    'https://api.x.ai/v1', 4096, FALSE),
  (gen_random_uuid(), 'deepseek-cloud', 'external', 'openai_compatible',
    'https://api.deepseek.com/v1', 4096, FALSE)
ON CONFLICT (name) DO NOTHING;

-- Локальная DeepSeek-7B (через LM Studio) — НЕ для chat, а для LLM-review
-- внутри sanitizer (см. docs/ARCHITECTURE.md §2.1 фильтр 2/3). Выключаем
-- из chat-набора (sanitizer обращается напрямую через SANITIZER_LLM_URL).
UPDATE model_providers
   SET is_enabled = FALSE
 WHERE name = 'deepseek-local' AND adapter = 'openai_compatible';

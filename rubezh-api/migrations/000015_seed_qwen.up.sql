-- Итерация L+ — добавление провайдера Qwen (Alibaba DashScope).
-- OpenAI-совместимый endpoint (adapter=openai_compatible, Bearer-ключ).
-- Засевается выключенным (is_enabled=false) — включается после ввода ключа.
INSERT INTO model_providers (id, name, trust_level, adapter, endpoint, max_tokens, is_enabled)
VALUES (gen_random_uuid(), 'qwen-cloud', 'external', 'openai_compatible',
        'https://dashscope-intl.aliyuncs.com/compatible-mode/v1', 4096, FALSE)
ON CONFLICT (name) DO NOTHING;

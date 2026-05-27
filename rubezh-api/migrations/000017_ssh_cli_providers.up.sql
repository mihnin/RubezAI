-- Итерация SSH-CLI bridge: новый adapter ssh_cli + seed CLI-провайдеров.
--
-- Подключает внешние LLM через CLI-bridge на удалённом сервере
-- (codex/claude/gemini/grok CLI уже залогинены серверной учёткой aiagent).
-- API-ключи провайдеров не используются: аутентификация на сервере,
-- репозиторий не хранит ни пароля, ни OAuth-кодов, ни приватных ключей.
--
-- Endpoint у adapter=ssh_cli — это первый аргумент ai-bridge
-- (codex|claude|gemini|grok), а не URL. Валидация — в
-- internal/api/models.go validSSHCLIEndpoint.

ALTER TABLE model_providers DROP CONSTRAINT IF EXISTS model_providers_adapter_check;
ALTER TABLE model_providers
  ADD CONSTRAINT model_providers_adapter_check
  CHECK (adapter IN ('mock', 'openai_compatible', 'anthropic', 'ssh_cli'));

-- Seed-провайдеры. Codex/Claude/Gemini включены по умолчанию (на сервере
-- залогинены OAuth/API-сессии). Grok оставлен disabled до завершения
-- серверной авторизации Grok. ON CONFLICT (name) DO NOTHING — повторный
-- run миграций идемпотентен.
INSERT INTO model_providers
    (id, name, trust_level, adapter, endpoint, max_tokens, is_enabled)
VALUES
    (gen_random_uuid(), 'codex-cli',        'external', 'ssh_cli',
        'codex',  4096, TRUE),
    (gen_random_uuid(), 'claude-code-cli',  'external', 'ssh_cli',
        'claude', 4096, TRUE),
    (gen_random_uuid(), 'gemini-cli',       'external', 'ssh_cli',
        'gemini', 4096, TRUE),
    (gen_random_uuid(), 'grok-cli',         'external', 'ssh_cli',
        'grok',   4096, FALSE)
ON CONFLICT (name) DO NOTHING;

-- Откат: удаляем seed-провайдеры. Может упасть на FK от chat_messages,
-- если уже была активность — это нормально, миграцию откатывают редко.
DELETE FROM model_providers
 WHERE name IN ('openai-gpt', 'anthropic-claude', 'google-gemini',
                'xai-grok', 'deepseek-cloud');

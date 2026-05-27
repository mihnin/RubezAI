-- Откат: soft-disable seed-провайдеров ssh_cli и вернуть CHECK к набору 000012.
--
-- MAJOR-M2 fix: НЕ делаем DELETE. На провайдеров уже могут ссылаться
-- chat_messages.model_provider_id / audit_events.model_provider_id —
-- DELETE → FK 23503 → golang-migrate откатит транзакцию, и CHECK не
-- успеет вернуться к старому набору. Soft-disable безопасен: запись
-- остаётся, append-only история не ломается, но провайдеры выпадают из
-- chat-picker'а.
--
-- При этом CHECK adapter возвращается к набору 000012 (без 'ssh_cli'),
-- что СЛОМАЕТ существующие seed-записи: они продолжают иметь
-- adapter='ssh_cli', нарушая новый CHECK. Поэтому миграция вниз снимает
-- adapter с провайдеров ssh_cli, ставя 'mock' (он не маршрутизирует
-- внешние запросы — fail-closed). Это компромисс отката с сохранением
-- audit FK-цепочки.

UPDATE model_providers
   SET is_enabled = FALSE,
       adapter    = 'mock'
 WHERE name IN ('codex-cli', 'claude-code-cli', 'gemini-cli', 'grok-cli')
   AND adapter = 'ssh_cli';

ALTER TABLE model_providers DROP CONSTRAINT IF EXISTS model_providers_adapter_check;
ALTER TABLE model_providers
  ADD CONSTRAINT model_providers_adapter_check
  CHECK (adapter IN ('mock', 'openai_compatible', 'anthropic'));

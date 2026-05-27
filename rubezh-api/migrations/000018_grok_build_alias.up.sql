-- Сервер bridge теперь принимает первым аргументом 'grok-build' (не 'grok').
-- Старый alias 'grok' оставлен на сервере для совместимости, но в приложении
-- используем 'grok-build'. Обновляем существующий seed-провайдер grok-cli:
--   * переименовать в grok-build
--   * endpoint = grok-build
--   * enable
-- Идемпотентно: UPDATE только если запись с adapter='ssh_cli' существует.

UPDATE model_providers
   SET name       = 'grok-build',
       endpoint   = 'grok-build',
       is_enabled = TRUE
 WHERE name = 'grok-cli'
   AND adapter = 'ssh_cli';

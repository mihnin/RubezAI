-- Откат: вернуть grok-build → grok-cli/endpoint=grok, disable.
UPDATE model_providers
   SET name       = 'grok-cli',
       endpoint   = 'grok',
       is_enabled = FALSE
 WHERE name = 'grok-build'
   AND adapter = 'ssh_cli';

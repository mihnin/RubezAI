-- model_providers.default_model — дефолтное имя модели (для adapter=ssh_cli
-- это model id, который реально принимает CLI на удалённом сервере).
--
-- Зачем: убрать хардкод «provider name → model id» из 6 мест (UI, CLI,
-- chat.go, ssh_cli.go, ProviderModelPicker, ai-bridge.py). Default
-- хранится в БД; API/UI берут его оттуда; bridge остаётся последним
-- рубежом устойчивости с фиксированным fallback (см. ai-bridge.py).
--
-- TEXT NOT NULL DEFAULT '': пусто означает «adapter сам решит» (для
-- openai_compatible/anthropic — это исторически provider.Name; для
-- ssh_cli — defaultSSHModelFor по endpoint).

ALTER TABLE model_providers
    ADD COLUMN IF NOT EXISTS default_model TEXT NOT NULL DEFAULT '';

-- Seed для уже существующих ssh_cli-провайдеров (Codex/Claude/Gemini/
-- Grok). Только модели, подтверждённые live smoke через bridge — не
-- ставим gpt-5.5-codex / grok-4.3 пока CLI на сервере их не принимает.
-- Gemini endpoint теперь обслуживается Antigravity CLI (`agy`), где модель
-- выбирается display-name строкой в ~/.gemini/antigravity-cli/settings.json.
UPDATE model_providers SET default_model = 'gpt-5.3-codex'
 WHERE name = 'codex-cli'       AND adapter = 'ssh_cli';
UPDATE model_providers SET default_model = 'claude-opus-4-7'
 WHERE name = 'claude-code-cli' AND adapter = 'ssh_cli';
UPDATE model_providers SET default_model = 'Gemini 3.5 Flash (High)'
 WHERE name = 'gemini-cli'      AND adapter = 'ssh_cli';
UPDATE model_providers SET default_model = 'grok-build'
 WHERE name = 'grok-build'      AND adapter = 'ssh_cli';

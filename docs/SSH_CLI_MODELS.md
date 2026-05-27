# Управление моделями SSH-CLI провайдеров

Документ описывает, как добавлять и менять model id для провайдеров
`adapter=ssh_cli` в RubezAI (Codex / Claude / Gemini-Antigravity / Grok через
`/usr/local/bin/ai-bridge` на удалённом сервере).

## Где хранится default model

Колонка `model_providers.default_model` (миграция
[000019](../rubezh-api/migrations/000019_model_provider_default_model.up.sql)).
Дефолт берут (в порядке приоритета):

1. явный `model` в теле `POST /api/chat` (UI/CLI/любой клиент);
2. `provider.default_model` из БД (то, что подставляет API при пустом
   `model` — см. `internal/api/chat.go::modelOrDefault`);
3. для `adapter=ssh_cli` — встроенный fallback в
   `internal/llm/ssh_cli.go::defaultSSHModelFor` (последний рубеж
   устойчивости, обновляется только если CLI на сервере меняет API);
4. для остального — `provider.name` (исторический поведение для
   `openai_compatible` / `anthropic`).

Bridge на сервере имеет свой собственный fallback `DEFAULT_MODELS` в
`deploy/ssh-bridge/ai-bridge.py`, но он применяется только если RubezAI
прислал пустой `model` или старый alias. В обычном flow `default_model`
из БД доходит до bridge без изменений.

## Текущие подтверждённые дефолты (smoke ✓)

| Provider           | Endpoint     | default_model      |
|--------------------|--------------|--------------------|
| `codex-cli`        | `codex`      | `gpt-5.3-codex`    |
| `claude-code-cli`  | `claude`     | `claude-opus-4-7`  |
| `gemini-cli`       | `gemini`     | `Gemini 3.5 Flash (High)` |
| `grok-build`       | `grok-build` | `grok-build`       |

Примечание: endpoint `gemini` теперь вызывает **Google Antigravity CLI**
(`agy`), а не старый `gemini` binary. Model задаётся display-name строкой
Antigravity и записывается в `~/.gemini/antigravity-cli/settings.json`.

## Известные НЕ работающие модели / старые alias

(Пожалуйста, не ставьте их в `default_model` без свежей проверки.)

| Provider | Не работает        |
|----------|--------------------|
| Codex    | `gpt-5.5-codex`    |
| Gemini legacy CLI | `gemini-3.5-flash` |
| Gemini legacy CLI | `gemini-2.5-pro` после перехода на Antigravity считается старым alias |
| Grok     | `grok-4.3`         |

## Как проверить новую модель

1. Проверьте, что CLI на сервере принимает model через bridge:

   ```powershell
   '{"prompt":"Reply OK only","model":"MODEL_ID"}' `
     | ssh -T -i C:\Users\Mih10\.ssh\rubezai_ruvds_aiagent_ed25519 `
           aiagent@193.124.93.157 /usr/local/bin/ai-bridge PROVIDER
   ```

   `PROVIDER` — один из `codex`, `claude`, `gemini`, `grok-build`.
   Для `gemini` это проверяет Antigravity CLI (`agy`), установленный под
   пользователем `aiagent`.
   Ожидаемый успех:

   ```json
   {"ok": true, "provider": "...", "model": "MODEL_ID", "content": "OK"}
   ```

2. Если `ok: true` — обновите `default_model` через API:

   ```bash
   curl -X PATCH http://localhost:8080/api/models/<provider_id> \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"default_model":"MODEL_ID"}'
   ```

   `provider_id` найти через `GET /api/models` или
   `rubezh models list`.

3. Если bridge вернул `ok: false` — оставьте текущий default, добавьте
   ID в список «не работают» в этом файле.

**Никогда** не меняйте `default_model` без подтверждённого `ok: true`
smoke-теста: RubezAI сразу начнёт слать запросы с новым model id, и при
несовместимости все запросы к провайдеру упадут.

## Когда нужны env-override на стороне bridge

Если у вас несколько инсталляций RubezAI с одним общим bridge и нужен
глобальный override (без правки каждой БД), используйте env-переменные
на сервере:

- `AI_BRIDGE_DEFAULT_CODEX_MODEL`
- `AI_BRIDGE_DEFAULT_CLAUDE_MODEL`
- `AI_BRIDGE_DEFAULT_GEMINI_MODEL`
- `AI_BRIDGE_DEFAULT_GROK_MODEL`

В обычной on-prem установке (одна БД RubezAI ↔ один bridge) этого
делать не надо — управляйте через `default_model` в БД.

## Переключение Gemini на Antigravity

На сервере `gemini` endpoint внутри bridge по умолчанию использует
Antigravity CLI:

```bash
AI_BRIDGE_GEMINI_BACKEND=antigravity
```

Для аварийного отката на старый `gemini` binary можно временно запустить
bridge с:

```bash
AI_BRIDGE_GEMINI_BACKEND=legacy
```

Официальная установка Antigravity CLI:

```bash
curl -fsSL https://antigravity.google/cli/install.sh | bash
```

После установки нужна интерактивная Google OAuth-авторизация под тем же
пользователем, от которого запускается bridge (`aiagent`).

## Что менять, если придёт новый CLI/endpoint

1. Подтвердить smoke (см. §«Как проверить новую модель»).
2. Расширить whitelist в трёх местах (defense-in-depth):
   - `rubezh-api/internal/llm/ssh_cli.go::validSSHProviderArg`
   - `rubezh-api/internal/api/models.go::validSSHCLIEndpoint`
   - `deploy/ssh-bridge/ai-bridge.py::VALID_PROVIDERS`
3. Завести миграцию (по образцу
   [000017](../rubezh-api/migrations/000017_ssh_cli_providers.up.sql)
   или [000018](../rubezh-api/migrations/000018_grok_build_alias.up.sql)),
   которая засеет нового провайдера с правильным `default_model`.
4. Обновить этот файл (таблицы выше).

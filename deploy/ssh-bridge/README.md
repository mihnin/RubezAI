# SSH-CLI bridge (`ai-bridge`)

`rubezh-api` подключается по SSH к удалённому Ubuntu-серверу (на котором
заранее залогинены `codex`, `claude`, Google Antigravity CLI (`agy`) и `grok` CLI серверной
учёткой `aiagent`) и запускает нормализующую обёртку
`/usr/local/bin/ai-bridge <provider>`.

Никаких API-ключей провайдеров на стороне `rubezh-api` НЕТ.
Аутентификация в OpenAI / Anthropic / Google Antigravity / xAI выполнена на сервере
интерактивно один раз; токены хранит сам CLI.

## Контракт

### Вход (stdin, JSON, UTF-8)

```json
{
  "prompt": "обращение к LLM",
  "model": "опциональный hint; пустой/старый alias нормализуется bridge",
  "session_id": "опциональный идентификатор — НЕ обязателен"
}
```

`prompt` — обязателен и непустой. Бэкенд уже отработал sanitize / policy /
mask — `prompt` приходит обезличенным (псевдонимами).

### Выход (stdout, JSON, UTF-8, всегда одна строка)

Успех:

```json
{
  "ok": true,
  "provider": "codex|claude|gemini|grok|grok-build",
  "model": "...",
  "content": "...",
  "files": [
    {"name": "report.xlsx", "path": "report.xlsx",
     "mime": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
     "size": 12345, "base64": "<...>"}
  ]
}
```

`files[]` опционально (отсутствует, если CLI не создал артефактов).
Bridge собирает новые/изменённые файлы из `WORKSPACE` между snapshot до и
после CLI; лимиты в env `AI_BRIDGE_FILES_MAX_*` (default: до 10 файлов,
до 5 MB каждый и суммарно).

Ошибка (exit-code всегда 0 — иначе SSH-сессия теряет диагностику):

```json
{"ok": false, "provider": "...", "error": "<code>", "detail": "..."}
```

Коды `error`:

| код                 | смысл                                                  |
| ------------------- | ------------------------------------------------------ |
| `usage`             | неверное число аргументов                              |
| `invalid_provider`  | provider не из codex/claude/gemini/grok/grok-build     |
| `bad_payload`       | stdin пуст / не JSON / без prompt                      |
| `cli_not_installed` | бинарь CLI отсутствует в PATH                          |
| `remote_cli_failed` | CLI вернул rc != 0                                     |
| `invalid_cli_output`| CLI не вернул валидный JSON (для claude/gemini/grok)   |
| `auth_error`        | CLI установлен, но требует интерактивную авторизацию    |
| `config_error`      | bridge не смог записать локальный конфиг CLI            |
| `timeout`           | CLI не уложился в таймаут (env `AI_BRIDGE_TIMEOUT`)    |
| `internal_error`    | необработанное исключение в скрипте                    |

## Реализация: Python vs Bash

`ai-bridge.py` — основная реализация (поддерживает все 4 провайдера).
`ai-bridge.sh` — fallback для систем без Python3-обвязки; реализует
только `claude` (codex/gemini/grok возвращают `bash_fallback_unsupported`).
Если установлен Python3 (обычно — да), используйте `.py`.

## Установка

```bash
# на удалённом сервере, под root:
sudo install -m 0755 deploy/ssh-bridge/ai-bridge.py /usr/local/bin/ai-bridge
# либо .sh:
# sudo install -m 0755 deploy/ssh-bridge/ai-bridge.sh /usr/local/bin/ai-bridge

# проверка
echo '{"prompt":"Reply OK only","model":"gpt-5.3-codex"}' \
  | /usr/local/bin/ai-bridge codex
# → {"ok":true,"provider":"codex","model":"gpt-5.3-codex","content":"OK"}
```

Окружение:

- `AI_BRIDGE_WORKSPACE` — рабочая директория для `codex exec`
  (default `/srv/ai-workspaces/default`); создать заранее.
- `AI_BRIDGE_TIMEOUT` — секунды (default 150).
- `AI_BRIDGE_USE_STDIN` (default `false`) — если `true`, prompt
  передаётся в CLI через stdin вместо argv. Включает митигацию
  cmdline-exposure (см. ниже §Security). Прежде чем включать на
  продакшене, проверьте интерактивно, что конкретная версия CLI
  принимает prompt с stdin (`<cli> -p` без позиционного аргумента).
- `AI_BRIDGE_CODEX_SANDBOX` (default `workspace-write`) — sandbox-режим
  для `codex exec`. `read-only` запрещает создание файлов (codex
  ответит, что папка только для чтения). `workspace-write` разрешает
  запись в WORKSPACE — обязательно для возврата артефактов через
  `files[]`. `danger-full-access` снимает все ограничения (не нужно).
- `AI_BRIDGE_FILES_MAX_COUNT` (default 10), `AI_BRIDGE_FILES_MAX_TOTAL_BYTES`
  (default 5 MB), `AI_BRIDGE_FILES_MAX_PER_FILE_BYTES` (default 5 MB) —
  лимиты на возвращаемые файлы. Превышение → файлы отбрасываются (текст
  ответа всегда возвращается).
- `AI_BRIDGE_DEFAULT_CODEX_MODEL` / `AI_BRIDGE_DEFAULT_CLAUDE_MODEL` /
  `AI_BRIDGE_DEFAULT_GEMINI_MODEL` / `AI_BRIDGE_DEFAULT_GROK_MODEL` —
  переопределить встроенный fallback-model. Используется ТОЛЬКО когда
  RubezAI прислал пустой `model` или старый alias. Основное место для
  смены дефолта — `model_providers.default_model` в БД RubezAI
  (миграция 000019; меняется через `PATCH /api/models/:id`). Меняйте
  env только если у вас нет доступа к БД RubezAI или нужен глобальный
  override для всех инсталляций.
- `AI_BRIDGE_GEMINI_BACKEND` — backend для endpoint `gemini`.
  По умолчанию `antigravity`, то есть bridge вызывает `agy`.
  Для аварийного отката на старый binary: `legacy`.
- `AI_BRIDGE_ANTIGRAVITY_FLAGS` — дополнительные флаги для `agy -p`
  (разбираются через `shlex.split`, shell не используется).

## Gemini endpoint = Antigravity CLI

После перехода Google на Antigravity endpoint `gemini` внутри bridge
больше не вызывает старый `gemini` binary по умолчанию. Он вызывает:

```bash
agy -p "<prompt>" --print-timeout <AI_BRIDGE_TIMEOUT>s
```

Модель задаётся через `~/.gemini/antigravity-cli/settings.json`, поле
`model`. Bridge обновляет его из входного `model`; старые alias
`gemini-2.5-pro` и `gemini-3.5-flash` нормализуются в
`Gemini 3.5 Flash (High)`.

Установка Antigravity CLI:

```bash
curl -fsSL https://antigravity.google/cli/install.sh | bash
```

Авторизация выполняется один раз под пользователем `aiagent`; при
отсутствии сессии bridge вернёт `{"ok":false,"error":"auth_error"}`.

## Smoke-test с локальной машины

```powershell
'{"prompt":"Reply OK only","model":"sonnet"}' `
  | ssh -T -i C:\Users\Mih10\.ssh\rubezai_ruvds_aiagent_ed25519 `
        aiagent@193.124.93.157 claude
# → {"ok":true,"provider":"claude","model":"sonnet","content":"OK"}

Для Grok основной endpoint/provider — `grok-build`; `grok` оставлен как
alias для совместимости. Для Codex рабочий default — `gpt-5.3-codex`;
старый alias `gpt-5-codex` bridge нормализует в `gpt-5.3-codex`.
```

## Security

- `shell=False` во всех вызовах CLI: argv-массив, никакой строковой
  интерполяции.
- Аргумент `<provider>` валидируется белым списком и в bridge, и в
  `rubezh-api` (`internal/llm/ssh_cli.go::validSSHProviderArg`,
  `internal/api/models.go::validSSHCLIEndpoint`).
- exit-code всегда 0 — даже при ошибке. SSH-сессия завершается чисто,
  rubezh-api различает успех/ошибку по `ok` в JSON.
- Bridge НЕ пишет prompt/content в системные логи.
- На стороне rubezh-api логируются только: `provider`, `remote`,
  `error-kind`. Stdout с raw-ответом не попадает ни в логи, ни в текст
  ошибок. Поле `detail` из bridge-ответа Go-структурой `sshBridgeResponse`
  игнорируется по дизайну — наружу идёт только структурный код `error`.
- **cmdline-exposure (известное ограничение)**: в режиме по умолчанию
  (`AI_BRIDGE_USE_STDIN=false`) prompt передаётся CLI argv-массивом.
  Это означает, что masked-промпт виден в `/proc/<pid>/cmdline` на
  удалённом сервере любому процессу, который читает `/proc`. На
  одно-пользовательском сервере (доступ только у `aiagent` + root) это
  приемлемо. Для multi-tenant установок включайте `AI_BRIDGE_USE_STDIN=true`
  после подтверждения, что CLI принимает stdin.

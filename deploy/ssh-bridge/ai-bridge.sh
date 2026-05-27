#!/usr/bin/env bash
# ai-bridge — тонкая bash-обёртка к Python-реализации.
#
# Поставляется как fallback на случай минимальной Ubuntu без Python3-обвязки.
# По умолчанию используйте ai-bridge.py (см. README.md).
#
# Контракт совпадает с ai-bridge.py: argv-аргумент — provider
# (codex|claude|gemini|grok), stdin — JSON, stdout — JSON.
#
# Этот скрипт делегирует Python-реализации, если та доступна, иначе
# реализует минимальный путь только для claude (как наиболее
# отлаженный). Расширение остальных провайдеров — через Python-версию.

set -u

# Жёстко фиксированный путь — никакой shell-интерполяции в командах.
PY_IMPL="/usr/local/lib/rubezh/ai-bridge.py"

if [[ -x "${PY_IMPL}" ]]; then
    exec "${PY_IMPL}" "$@"
fi

provider="${1:-}"
case "${provider}" in
    codex|claude|gemini|grok) ;;
    *)
        printf '{"ok":false,"provider":"","error":"invalid_provider","detail":"expected codex|claude|gemini|grok"}\n'
        exit 0
        ;;
esac

# Минимальный путь: только claude через `claude -p ... --output-format json`.
# Остальные провайдеры доступны лишь через Python-реализацию.
if [[ "${provider}" != "claude" ]]; then
    printf '{"ok":false,"provider":"%s","error":"bash_fallback_unsupported","detail":"install ai-bridge.py"}\n' "${provider}"
    exit 0
fi

# Жадно читаем JSON со stdin (одной строкой).
payload="$(cat -)"
if [[ -z "${payload// }" ]]; then
    printf '{"ok":false,"provider":"claude","error":"bad_payload","detail":"empty_stdin"}\n'
    exit 0
fi

# Минимальная JSON-обвязка через python -c (всегда доступен в Ubuntu/server).
# Если python3 нет — отдаём ошибку, не пытаясь парсить JSON руками.
if ! command -v python3 >/dev/null 2>&1; then
    printf '{"ok":false,"provider":"claude","error":"python_missing","detail":"python3 not in PATH"}\n'
    exit 0
fi

prompt="$(printf '%s' "${payload}" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("prompt",""))')"
model="$(printf '%s' "${payload}" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("model",""))')"

if [[ -z "${prompt}" ]]; then
    printf '{"ok":false,"provider":"claude","error":"bad_payload","detail":"missing_prompt"}\n'
    exit 0
fi

# argv-массив: никакого shell-quoting prompt'а.
raw="$(claude -p "${prompt}" --output-format json 2>/dev/null)"
rc=$?
if [[ ${rc} -ne 0 ]]; then
    printf '{"ok":false,"provider":"claude","error":"remote_cli_failed","detail":"rc=%d"}\n' "${rc}"
    exit 0
fi

# Извлекаем content через python3 (надёжнее, чем jq, не везде установлен).
content="$(printf '%s' "${raw}" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception as e:
    sys.stderr.write(f"json: {e}\n"); sys.exit(2)
if isinstance(d, dict):
    print(d.get("result") or d.get("content") or d.get("text") or "")
')"
py_rc=$?
if [[ ${py_rc} -ne 0 ]]; then
    printf '{"ok":false,"provider":"claude","error":"invalid_cli_output","detail":"non-JSON from claude"}\n'
    exit 0
fi

python3 -c '
import json, sys
print(json.dumps({
    "ok": True,
    "provider": "claude",
    "model": sys.argv[1],
    "content": sys.argv[2],
}, ensure_ascii=False))
' "${model}" "${content}"

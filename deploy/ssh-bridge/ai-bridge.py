#!/usr/bin/env python3
"""ai-bridge — нормализованный CLI-мост к Codex / Claude / Gemini / Grok.

rubezh-api подключается к серверу по SSH с pubkey-аутентификацией и
запускает эту команду:

    /usr/local/bin/ai-bridge <provider>

Аргумент <provider> — строго из набора
{codex, claude, gemini, grok, grok-build}.
stdin: JSON {"prompt": "...", "model": "...", "session_id": "..." (опц.)}.
stdout: JSON {"ok": bool, "provider": "...", "model": "...", "content": "..."}.

Контракт ошибок (stdout, exit-code 0):

    {"ok": false, "provider": "...", "error": "<code>", "detail": "..."}

Никакого shell=True / eval. Все CLI вызываются через subprocess.run с
argv-массивом. Локальные пути CLI-инструментов читаются из env, чтобы
скрипт работал без правок при разных установках.
"""

from __future__ import annotations

import base64
import json
import mimetypes
import os
import shlex
import shutil
import subprocess
import sys
import tempfile
import time
from typing import Any

VALID_PROVIDERS = ("codex", "claude", "gemini", "grok", "grok-build")

# DEFAULT_MODELS — встроенный fallback, который используется ТОЛЬКО когда
# RubezAI прислал пустой model или старый alias (см. normalized_model).
# Основной канал управления дефолтами — model_providers.default_model
# в БД RubezAI (миграция 000019); fallback здесь — последний рубеж
# устойчивости (если default_model в БД пуст или RubezAI ещё не обновили).
#
# Каждое значение можно переопределить env:
#   AI_BRIDGE_DEFAULT_CODEX_MODEL
#   AI_BRIDGE_DEFAULT_CLAUDE_MODEL
#   AI_BRIDGE_DEFAULT_GEMINI_MODEL
#   AI_BRIDGE_DEFAULT_GROK_MODEL
DEFAULT_MODELS = {
    "codex":      os.environ.get("AI_BRIDGE_DEFAULT_CODEX_MODEL",  "gpt-5.3-codex"),
    "claude":     os.environ.get("AI_BRIDGE_DEFAULT_CLAUDE_MODEL", "claude-opus-4-7"),
    "gemini":     os.environ.get("AI_BRIDGE_DEFAULT_GEMINI_MODEL", "Gemini 3.5 Flash (High)"),
    "grok":       os.environ.get("AI_BRIDGE_DEFAULT_GROK_MODEL",   "grok-build"),
    "grok-build": os.environ.get("AI_BRIDGE_DEFAULT_GROK_MODEL",   "grok-build"),
}

WORKSPACE = os.environ.get(
    "AI_BRIDGE_WORKSPACE", "/srv/ai-workspaces/default"
)
DEFAULT_TIMEOUT_SECONDS = int(os.environ.get("AI_BRIDGE_TIMEOUT", "150"))

EXTRA_BIN_DIRS = [
    os.environ.get("AI_BRIDGE_BIN_DIR", ""),
    os.path.expanduser("~/.local/bin"),
    "/home/aiagent/.local/bin",
    "/usr/local/bin",
    "/usr/bin",
    "/bin",
]
os.environ["PATH"] = os.pathsep.join(
    [p for p in EXTRA_BIN_DIRS if p] + [os.environ.get("PATH", "")]
)

# MAJOR-M3 митигация cmdline-exposure: если AI_BRIDGE_USE_STDIN=true,
# prompt передаётся в CLI через stdin (где CLI это поддерживает), а не
# argv. Default — false: сохраняет user-verified рабочий контракт
# (`claude -p <prompt>`). Включать после проверки конкретного CLI.
USE_STDIN = os.environ.get("AI_BRIDGE_USE_STDIN", "").lower() == "true"

# Codex sandbox: workspace-write позволяет codex создавать файлы в WORKSPACE.
# Без этого codex отвечает «папка read-only» и не создаёт артефакты.
# Можно переопределить env: read-only | workspace-write | danger-full-access.
CODEX_SANDBOX = os.environ.get("AI_BRIDGE_CODEX_SANDBOX", "workspace-write")

# Лимиты на сбор файлов из WORKSPACE после CLI:
# - максимум файлов в одном ответе;
# - максимум суммарного размера raw bytes (base64 раздувает на ~33%).
# Цель — не перегружать SSE-канал и не разрывать соединение фронта.
FILES_MAX_COUNT = int(os.environ.get("AI_BRIDGE_FILES_MAX_COUNT", "10"))
FILES_MAX_TOTAL_BYTES = int(
    os.environ.get("AI_BRIDGE_FILES_MAX_TOTAL_BYTES", str(5 * 1024 * 1024))
)
FILES_MAX_PER_FILE_BYTES = int(
    os.environ.get("AI_BRIDGE_FILES_MAX_PER_FILE_BYTES", str(5 * 1024 * 1024))
)

# Имена файлов и подкаталогов, которые НЕ возвращаем как артефакты
# (мусор от CLI, конфиги, скрытые директории VCS и т. п.).
FILES_SKIP_PREFIXES = (".git/", ".codex/", ".claude/", ".gemini/", ".antigravity/",
                       "node_modules/", "__pycache__/", ".venv/")


def snapshot_workspace() -> dict[str, float]:
    """Снимает (relative_path → mtime) для всех файлов в WORKSPACE.

    Используется до и после вызова CLI: diff даёт список созданных/изменённых
    файлов. mtime лучше size: file может быть переписан тем же размером.
    """
    if not os.path.isdir(WORKSPACE):
        return {}
    snap: dict[str, float] = {}
    for root, dirs, files in os.walk(WORKSPACE):
        # Не углубляемся в скрытые/служебные директории.
        dirs[:] = [d for d in dirs if not d.startswith(".")
                   and d not in ("node_modules", "__pycache__", ".venv")]
        for name in files:
            full = os.path.join(root, name)
            rel = os.path.relpath(full, WORKSPACE).replace(os.sep, "/")
            try:
                snap[rel] = os.path.getmtime(full)
            except OSError:
                continue
    return snap


def collect_new_files(
    before: dict[str, float], after: dict[str, float],
) -> list[dict[str, Any]]:
    """Возвращает новые/изменённые файлы из WORKSPACE как список объектов
    {name, mime, size, base64}. Лимиты — FILES_MAX_*.

    Read-only сторона: bridge не удаляет файлы, только читает. Codex/Claude
    могут поверх перезаписывать; bridge возвращает финальное состояние.
    """
    files: list[dict[str, Any]] = []
    total = 0
    for rel, mtime in sorted(after.items()):
        if rel.startswith(FILES_SKIP_PREFIXES):
            continue
        before_mtime = before.get(rel)
        if before_mtime is not None and before_mtime >= mtime:
            continue
        full = os.path.join(WORKSPACE, rel)
        try:
            size = os.path.getsize(full)
        except OSError:
            continue
        if size <= 0 or size > FILES_MAX_PER_FILE_BYTES:
            continue
        if total + size > FILES_MAX_TOTAL_BYTES:
            break
        try:
            with open(full, "rb") as fh:
                raw = fh.read(FILES_MAX_PER_FILE_BYTES + 1)
        except OSError:
            continue
        if len(raw) > FILES_MAX_PER_FILE_BYTES:
            continue
        mime, _ = mimetypes.guess_type(rel)
        files.append({
            "name": os.path.basename(rel),
            "path": rel,
            "mime": mime or "application/octet-stream",
            "size": size,
            "base64": base64.b64encode(raw).decode("ascii"),
        })
        total += size
        if len(files) >= FILES_MAX_COUNT:
            break
    return files


def emit_success(provider: str, model: str, content: str,
                 files: list[dict[str, Any]] | None = None) -> int:
    """Стандартный успешный JSON. files[] опционально."""
    out: dict[str, Any] = {
        "ok": True, "provider": provider, "model": model, "content": content,
    }
    if files:
        out["files"] = files
    json.dump(out, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")
    return 0


def emit_error(code: str, *, provider: str = "", detail: str = "") -> int:
    """Печатает структурную ошибку в stdout и завершает с exit 0.

    rubezh-api ожидает JSON в stdout даже при ошибке (см.
    rubezh-api/internal/llm/ssh_cli.go). exit-code != 0 ломает SSH-сессию
    и теряет диагностику.
    """
    json.dump(
        {"ok": False, "provider": provider, "error": code, "detail": detail},
        sys.stdout,
        ensure_ascii=False,
    )
    sys.stdout.write("\n")
    return 0


def read_payload() -> dict[str, Any]:
    raw = sys.stdin.read()
    if not raw.strip():
        raise ValueError("empty_stdin")
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise ValueError(f"invalid_json: {exc}") from exc
    if not isinstance(payload, dict):
        raise ValueError("payload_not_object")
    if not isinstance(payload.get("prompt"), str) or not payload["prompt"]:
        raise ValueError("missing_prompt")
    return payload


def run_cli(argv: list[str], stdin_text: str | None = None) -> tuple[int, str, str]:
    """Запускает CLI argv-массивом. Возвращает (rc, stdout, stderr).

    shell=False, никакой интерполяции. Таймаут управляем через env.
    """
    proc = subprocess.run(
        argv,
        input=stdin_text,
        capture_output=True,
        text=True,
        cwd=WORKSPACE if os.path.isdir(WORKSPACE) else None,
        timeout=DEFAULT_TIMEOUT_SECONDS,
        check=False,
    )
    return proc.returncode, proc.stdout, proc.stderr


def normalized_model(provider: str, payload: dict[str, Any]) -> str:
    model = payload.get("model")
    if not isinstance(model, str):
        model = ""
    if provider == "codex" and model in ("", "codex-cli", "gpt-5-codex"):
        return DEFAULT_MODELS[provider]
    if provider == "claude" and model in ("", "claude-code-cli"):
        return DEFAULT_MODELS[provider]
    if provider == "gemini" and model in (
        "", "gemini-cli", "gemini-2.5-pro", "gemini-3.5-flash",
    ):
        return DEFAULT_MODELS[provider]
    if provider in ("grok", "grok-build") and model in ("", "grok", "grok-cli"):
        return DEFAULT_MODELS[provider]
    return model or DEFAULT_MODELS[provider]


def which_or_error(binary: str, provider: str) -> str | None:
    path = shutil.which(binary)
    if not path:
        emit_error(
            "cli_not_installed",
            provider=provider,
            detail=f"binary {binary} not in PATH",
        )
        return None
    return path


def which_any_or_error(binaries: list[str], provider: str) -> str | None:
    for binary in binaries:
        path = shutil.which(binary)
        if path:
            return path
    emit_error(
        "cli_not_installed",
        provider=provider,
        detail=f"none of {','.join(binaries)} in PATH",
    )
    return None


def configure_antigravity_model(model: str) -> None:
    if not model:
        return
    settings_dir = os.path.expanduser("~/.gemini/antigravity-cli")
    settings_path = os.path.join(settings_dir, "settings.json")
    os.makedirs(settings_dir, exist_ok=True)
    try:
        with open(settings_path, "r", encoding="utf-8") as fh:
            settings = json.load(fh)
        if not isinstance(settings, dict):
            settings = {}
    except (OSError, json.JSONDecodeError):
        settings = {}
    settings["model"] = model
    settings.setdefault("enableTerminalSandbox", True)
    tmp_path = settings_path + ".tmp"
    with open(tmp_path, "w", encoding="utf-8") as fh:
        json.dump(settings, fh, ensure_ascii=False, indent=2)
        fh.write("\n")
    os.replace(tmp_path, settings_path)


def looks_like_antigravity_auth_error(text: str) -> bool:
    lowered = text.lower()
    return any(
        marker in lowered
        for marker in (
            "authentication required",
            "authentication timed out",
            "authorization code",
            "please visit the url to log in",
        )
    )


def looks_like_antigravity_eligibility_error(text: str) -> bool:
    lowered = text.lower()
    return any(
        marker in lowered
        for marker in (
            "not eligible for antigravity",
            "not currently available in your location",
            "user location is not supported",
            "failed_precondition (code 400)",
        )
    )


def recent_antigravity_log_text(started_at: float) -> str:
    """Return a small tail from Antigravity logs written during this run."""
    log_dir = os.path.expanduser("~/.gemini/antigravity-cli/log")
    try:
        names = [
            os.path.join(log_dir, name)
            for name in os.listdir(log_dir)
            if name.startswith("cli-") and name.endswith(".log")
        ]
    except OSError:
        return ""

    chunks: list[str] = []
    for path in sorted(names, key=lambda p: os.path.getmtime(p), reverse=True):
        try:
            if os.path.getmtime(path) < started_at - 5:
                continue
            with open(path, "rb") as fh:
                fh.seek(0, os.SEEK_END)
                size = fh.tell()
                fh.seek(max(0, size - 24000))
                chunks.append(
                    fh.read().decode("utf-8", errors="replace")
                )
        except OSError:
            continue
        if len(chunks) >= 2:
            break
    return "\n".join(chunks)


def handle_codex(payload: dict[str, Any]) -> int:
    bin_path = which_or_error("codex", "codex")
    if not bin_path:
        return 0
    model = normalized_model("codex", payload)
    tmp = tempfile.NamedTemporaryFile(prefix="codex-last-", suffix=".txt", delete=False)
    tmp_path = tmp.name
    tmp.close()
    # Snapshot WORKSPACE до запуска codex, чтобы потом отдать новые/изменённые
    # файлы как attachments (xlsx/png/pdf и т. п., которые модель сама создаёт).
    before = snapshot_workspace()
    sandbox_flag = ["--sandbox", CODEX_SANDBOX] if CODEX_SANDBOX else []
    if USE_STDIN:
        argv = [
            bin_path, "exec", "--skip-git-repo-check", "-C", WORKSPACE,
            *sandbox_flag,
            "--output-last-message", tmp_path, "-m", model, "-",
        ]
        rc, out, err = run_cli(argv, stdin_text=payload["prompt"])
    else:
        argv = [
            bin_path, "exec", "--skip-git-repo-check", "-C", WORKSPACE,
            *sandbox_flag,
            "--output-last-message", tmp_path, "-m", model, payload["prompt"],
        ]
        rc, out, err = run_cli(argv)
    if rc != 0:
        try:
            os.unlink(tmp_path)
        except OSError:
            pass
        return emit_error(
            "remote_cli_failed",
            provider="codex",
            detail=f"rc={rc}",
        )
    try:
        with open(tmp_path, "r", encoding="utf-8") as fh:
            content = fh.read().strip()
    except OSError:
        content = out.strip()
    finally:
        try:
            os.unlink(tmp_path)
        except OSError:
            pass
    if not content:
        content = out.strip()
    files = collect_new_files(before, snapshot_workspace())
    return emit_success("codex", model, content, files)


def handle_claude(payload: dict[str, Any]) -> int:
    bin_path = which_or_error("claude", "claude")
    if not bin_path:
        return 0
    model = normalized_model("claude", payload)
    before = snapshot_workspace()
    if USE_STDIN:
        argv = [bin_path, "-p", "--output-format", "json", "--model", model]
        rc, out, err = run_cli(argv, stdin_text=payload["prompt"])
    else:
        argv = [
            bin_path, "-p", payload["prompt"], "--output-format", "json",
            "--model", model,
        ]
        rc, out, err = run_cli(argv)
    if rc != 0:
        return emit_error(
            "remote_cli_failed",
            provider="claude",
            detail=f"rc={rc}",
        )
    try:
        parsed = json.loads(out)
    except json.JSONDecodeError:
        return emit_error(
            "invalid_cli_output",
            provider="claude",
            detail="claude CLI returned non-JSON",
        )
    content = ""
    if isinstance(parsed, dict):
        content = (
            parsed.get("result")
            or parsed.get("content")
            or parsed.get("text")
            or ""
        )
        if not isinstance(content, str):
            content = json.dumps(content, ensure_ascii=False)
    files = collect_new_files(before, snapshot_workspace())
    return emit_success("claude", model, content, files)


def handle_gemini_legacy(payload: dict[str, Any]) -> int:
    bin_path = which_or_error("gemini", "gemini")
    if not bin_path:
        return 0
    model = normalized_model("gemini", payload)
    before = snapshot_workspace()
    if USE_STDIN:
        argv = [
            bin_path, "--skip-trust", "-p", "--output-format", "json",
            "--model", model,
        ]
        rc, out, err = run_cli(argv, stdin_text=payload["prompt"])
    else:
        argv = [
            bin_path, "--skip-trust", "-p", payload["prompt"],
            "--output-format", "json", "--model", model,
        ]
        rc, out, err = run_cli(argv)
    if rc != 0:
        return emit_error(
            "remote_cli_failed", provider="gemini", detail=f"rc={rc}"
        )
    try:
        parsed = json.loads(out)
    except json.JSONDecodeError:
        return emit_error(
            "invalid_cli_output",
            provider="gemini",
            detail="gemini CLI returned non-JSON",
        )
    content = ""
    if isinstance(parsed, dict):
        content = (
            parsed.get("response")
            or parsed.get("text")
            or parsed.get("content")
            or ""
        )
        if not isinstance(content, str):
            content = json.dumps(content, ensure_ascii=False)
    files = collect_new_files(before, snapshot_workspace())
    return emit_success("gemini", model, content, files)


def handle_gemini(payload: dict[str, Any]) -> int:
    backend = os.environ.get("AI_BRIDGE_GEMINI_BACKEND", "antigravity").lower()
    if backend in ("legacy", "gemini", "gemini-cli"):
        return handle_gemini_legacy(payload)

    bin_path = which_any_or_error(["agy", "antigravity"], "gemini")
    if not bin_path:
        return 0
    model = normalized_model("gemini", payload)
    try:
        configure_antigravity_model(model)
    except OSError:
        return emit_error(
            "config_error",
            provider="gemini",
            detail="failed to update Antigravity settings",
        )

    extra_flags = shlex.split(os.environ.get("AI_BRIDGE_ANTIGRAVITY_FLAGS", ""))
    timeout_value = f"{DEFAULT_TIMEOUT_SECONDS}s"
    before = snapshot_workspace()
    started_at = time.time()
    if USE_STDIN:
        argv = [bin_path, "-p", "--print-timeout", timeout_value, *extra_flags]
        rc, out, err = run_cli(argv, stdin_text=payload["prompt"])
    else:
        argv = [
            bin_path, "-p", payload["prompt"],
            "--print-timeout", timeout_value,
            *extra_flags,
        ]
        rc, out, err = run_cli(argv)

    combined = (
        (out or "") + "\n" + (err or "") + "\n"
        + recent_antigravity_log_text(started_at)
    )
    if looks_like_antigravity_auth_error(combined):
        return emit_error(
            "auth_error",
            provider="gemini",
            detail="Antigravity CLI authentication required",
        )
    if looks_like_antigravity_eligibility_error(combined):
        return emit_error(
            "eligibility_error",
            provider="gemini",
            detail=(
                "Antigravity account/location is not eligible: "
                "user location is not supported for API use"
            ),
        )
    if rc != 0:
        return emit_error(
            "remote_cli_failed",
            provider="gemini",
            detail=f"rc={rc}",
        )
    content = out.strip()
    if not content:
        content = err.strip()
    if not content:
        return emit_error(
            "empty_response",
            provider="gemini",
            detail="Antigravity CLI returned no content",
        )
    files = collect_new_files(before, snapshot_workspace())
    return emit_success("gemini", model, content, files)


def handle_grok(payload: dict[str, Any], provider: str = "grok") -> int:
    bin_path = which_or_error("grok", provider)
    if not bin_path:
        return 0
    model = normalized_model(provider, payload)
    if USE_STDIN:
        argv = [
            bin_path, "-p", "--output-format", "json",
            "--model", model, "--no-alt-screen",
        ]
        rc, out, err = run_cli(argv, stdin_text=payload["prompt"])
    else:
        argv = [
            bin_path, "-p", payload["prompt"], "--output-format", "json",
            "--model", model, "--no-alt-screen",
        ]
        rc, out, err = run_cli(argv)
    if rc != 0:
        return emit_error(
            "remote_cli_failed", provider=provider, detail=f"rc={rc}"
        )
    try:
        parsed = json.loads(out)
    except json.JSONDecodeError:
        return emit_error(
            "invalid_cli_output",
            provider=provider,
            detail="grok CLI returned non-JSON",
        )
    content = ""
    if isinstance(parsed, dict):
        content = (
            parsed.get("text")
            or parsed.get("content")
            or parsed.get("result")
            or ""
        )
        if not isinstance(content, str):
            content = json.dumps(content, ensure_ascii=False)
    json.dump(
        {
            "ok": True,
            "provider": provider,
            "model": model,
            "content": content,
        },
        sys.stdout,
        ensure_ascii=False,
    )
    sys.stdout.write("\n")
    return 0


HANDLERS = {
    "codex": handle_codex,
    "claude": handle_claude,
    "gemini": handle_gemini,
    "grok": handle_grok,
    "grok-build": lambda payload: handle_grok(payload, "grok-build"),
}


def resolve_provider(argv: list[str]) -> str | None:
    if len(argv) == 2 and argv[1] != "--forced":
        return argv[1]
    if len(argv) == 2 and argv[1] == "--forced":
        original = os.environ.get("SSH_ORIGINAL_COMMAND", "").strip()
        if not original:
            return None
        return original.split()[-1]
    return None


def main(argv: list[str]) -> int:
    provider = resolve_provider(argv)
    if not provider:
        return emit_error(
            "usage",
            detail="ai-bridge <codex|claude|gemini|grok|grok-build>",
        )
    if provider not in VALID_PROVIDERS:
        return emit_error(
            "invalid_provider",
            provider=provider,
            detail=f"expected one of {','.join(VALID_PROVIDERS)}",
        )
    try:
        payload = read_payload()
    except ValueError as exc:
        return emit_error("bad_payload", provider=provider, detail=str(exc))
    try:
        return HANDLERS[provider](payload)
    except subprocess.TimeoutExpired:
        return emit_error("timeout", provider=provider)
    except Exception as exc:  # noqa: BLE001 — последний рубеж: всегда отдаём JSON.
        return emit_error("internal_error", provider=provider, detail=str(exc))


if __name__ == "__main__":
    sys.exit(main(sys.argv))

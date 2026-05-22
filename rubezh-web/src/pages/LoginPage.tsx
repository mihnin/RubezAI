import { useState, useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { ShieldCheck, ArrowRight, Info, LogIn } from "lucide-react";
import { useAuth } from "../auth/context";

const ROLES = [
  { value: "user", label: "Пользователь", hint: "обычные запросы к LLM" },
  {
    value: "security_officer",
    label: "Сотрудник ИБ",
    hint: "инциденты, эскалации",
  },
  {
    value: "compliance_officer",
    label: "Комплаенс",
    hint: "политики, журнал аудита",
  },
  { value: "admin", label: "Администратор", hint: "управление моделями" },
  { value: "auditor", label: "Аудитор", hint: "только чтение audit" },
  { value: "developer", label: "Разработчик", hint: "разработка, доступ к API" },
];

/** Login (Итерация 13/каркас 12). docs/design/ui/login.md. */
export default function LoginPage() {
  const [role, setRole] = useState("user");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const { login, loginWithToken } = useAuth();
  const nav = useNavigate();

  // Возврат с OIDC-callback: токен/ошибка приходят во фрагменте URL (K.1).
  useEffect(() => {
    const hash = window.location.hash.replace(/^#/, "");
    if (!hash) return;
    const p = new URLSearchParams(hash);
    const err = p.get("error");
    const tok = p.get("token");
    window.history.replaceState(null, "", window.location.pathname);
    if (err) {
      setError(err);
      return;
    }
    if (tok) {
      loginWithToken(tok, p.get("role") || "user");
      nav("/", { replace: true });
    }
  }, [loginWithToken, nav]);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await login(role);
      nav("/", { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка входа");
    } finally {
      setBusy(false);
    }
  }

  const activeRole = ROLES.find((r) => r.value === role)!;

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-950 via-slate-950 to-slate-900 text-slate-50 p-6">
      <div className="w-[420px]">
        <div className="flex items-center justify-center gap-3 mb-8">
          <div className="w-12 h-12 rounded-xl bg-gradient-to-br from-cyan-400 to-cyan-600 flex items-center justify-center shadow-xl shadow-cyan-500/30">
            <ShieldCheck
              className="w-7 h-7 text-slate-950"
              strokeWidth={2.5}
            />
          </div>
          <div>
            <div className="text-2xl font-semibold tracking-tight">
              Рубеж&nbsp;ИИ
            </div>
            <div className="text-xs text-slate-400 uppercase tracking-wider">
              On-prem AI&nbsp;Gateway
            </div>
          </div>
        </div>

        <form
          onSubmit={handleSubmit}
          className="bg-slate-900/70 backdrop-blur rounded-xl p-8 border border-slate-800 shadow-2xl shadow-cyan-950/40"
        >
          <h1 className="text-sm font-medium text-slate-300 mb-1">
            Вход в систему
          </h1>
          <p className="text-xs text-slate-500 mb-6">
            Dev-режим. После MVP — единый OIDC SSO.
          </p>

          <label
            className="block text-xs uppercase tracking-wider text-slate-400 mb-2"
            htmlFor="role"
          >
            Роль
          </label>
          <select
            id="role"
            value={role}
            onChange={(e) => setRole(e.target.value)}
            disabled={busy}
            className="w-full h-11 px-3 rounded-lg bg-slate-800/80 border border-slate-700 text-slate-50 mb-1 focus:outline-none focus:border-cyan-500 focus:ring-2 focus:ring-cyan-500/20 transition"
          >
            {ROLES.map((r) => (
              <option key={r.value} value={r.value}>
                {r.label}
              </option>
            ))}
          </select>
          <p className="text-xs text-slate-500 mb-6">{activeRole.hint}</p>

          {error && (
            <div
              role="alert"
              aria-live="assertive"
              className="mb-4 p-3 rounded-lg bg-red-900/30 border border-red-800/60 text-red-200 text-sm"
            >
              {error}
            </div>
          )}

          <button
            type="submit"
            disabled={busy}
            className="w-full h-11 rounded-lg bg-cyan-500 hover:bg-cyan-400 text-slate-950 font-medium disabled:opacity-50 disabled:cursor-not-allowed transition shadow-lg shadow-cyan-500/20 flex items-center justify-center gap-1.5"
          >
            {busy ? (
              "Вход…"
            ) : (
              <>
                Войти <ArrowRight className="w-4 h-4" strokeWidth={2.5} />
              </>
            )}
          </button>

          <div className="mt-5 pt-5 border-t border-slate-800">
            <a
              href="/api/auth/oidc/login"
              className="w-full h-11 rounded-lg border border-slate-700 hover:border-cyan-500 text-slate-200 font-medium transition flex items-center justify-center gap-2"
            >
              <LogIn className="w-4 h-4" strokeWidth={2} />
              Войти через корпоративную учётную запись (SSO)
            </a>
            <p className="text-[11px] text-slate-500 mt-2 text-center">
              OIDC-вход по корп. почте. Если SSO не настроен — кнопка вернёт
              ошибку; используйте выбор роли выше (dev).
            </p>
          </div>

          <div className="mt-5 pt-5 border-t border-slate-800 text-xs text-slate-500 flex items-start gap-1.5">
            <Info className="w-3.5 h-3.5 mt-0.5 shrink-0" strokeWidth={2} />
            <span>
              Dev-вход (выбор роли) — токен в localStorage. SSO заменит его после
              настройки OIDC.
            </span>
          </div>
        </form>
      </div>
    </div>
  );
}

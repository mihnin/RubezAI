import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../auth/context";

const ROLES = [
  { value: "user", label: "Пользователь" },
  { value: "security_officer", label: "Сотрудник ИБ" },
  { value: "compliance_officer", label: "Комплаенс" },
  { value: "admin", label: "Администратор" },
  { value: "auditor", label: "Аудитор" },
  { value: "developer", label: "Разработчик" },
];

/** Login (Итерация 13/каркас 12). docs/design/ui/login.md. */
export default function LoginPage() {
  const [role, setRole] = useState("user");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const { login } = useAuth();
  const nav = useNavigate();

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

  return (
    <div className="min-h-screen flex items-center justify-center bg-slate-950 text-slate-50">
      <form
        onSubmit={handleSubmit}
        className="w-[360px] bg-slate-900 rounded-lg p-8 border border-slate-700"
      >
        <div className="text-center mb-6">
          <div className="text-2xl font-semibold">⛨ Рубеж ИИ</div>
          <div className="text-sm text-slate-400 mt-1">
            On-prem AI Gateway
          </div>
        </div>

        <label className="block text-sm text-slate-300 mb-1" htmlFor="role">
          Войти под ролью
        </label>
        <select
          id="role"
          value={role}
          onChange={(e) => setRole(e.target.value)}
          disabled={busy}
          className="w-full h-10 px-3 rounded-md bg-slate-800 border border-slate-700 text-slate-50 mb-4"
        >
          {ROLES.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </select>

        {error && (
          <div
            role="alert"
            aria-live="assertive"
            className="mb-4 p-3 rounded-md bg-red-900/30 border border-red-700 text-red-200 text-sm"
          >
            {error}
          </div>
        )}

        <button
          type="submit"
          disabled={busy}
          className="w-full h-10 rounded-md bg-cyan-500 hover:bg-cyan-400 text-slate-950 font-medium disabled:opacity-50"
        >
          {busy ? "Вход…" : "Войти →"}
        </button>

        <div className="text-xs text-slate-500 mt-6 text-center">
          Dev-режим • замена на OIDC после MVP
          <br />
          <span title="См. docs/design/identity.md">
            ⓘ Токен хранится в localStorage
          </span>
        </div>
      </form>
    </div>
  );
}

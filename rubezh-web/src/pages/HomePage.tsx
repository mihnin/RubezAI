import { useAuth } from "../auth/context";

/** HomePage — заглушка для каркаса Итерации 12.
 * Реальные экраны (chat/documents/policies/models/audit-log/incidents)
 * — Итерации 13–15. */
export default function HomePage() {
  const { user, logout } = useAuth();
  return (
    <div className="min-h-screen bg-slate-950 text-slate-50">
      <header className="border-b border-slate-700 px-6 py-4 flex justify-between items-center">
        <div className="font-semibold">⛨ Рубеж ИИ</div>
        <div className="flex items-center gap-4">
          <span className="text-sm text-slate-400">
            {user?.role} · {user?.user_id?.slice(0, 8)}
          </span>
          <button
            onClick={logout}
            className="text-sm text-slate-400 hover:text-slate-200"
          >
            Выйти
          </button>
        </div>
      </header>
      <main className="p-6">
        <h1 className="text-xl font-semibold mb-4">
          Каркас Итерации 12 — готов
        </h1>
        <p className="text-slate-300 mb-2">
          Реальные экраны добавляются в Итерациях 13–15:
        </p>
        <ul className="list-disc list-inside text-slate-400 space-y-1">
          <li>Chat (с SSE-стримом)</li>
          <li>Documents (загрузка PDF/DOCX)</li>
          <li>Policies / Models</li>
          <li>Audit Log / Incidents</li>
        </ul>
      </main>
    </div>
  );
}

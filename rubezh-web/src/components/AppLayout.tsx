import { NavLink, Outlet } from "react-router-dom";
import { useAuth } from "../auth/context";

const NAV = [
  { to: "/chat", label: "Чат" },
  { to: "/documents", label: "Документы" },
  { to: "/policies", label: "Политики" },
  { to: "/models", label: "Модели" },
  { to: "/audit-log", label: "Аудит" },
  { to: "/incidents", label: "Инциденты" },
];

export default function AppLayout() {
  const { user, logout } = useAuth();
  return (
    <div className="min-h-screen flex bg-slate-950 text-slate-50">
      <aside className="w-56 border-r border-slate-800 p-4 flex flex-col">
        <div className="font-semibold text-lg mb-6">⛨ Рубеж ИИ</div>
        <nav className="flex flex-col gap-1 flex-1">
          {NAV.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              className={({ isActive }) =>
                `px-3 py-2 rounded text-sm ${
                  isActive
                    ? "bg-cyan-500/15 text-cyan-300"
                    : "text-slate-300 hover:bg-slate-800"
                }`
              }
            >
              {n.label}
            </NavLink>
          ))}
        </nav>
        <div className="text-xs text-slate-500 mt-4">
          <div>{user?.role}</div>
          <div className="truncate" title={user?.user_id}>
            {user?.user_id?.slice(0, 8)}
          </div>
          <button
            onClick={logout}
            className="mt-2 text-cyan-400 hover:text-cyan-300"
          >
            Выйти →
          </button>
        </div>
      </aside>
      <main className="flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  );
}

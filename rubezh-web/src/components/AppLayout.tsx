import { NavLink, Outlet } from "react-router-dom";
import {
  MessageSquare,
  FileText,
  Shield,
  Cpu,
  ClipboardList,
  AlertTriangle,
  LogOut,
  ShieldCheck,
} from "lucide-react";
import { useAuth } from "../auth/context";

const NAV = [
  { to: "/chat", label: "Чат", icon: MessageSquare },
  { to: "/documents", label: "Документы", icon: FileText },
  { to: "/policies", label: "Политики", icon: Shield },
  { to: "/models", label: "Модели", icon: Cpu },
  { to: "/audit-log", label: "Аудит", icon: ClipboardList },
  { to: "/incidents", label: "Инциденты", icon: AlertTriangle },
];

export default function AppLayout() {
  const { user, logout } = useAuth();
  return (
    <div className="min-h-screen flex bg-slate-950 text-slate-50">
      <aside className="w-60 border-r border-slate-800/80 p-4 flex flex-col bg-slate-950/95 backdrop-blur">
        <div className="flex items-center gap-2 mb-7 px-2">
          <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-cyan-400 to-cyan-600 flex items-center justify-center shadow-lg shadow-cyan-500/30">
            <ShieldCheck className="w-5 h-5 text-slate-950" strokeWidth={2.5} />
          </div>
          <div>
            <div className="font-semibold text-base leading-tight">
              Рубеж&nbsp;ИИ
            </div>
            <div className="text-[10px] text-slate-500 uppercase tracking-wider">
              AI Gateway
            </div>
          </div>
        </div>
        <nav className="flex flex-col gap-0.5 flex-1">
          {NAV.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              className={({ isActive }) =>
                `flex items-center gap-2.5 px-3 py-2 rounded-md text-sm transition-colors ${
                  isActive
                    ? "bg-cyan-500/15 text-cyan-300 shadow-inner shadow-cyan-500/10"
                    : "text-slate-300 hover:bg-slate-800/60 hover:text-slate-100"
                }`
              }
            >
              <n.icon className="w-4 h-4" strokeWidth={2} />
              {n.label}
            </NavLink>
          ))}
        </nav>
        <div className="mt-4 pt-4 border-t border-slate-800/80 px-2">
          <div className="text-xs text-slate-400 mb-1">{user?.role}</div>
          <div
            className="text-[10px] text-slate-600 truncate mb-2"
            title={user?.user_id}
          >
            {user?.user_id?.slice(0, 8)}
          </div>
          <button
            onClick={logout}
            className="flex items-center gap-1.5 text-xs text-slate-400 hover:text-cyan-300 transition-colors"
          >
            <LogOut className="w-3.5 h-3.5" strokeWidth={2} />
            Выйти
          </button>
        </div>
      </aside>
      <main className="flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  );
}

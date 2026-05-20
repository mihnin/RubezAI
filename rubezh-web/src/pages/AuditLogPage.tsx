import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Download, AlertTriangle, ClipboardList } from "lucide-react";
import { apiFetch, apiDownload, ApiError } from "../api/client";
import { AuditListSchema, type AuditEvent } from "../api/schemas";
import { SkeletonRows } from "../components/Skeleton";
import { EmptyState } from "../components/EmptyState";

/** AuditLogPage. Контракт — auditEventSummaryDTO. */
export default function AuditLogPage() {
  const [filter, setFilter] = useState("");
  const [exportError, setExportError] = useState<string | null>(null);

  const { data, isLoading, error } = useQuery({
    queryKey: ["audit", filter],
    queryFn: () =>
      apiFetch(
        `/api/audit-events${filter ? `?event_type=${encodeURIComponent(filter)}` : ""}`,
        AuditListSchema,
      ),
  });

  async function exportCsv() {
    setExportError(null);
    const ts = new Date().toISOString().slice(0, 19).replace(/[:]/g, "-");
    try {
      await apiDownload(
        `/api/audit-events/export${filter ? `?event_type=${filter}` : ""}`,
        `audit-${ts}.csv`,
      );
    } catch (e) {
      setExportError(e instanceof ApiError ? `HTTP ${e.status}` : "Сбой");
    }
  }

  return (
    <div className="p-8">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tight">Журнал аудита</h1>
        <p className="text-sm text-slate-500 mt-1">
          Append-only. Все действия сохранены без raw-данных пользователей.
        </p>
      </header>

      <div className="mb-4 flex gap-3 items-center text-sm">
        <label className="text-slate-400">Тип события:</label>
        <select
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="bg-slate-900 border border-slate-800 rounded-lg px-3 py-1.5 focus:outline-none focus:border-cyan-500 focus:ring-2 focus:ring-cyan-500/20"
        >
          <option value="">все</option>
          <option value="chat_request">chat_request</option>
          <option value="chat_response">chat_response</option>
          <option value="document_uploaded">document_uploaded</option>
          <option value="search_performed">search_performed</option>
          <option value="incident_created_auto">incident_created_auto</option>
        </select>
        <button
          onClick={exportCsv}
          className="ml-auto inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs text-cyan-300 hover:text-cyan-200 hover:bg-cyan-500/10 transition"
        >
          <Download className="w-3.5 h-3.5" strokeWidth={2} />
          Экспорт CSV
        </button>
      </div>
      {(exportError || error) && (
        <div
          role="alert"
          className="mb-3 text-sm text-red-300 bg-red-900/20 border border-red-800/50 rounded-lg px-3 py-2"
        >
          {exportError || (error as Error)?.message}
        </div>
      )}

      {isLoading ? (
        <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-4">
          <SkeletonRows count={8} className="h-8" />
        </div>
      ) : (data?.events?.length ?? 0) === 0 ? (
        <div className="bg-slate-900/60 border border-slate-800 rounded-xl">
          <EmptyState
            icon={ClipboardList}
            title="Событий не найдено"
            hint="Запустите чат-запрос или загрузите документ — записи появятся здесь."
          />
        </div>
      ) : (
        <div className="bg-slate-900/60 border border-slate-800 rounded-xl overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-slate-900 text-slate-400 text-xs uppercase tracking-wider">
              <tr>
                <th className="p-3 text-left font-medium">Время</th>
                <th className="p-3 text-left font-medium">Тип</th>
                <th className="p-3 text-left font-medium">User</th>
                <th className="p-3 text-left font-medium">Risk</th>
                <th className="p-3 text-left font-medium">Decision</th>
                <th className="p-3 text-left font-medium">Leak</th>
              </tr>
            </thead>
            <tbody>
              {data?.events?.map((e: AuditEvent) => (
                <tr
                  key={e.id}
                  className="border-t border-slate-800 hover:bg-slate-800/30"
                >
                  <td className="p-3 text-slate-400 whitespace-nowrap text-xs tabular-nums">
                    {new Date(e.created_at).toLocaleString("ru-RU")}
                  </td>
                  <td className="p-3 text-cyan-300 text-xs font-mono">
                    {e.event_type}
                  </td>
                  <td className="p-3 text-slate-500 text-xs font-mono">
                    {e.user_id.slice(0, 8)}
                  </td>
                  <td className="p-3 text-xs">
                    {e.risk_level ? (
                      <span
                        className={
                          e.risk_level === "high" ||
                          e.risk_level === "critical"
                            ? "text-red-300"
                            : e.risk_level === "medium"
                              ? "text-amber-300"
                              : "text-slate-300"
                        }
                      >
                        {e.risk_level}
                      </span>
                    ) : (
                      <span className="text-slate-600">—</span>
                    )}
                  </td>
                  <td className="p-3 text-xs">
                    {e.policy_decision ? (
                      <span
                        className={
                          e.policy_decision === "deny" ||
                          e.policy_decision === "escalate"
                            ? "text-red-300"
                            : e.policy_decision === "allow_masked"
                              ? "text-amber-300"
                              : "text-slate-300"
                        }
                      >
                        {e.policy_decision}
                      </span>
                    ) : (
                      <span className="text-slate-600">—</span>
                    )}
                  </td>
                  <td className="p-3 text-xs">
                    {e.has_leak ? (
                      <span className="inline-flex items-center gap-1 text-red-300">
                        <AlertTriangle
                          className="w-3 h-3"
                          strokeWidth={2.5}
                        />
                        утечка
                      </span>
                    ) : (
                      <span className="text-slate-600">—</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

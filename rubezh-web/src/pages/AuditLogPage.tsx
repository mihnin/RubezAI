import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { apiFetch, apiDownload, ApiError } from "../api/client";
import { AuditListSchema, type AuditEvent } from "../api/schemas";

/** AuditLogPage (Итерация 15). Контракт — auditEventSummaryDTO
 *  (rubezh-api/internal/api/audit.go: events + next_cursor). */
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
    <div className="p-6">
      <h1 className="text-xl font-semibold mb-4">Журнал аудита</h1>

      <div className="mb-4 flex gap-2 items-center text-sm">
        <label className="text-slate-400">Тип события:</label>
        <select
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="bg-slate-800 border border-slate-700 rounded px-2 py-1"
        >
          <option value="">все</option>
          <option value="chat_request_received">chat_request_received</option>
          <option value="chat_response_completed">chat_response_completed</option>
          <option value="document_uploaded">document_uploaded</option>
          <option value="search_performed">search_performed</option>
          <option value="incident_created_auto">incident_created_auto</option>
        </select>
        <button
          onClick={exportCsv}
          className="ml-auto text-xs text-cyan-400 hover:text-cyan-300"
        >
          Экспорт CSV →
        </button>
      </div>
      {exportError && (
        <div role="alert" className="mb-2 text-xs text-red-300">
          {exportError}
        </div>
      )}
      {error && (
        <div role="alert" className="mb-2 text-xs text-red-300">
          {(error as Error).message}
        </div>
      )}

      <div className="bg-slate-900 border border-slate-700 rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-slate-800 text-slate-400 text-xs uppercase">
            <tr>
              <th className="p-2 text-left">Время</th>
              <th className="p-2 text-left">Тип</th>
              <th className="p-2 text-left">Пользователь</th>
              <th className="p-2 text-left">Risk</th>
              <th className="p-2 text-left">Decision</th>
              <th className="p-2 text-left">Leak</th>
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              <tr>
                <td colSpan={6} className="p-6 text-center text-slate-500">
                  Загрузка…
                </td>
              </tr>
            )}
            {data?.events?.map((e: AuditEvent) => (
              <tr key={e.id} className="border-t border-slate-800 hover:bg-slate-800/30">
                <td className="p-2 text-slate-400 whitespace-nowrap text-xs">
                  {new Date(e.created_at).toLocaleString("ru-RU")}
                </td>
                <td className="p-2 text-cyan-300 text-xs">{e.event_type}</td>
                <td className="p-2 text-slate-400 text-xs">
                  {e.user_id.slice(0, 8)}
                </td>
                <td className="p-2 text-xs text-slate-300">
                  {e.risk_level ?? "—"}
                </td>
                <td className="p-2 text-xs">
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
                    "—"
                  )}
                </td>
                <td className="p-2 text-xs">
                  {e.has_leak ? (
                    <span className="text-red-300">⚠</span>
                  ) : (
                    <span className="text-slate-500">—</span>
                  )}
                </td>
              </tr>
            ))}
            {!isLoading && (data?.events?.length ?? 0) === 0 && (
              <tr>
                <td colSpan={6} className="p-6 text-center text-slate-500">
                  Событий не найдено
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

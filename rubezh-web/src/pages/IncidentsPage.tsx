import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { apiFetch, apiFetchRaw } from "../api/client";
import { IncidentListSchema, type Incident } from "../api/schemas";

const SEVERITY_COLOR: Record<string, string> = {
  low: "bg-slate-700 text-slate-300",
  medium: "bg-amber-500/20 text-amber-300",
  high: "bg-orange-500/20 text-orange-300",
  critical: "bg-red-500/20 text-red-300",
};

const STATUS_NEXT: Record<string, string[]> = {
  open: ["in_progress", "closed"],
  in_progress: ["closed"],
  closed: [],
};

/** IncidentsPage (Итерация 15). Контракт — incidentDTO
 *  (rubezh-api/internal/api/incidents.go: incidents + next_cursor). */
export default function IncidentsPage() {
  const [status, setStatus] = useState("");
  const { data, isLoading, error } = useQuery({
    queryKey: ["incidents", status],
    queryFn: () =>
      apiFetch(
        `/api/incidents${status ? `?status=${encodeURIComponent(status)}` : ""}`,
        IncidentListSchema,
      ),
  });

  return (
    <div className="p-6">
      <h1 className="text-xl font-semibold mb-4">Инциденты</h1>
      <div className="mb-4 flex gap-2 text-sm">
        <label className="text-slate-400">Статус:</label>
        <select
          value={status}
          onChange={(e) => setStatus(e.target.value)}
          className="bg-slate-800 border border-slate-700 rounded px-2 py-1"
        >
          <option value="">все</option>
          <option value="open">open</option>
          <option value="in_progress">in_progress</option>
          <option value="closed">closed</option>
        </select>
      </div>

      <div className="space-y-2">
        {isLoading && <div className="text-slate-500">Загрузка…</div>}
        {error && (
          <div role="alert" className="text-sm text-red-300">
            {(error as Error).message}
          </div>
        )}
        {data?.incidents?.map((i: Incident) => (
          <IncidentCard key={i.id} inc={i} />
        ))}
        {!isLoading && (data?.incidents?.length ?? 0) === 0 && (
          <div className="text-slate-500">Инцидентов нет</div>
        )}
      </div>
    </div>
  );
}

function IncidentCard({ inc }: { inc: Incident }) {
  const qc = useQueryClient();
  const [err, setErr] = useState<string | null>(null);

  // MVP: optimistic concurrency через If-Match не используется, т.к. backend DTO
  // не возвращает etag-поле в JSON (ETag должен браться из response-header — техдолг F1).
  // PATCH без If-Match. При коллизии — последний writer выигрывает.
  const patchMut = useMutation({
    mutationFn: (status: string) =>
      apiFetchRaw(`/api/incidents/${inc.id}`, {
        method: "PATCH",
        body: JSON.stringify({ status }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["incidents"] }),
    onError: (e: Error) => setErr(e.message),
  });

  const nextStatuses = STATUS_NEXT[inc.status] ?? [];

  return (
    <div className="bg-slate-900 border border-slate-700 rounded-lg p-4">
      <div className="flex items-start justify-between mb-2 gap-3">
        <div className="flex-1">
          <h3 className="font-medium">{inc.title}</h3>
          {inc.summary && (
            <p className="text-sm text-slate-400 mt-1">{inc.summary}</p>
          )}
        </div>
        <div className="flex flex-col items-end gap-1">
          <span
            className={`text-xs px-2 py-0.5 rounded ${SEVERITY_COLOR[inc.severity] ?? "bg-slate-700"}`}
          >
            {inc.severity}
          </span>
          <span className="text-xs px-2 py-0.5 rounded bg-slate-700 text-slate-300">
            {inc.status}
          </span>
        </div>
      </div>
      <div className="text-xs text-slate-500 flex gap-4 mt-2">
        {inc.trigger && <span>trigger: {inc.trigger}</span>}
        <span>{new Date(inc.created_at).toLocaleString("ru-RU")}</span>
        {inc.reporter_id === null && (
          <span className="text-amber-400">auto</span>
        )}
      </div>
      {err && (
        <div className="mt-2 text-xs text-red-300" role="alert">
          {err}
        </div>
      )}
      {nextStatuses.length > 0 && (
        <div className="mt-3 pt-3 border-t border-slate-800 flex gap-2">
          {nextStatuses.map((s) => (
            <button
              key={s}
              onClick={() => patchMut.mutate(s)}
              disabled={patchMut.isPending}
              className="text-xs px-2 py-1 rounded bg-slate-800 hover:bg-slate-700 disabled:opacity-40"
            >
              → {s}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

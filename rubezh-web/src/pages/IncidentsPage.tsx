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
  open: ["investigating", "resolved", "false_positive"],
  investigating: ["resolved", "false_positive"],
  resolved: [],
  false_positive: [],
};

const STATUS_LABEL: Record<string, string> = {
  open: "открыт",
  investigating: "расследование",
  resolved: "закрыт",
  false_positive: "ложное срабатывание",
};

const TERMINAL_STATUSES = new Set(["resolved", "false_positive"]);

/** IncidentsPage (F1+F3). PATCH через If-Match (RFC 7232). Терминальный
 *  переход требует resolution (бизнес-правило backend incidentPatchDTO). */
export default function IncidentsPage() {
  const [statusFilter, setStatusFilter] = useState("");
  const { data, isLoading, error } = useQuery({
    queryKey: ["incidents", statusFilter],
    queryFn: () =>
      apiFetch(
        `/api/incidents${statusFilter ? `?status=${encodeURIComponent(statusFilter)}` : ""}`,
        IncidentListSchema,
      ),
  });

  return (
    <div className="p-6">
      <h1 className="text-xl font-semibold mb-4">Инциденты</h1>
      <div className="mb-4 flex gap-2 text-sm">
        <label className="text-slate-400">Статус:</label>
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className="bg-slate-800 border border-slate-700 rounded px-2 py-1"
        >
          <option value="">все</option>
          <option value="open">open</option>
          <option value="investigating">investigating</option>
          <option value="resolved">resolved</option>
          <option value="false_positive">false_positive</option>
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
  const [closing, setClosing] = useState<string | null>(null);

  const patchMut = useMutation({
    mutationFn: (vars: { status: string; resolution?: string }) =>
      apiFetchRaw(`/api/incidents/${inc.id}`, {
        method: "PATCH",
        headers: { "If-Match": inc.updated_at },
        body: JSON.stringify(vars),
      }),
    onSuccess: () => {
      setClosing(null);
      qc.invalidateQueries({ queryKey: ["incidents"] });
    },
    onError: (e: Error) => setErr(e.message),
  });

  const onClick = (status: string) => {
    if (TERMINAL_STATUSES.has(status)) {
      setClosing(status);
    } else {
      patchMut.mutate({ status });
    }
  };

  const nextStatuses = STATUS_NEXT[inc.status] ?? [];

  return (
    <div className="bg-slate-900 border border-slate-700 rounded-lg p-4">
      <div className="flex items-start justify-between mb-2 gap-3">
        <div className="flex-1">
          <h3 className="font-medium">{inc.title}</h3>
          {inc.summary && (
            <p className="text-sm text-slate-400 mt-1">{inc.summary}</p>
          )}
          {inc.resolution && (
            <p className="mt-2 text-xs text-emerald-300 italic">
              Решение: {inc.resolution}
            </p>
          )}
        </div>
        <div className="flex flex-col items-end gap-1">
          <span
            className={`text-xs px-2 py-0.5 rounded ${SEVERITY_COLOR[inc.severity] ?? "bg-slate-700"}`}
          >
            {inc.severity}
          </span>
          <span className="text-xs px-2 py-0.5 rounded bg-slate-700 text-slate-300">
            {STATUS_LABEL[inc.status] ?? inc.status}
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
          {err.includes("412") &&
            " — инцидент изменён другим пользователем. Обновите страницу."}
        </div>
      )}
      {nextStatuses.length > 0 && (
        <div className="mt-3 pt-3 border-t border-slate-800 flex gap-2">
          {nextStatuses.map((s) => (
            <button
              key={s}
              onClick={() => onClick(s)}
              disabled={patchMut.isPending}
              className="text-xs px-2 py-1 rounded bg-slate-800 hover:bg-slate-700 disabled:opacity-40"
            >
              → {STATUS_LABEL[s] ?? s}
            </button>
          ))}
        </div>
      )}

      {closing && (
        <ResolutionDialog
          status={closing}
          onCancel={() => setClosing(null)}
          onConfirm={(resolution) => patchMut.mutate({ status: closing, resolution })}
          busy={patchMut.isPending}
        />
      )}
    </div>
  );
}

function ResolutionDialog({
  status,
  onCancel,
  onConfirm,
  busy,
}: {
  status: string;
  onCancel: () => void;
  onConfirm: (resolution: string) => void;
  busy: boolean;
}) {
  const [text, setText] = useState("");
  return (
    <div
      className="fixed inset-0 bg-black/60 flex items-center justify-center"
      onClick={onCancel}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="bg-slate-900 border border-slate-700 rounded-lg p-6 w-[480px]"
      >
        <h2 className="font-semibold mb-1">
          Закрыть инцидент: {STATUS_LABEL[status]}
        </h2>
        <p className="text-xs text-slate-500 mb-4">
          Резолюция обязательна. Будет записана в audit и в incident_notes.
        </p>
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          autoFocus
          rows={5}
          className="w-full bg-slate-800 border border-slate-700 rounded p-2 text-sm"
          placeholder="Опишите принятые меры…"
        />
        <div className="mt-4 flex gap-2 justify-end">
          <button
            onClick={onCancel}
            className="px-3 py-1.5 text-sm text-slate-400"
          >
            Отмена
          </button>
          <button
            onClick={() => onConfirm(text)}
            disabled={busy || text.trim().length < 3}
            className="px-3 py-1.5 text-sm rounded bg-cyan-500 text-slate-950 disabled:opacity-40"
          >
            Подтвердить
          </button>
        </div>
      </div>
    </div>
  );
}

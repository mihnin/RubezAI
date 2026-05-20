import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../api/client";

interface Policy {
  id: string;
  name: string;
  description: string;
  enabled: boolean;
  thresholds?: Record<string, unknown>;
}

interface PolicyList {
  items: Policy[];
}

/** PoliciesPage (Итерация 14). Read-only MVP. docs/design/ui/policies.md. */
export default function PoliciesPage() {
  const { data, isLoading, error } = useQuery<PolicyList>({
    queryKey: ["policies"],
    queryFn: () => apiFetch("/api/policies"),
  });

  return (
    <div className="p-6 max-w-4xl">
      <h1 className="text-xl font-semibold mb-1">Политики</h1>
      <p className="text-sm text-slate-500 mb-4">
        Read-only в MVP. Изменение — через API после согласования с ИБ.
      </p>

      {isLoading && <div className="text-slate-500">Загрузка…</div>}
      {error && (
        <div className="text-red-400">Ошибка: {(error as Error).message}</div>
      )}

      <div className="space-y-3">
        {data?.items?.map((p) => (
          <div
            key={p.id}
            className="bg-slate-900 border border-slate-700 rounded-lg p-4"
          >
            <div className="flex items-center justify-between mb-2">
              <h3 className="font-medium">{p.name}</h3>
              <span
                className={`text-xs px-2 py-0.5 rounded ${
                  p.enabled
                    ? "bg-emerald-500/20 text-emerald-300"
                    : "bg-slate-700 text-slate-400"
                }`}
              >
                {p.enabled ? "включена" : "выключена"}
              </span>
            </div>
            <p className="text-sm text-slate-400">{p.description}</p>
            {p.thresholds && (
              <pre className="mt-3 text-xs bg-slate-950 p-2 rounded overflow-auto text-slate-400">
                {JSON.stringify(p.thresholds, null, 2)}
              </pre>
            )}
          </div>
        ))}
        {!isLoading && (data?.items?.length ?? 0) === 0 && (
          <div className="text-slate-500">Политики не настроены</div>
        )}
      </div>
    </div>
  );
}

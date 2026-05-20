import { useQuery } from "@tanstack/react-query";
import { Shield, CheckCircle2, XCircle } from "lucide-react";
import { apiFetch } from "../api/client";
import { PolicyListSchema, type Policy } from "../api/schemas";
import { SkeletonRows } from "../components/Skeleton";
import { EmptyState } from "../components/EmptyState";

/** PoliciesPage. Read-only MVP. docs/design/ui/policies.md. */
export default function PoliciesPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ["policies"],
    queryFn: () => apiFetch("/api/policies", PolicyListSchema),
  });

  return (
    <div className="p-8 max-w-5xl">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tight">Политики</h1>
        <p className="text-sm text-slate-500 mt-1">
          Read-only в MVP. Изменения — через API после согласования с ИБ.
        </p>
      </header>

      {isLoading && <SkeletonRows count={3} className="h-24" />}
      {error && (
        <div className="text-red-400 text-sm">
          Ошибка: {(error as Error).message}
        </div>
      )}

      {!isLoading && (data?.length ?? 0) === 0 && (
        <div className="bg-slate-900/60 border border-slate-800 rounded-xl">
          <EmptyState
            icon={Shield}
            title="Политики не настроены"
            hint="В рабочей среде ИБ-офицер создаст набор правил для классов риска."
          />
        </div>
      )}

      <div className="space-y-3">
        {data?.map((p: Policy) => (
          <div
            key={p.id}
            className="bg-slate-900/60 border border-slate-800 rounded-xl p-5 hover:border-slate-700 transition"
          >
            <div className="flex items-start justify-between gap-3 mb-2">
              <div className="flex items-start gap-3">
                <div className="w-9 h-9 rounded-lg bg-cyan-500/10 flex items-center justify-center shrink-0">
                  <Shield
                    className="w-4 h-4 text-cyan-300"
                    strokeWidth={2}
                  />
                </div>
                <div>
                  <h3 className="font-medium">{p.name}</h3>
                  <p className="text-sm text-slate-400 mt-0.5">
                    {p.description}
                  </p>
                </div>
              </div>
              <span
                className={`text-xs px-2.5 py-1 rounded-full font-medium inline-flex items-center gap-1 ${
                  p.is_active
                    ? "bg-emerald-500/15 text-emerald-300"
                    : "bg-slate-800 text-slate-500"
                }`}
              >
                {p.is_active ? (
                  <CheckCircle2 className="w-3 h-3" strokeWidth={2.5} />
                ) : (
                  <XCircle className="w-3 h-3" strokeWidth={2.5} />
                )}
                {p.is_active ? "активна" : "неактивна"}
              </span>
            </div>
            <div className="text-xs text-slate-500 mt-3">
              версия {p.current_version}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

import { useMemo } from "react";
import { AlertTriangle } from "lucide-react";
import type { ChatPreview } from "../api/schemas";

/** CloudGate — модал подтверждения перед отправкой в облако (J.1): показывает
 *  обезличенный текст и статистику; raw тут не фигурирует (только псевдонимы). */
export function CloudGate({
  gate,
  provider,
  onCancel,
  onConfirm,
}: {
  gate: ChatPreview & { input: string };
  provider: string;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const byType = useMemo(() => {
    const m: Record<string, number> = {};
    for (const e of gate.entities) m[e.type] = (m[e.type] ?? 0) + 1;
    return Object.entries(m).sort((a, b) => b[1] - a[1]);
  }, [gate.entities]);
  const riskColor =
    gate.risk.level === "critical" || gate.risk.level === "high"
      ? "bg-red-500/20 text-red-300"
      : gate.risk.level === "medium"
        ? "bg-amber-500/20 text-amber-300"
        : "bg-slate-700 text-slate-300";
  return (
    <div
      className="fixed inset-0 bg-black/60 flex items-center justify-center z-20"
      onClick={onCancel}
    >
      <div
        role="alertdialog"
        aria-label="Подтверждение отправки в облако"
        onClick={(e) => e.stopPropagation()}
        className="bg-slate-900 border border-slate-700 rounded-xl p-6 w-[560px] max-h-[85vh] overflow-auto"
      >
        <div className="flex items-start gap-3 mb-4">
          <AlertTriangle
            className="w-5 h-5 text-amber-400 shrink-0 mt-0.5"
            strokeWidth={2}
          />
          <div>
            <h2 className="font-semibold">Данные уйдут за контур предприятия</h2>
            <p className="text-sm text-slate-400">
              Провайдер <span className="font-mono">{provider}</span> (облако).
              Проверьте обезличенный текст перед отправкой.
            </p>
          </div>
        </div>
        <div className="text-[11px] uppercase tracking-wider text-slate-500 mb-1">
          Обезличенный текст (уйдёт в LLM)
        </div>
        <div className="bg-slate-800/70 border border-slate-700 rounded-lg p-3 text-sm font-mono whitespace-pre-wrap leading-relaxed mb-4 max-h-48 overflow-auto">
          {gate.sanitized_text}
        </div>
        <div className="flex items-center gap-3 mb-1 text-sm">
          <span className="text-slate-400">Найдено и заменено:</span>
          <span className="font-semibold">{gate.entities.length}</span>
          <span className={`text-xs px-2 py-0.5 rounded-full ${riskColor}`}>
            ● {gate.risk.level}
          </span>
        </div>
        {byType.length > 0 && (
          <div className="text-xs text-slate-400 mb-5">
            {byType.map(([t, n]) => `${t} ${n}`).join(" · ")}
            {gate.risk.classes.length > 0 && (
              <> &nbsp;|&nbsp; классы: {gate.risk.classes.join(" · ")}</>
            )}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <button
            onClick={onCancel}
            className="px-3 py-1.5 text-sm text-slate-400"
          >
            Отмена
          </button>
          <button
            onClick={onConfirm}
            className="px-3 py-1.5 text-sm rounded-lg bg-cyan-500 text-slate-950 font-medium"
          >
            Отправить в облако
          </button>
        </div>
      </div>
    </div>
  );
}

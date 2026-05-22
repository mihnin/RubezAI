import { useState } from "react";
import { ChevronDown, AlertTriangle, ShieldCheck } from "lucide-react";
import type { Model } from "../api/schemas";

interface PickerProps {
  providers: Model[];
  providerName: string;
  modelName: string;
  onProviderChange: (p: Model) => void;
  onModelChange: (m: string) => void;
}

/** ProviderModelPicker — выбор провайдера/модели в чате с явным разделением
 *  «Облачные» (external, данные за контур) и «Локальные» (trusted_local, raw
 *  в периметре). Группировка помогает не отправить ПДн в облако по ошибке. */
export function ProviderModelPicker({
  providers,
  providerName,
  modelName,
  onProviderChange,
  onModelChange,
}: PickerProps) {
  const [open, setOpen] = useState(false);
  const active = providers.find((p) => p.name === providerName);
  if (!active) {
    return (
      <div className="text-xs text-amber-400">нет активных провайдеров</div>
    );
  }
  const cloud = providers.filter((p) => p.trust_level === "external");
  const local = providers.filter((p) => p.trust_level !== "external");
  const isCloud = active.trust_level === "external";
  return (
    <div className="relative">
      <button
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-2 text-xs bg-slate-900/60 border border-slate-800 rounded-full px-3 py-1.5 hover:border-slate-700 transition"
      >
        {isCloud ? (
          <AlertTriangle className="w-3.5 h-3.5 text-amber-400" strokeWidth={2} />
        ) : (
          <ShieldCheck className="w-3.5 h-3.5 text-emerald-400" strokeWidth={2} />
        )}
        <span className="text-slate-200 font-medium">{active.name}</span>
        <span className="text-slate-600">·</span>
        <span className="text-slate-500 font-mono max-w-[180px] truncate">
          {modelName}
        </span>
        <ChevronDown
          className={`w-3 h-3 text-slate-500 transition-transform ${open ? "rotate-180" : ""}`}
          strokeWidth={2.5}
        />
      </button>
      {open && (
        <div className="absolute right-0 mt-2 w-[360px] bg-slate-900 border border-slate-700 rounded-xl p-3 shadow-2xl z-10">
          <ProviderGroup
            label="Облачные"
            hint="⚠ данные уходят за контур"
            hintClass="text-amber-300/80"
            list={cloud}
            providerName={providerName}
            onPick={(p) => {
              onProviderChange(p);
              setOpen(false);
            }}
          />
          <ProviderGroup
            label="Локальные"
            hint="🛡 в периметре, raw"
            hintClass="text-emerald-300/80"
            list={local}
            providerName={providerName}
            onPick={(p) => {
              onProviderChange(p);
              setOpen(false);
            }}
          />
          <div className="text-[10px] uppercase tracking-wider text-slate-500 mb-1.5 mt-3">
            Модель
          </div>
          <input
            value={modelName}
            onChange={(e) => onModelChange(e.target.value)}
            placeholder="например: deepseek-r1-distill-qwen-7b"
            className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-xs font-mono focus:outline-none focus:border-cyan-500"
          />
          <p className="text-[10px] text-slate-500 mt-1.5 leading-relaxed">
            Имя модели передаётся как поле <code>model</code> в OpenAI-
            совместимый endpoint провайдера. Сохраняется в&nbsp;localStorage.
          </p>
        </div>
      )}
    </div>
  );
}

function ProviderGroup({
  label,
  hint,
  hintClass,
  list,
  providerName,
  onPick,
}: {
  label: string;
  hint: string;
  hintClass: string;
  list: Model[];
  providerName: string;
  onPick: (p: Model) => void;
}) {
  return (
    <>
      <div className="text-[10px] uppercase tracking-wider text-slate-500 mb-1">
        {label} <span className={hintClass}>{hint}</span>
      </div>
      <div className="space-y-1 mb-2">
        {list.length === 0 && (
          <div className="px-2.5 py-1 text-[11px] text-slate-600">
            нет включённых
          </div>
        )}
        {list.map((p) => (
          <button
            key={p.id}
            onClick={() => onPick(p)}
            className={`w-full text-left px-2.5 py-1.5 rounded text-xs ${
              p.name === providerName
                ? "bg-cyan-500/15 text-cyan-300"
                : "hover:bg-slate-800/60 text-slate-300"
            }`}
          >
            <div className="flex items-center justify-between gap-2">
              <span className="font-medium">{p.name}</span>
              <span className="text-[10px] text-slate-500">{p.trust_level}</span>
            </div>
            <div className="font-mono text-[10px] text-slate-600 truncate">
              {p.endpoint}
            </div>
          </button>
        ))}
      </div>
    </>
  );
}

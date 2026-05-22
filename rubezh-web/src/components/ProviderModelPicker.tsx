import { useState } from "react";
import { ChevronDown, AlertTriangle, ShieldCheck } from "lucide-react";
import type { Model } from "../api/schemas";

// modelSuggestions — частые имена моделей по провайдеру (для подсказок выбора).
// Это лишь подсказки; поле остаётся свободным вводом.
function modelSuggestions(providerName: string): string[] {
  const n = providerName.toLowerCase();
  if (n.includes("deepseek")) {
    return n.includes("local")
      ? ["deepseek-r1-distill-qwen-7b"]
      : ["deepseek-chat", "deepseek-reasoner"];
  }
  if (n.includes("claude") || n.includes("anthropic")) {
    return ["claude-sonnet-4-6", "claude-opus-4-7", "claude-haiku-4-5-20251001"];
  }
  if (n.includes("gpt") || n.includes("openai")) {
    return ["gpt-4o", "gpt-4o-mini"];
  }
  if (n.includes("gemini") || n.includes("google")) {
    return ["gemini-2.0-flash", "gemini-2.5-pro"];
  }
  if (n.includes("grok") || n.includes("xai")) {
    return ["grok-2-latest"];
  }
  return [];
}

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
            list="model-suggestions"
            placeholder="например: deepseek-chat"
            className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-xs font-mono focus:outline-none focus:border-cyan-500"
          />
          <datalist id="model-suggestions">
            {modelSuggestions(active.name).map((m) => (
              <option key={m} value={m} />
            ))}
          </datalist>
          {modelSuggestions(active.name).length > 0 && (
            <div className="flex flex-wrap gap-1 mt-1.5">
              {modelSuggestions(active.name).map((m) => (
                <button
                  key={m}
                  onClick={() => onModelChange(m)}
                  className={`text-[10px] font-mono px-1.5 py-0.5 rounded ${
                    modelName === m
                      ? "bg-cyan-500/15 text-cyan-300"
                      : "bg-slate-800 text-slate-400 hover:text-slate-200"
                  }`}
                >
                  {m}
                </button>
              ))}
            </div>
          )}
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

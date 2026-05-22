import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Cpu, Plus, Key, ShieldCheck, AlertTriangle, Power, Trash2 } from "lucide-react";
import { apiFetch, apiFetchRaw, ApiError } from "../api/client";
import { useAuth } from "../auth/context";
import { ModelListSchema, type Model } from "../api/schemas";
import { SkeletonRows } from "../components/Skeleton";
import { EmptyState } from "../components/EmptyState";

/** ModelsPage (Итерация 15). admin/security_officer пишет; user читает.
 *  Контракт — modelProviderDTO (rubezh-api/internal/api/models.go). */
export default function ModelsPage() {
  const qc = useQueryClient();
  const { user } = useAuth();
  const canWrite = ["admin", "security_officer"].includes(user?.role ?? "");

  const { data, isLoading } = useQuery({
    queryKey: ["models"],
    queryFn: () => apiFetch("/api/models", ModelListSchema),
  });

  const [showCreate, setShowCreate] = useState(false);

  return (
    <div className="p-8 max-w-5xl">
      <header className="flex items-end justify-between mb-6">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">
            LLM-провайдеры
          </h1>
          <p className="text-sm text-slate-500 mt-1">
            trusted_local получает raw данные; external — только masked.
          </p>
        </div>
        {canWrite && (
          <button
            onClick={() => setShowCreate(true)}
            className="inline-flex items-center gap-1.5 text-sm px-3 py-2 rounded-lg bg-cyan-500 hover:bg-cyan-400 text-slate-950 font-medium shadow-lg shadow-cyan-500/20"
          >
            <Plus className="w-4 h-4" strokeWidth={2.5} />
            Добавить
          </button>
        )}
      </header>

      {isLoading && <SkeletonRows count={2} className="h-28" />}

      <div className="space-y-3">
        {data?.map((m: Model) => (
          <ModelRow key={m.id} model={m} canWrite={canWrite} />
        ))}
        {!isLoading && (data?.length ?? 0) === 0 && (
          <div className="bg-slate-900/60 border border-slate-800 rounded-xl">
            <EmptyState
              icon={Cpu}
              title="Провайдеры не настроены"
              hint="Создайте первого провайдера — chat станет доступен сразу (без restart api)."
            />
          </div>
        )}
      </div>

      {showCreate && canWrite && (
        <CreateModelModal
          onClose={() => setShowCreate(false)}
          onCreated={() => {
            qc.invalidateQueries({ queryKey: ["models"] });
            setShowCreate(false);
          }}
        />
      )}
    </div>
  );
}

function ModelRow({ model, canWrite }: { model: Model; canWrite: boolean }) {
  const qc = useQueryClient();
  const [editKey, setEditKey] = useState(false);
  const [newKey, setNewKey] = useState("");
  const [confirmDel, setConfirmDel] = useState(false);
  const [delErr, setDelErr] = useState<string | null>(null);

  const updMut = useMutation({
    mutationFn: () =>
      apiFetchRaw(`/api/models/${model.id}/api-key`, {
        method: "POST",
        body: JSON.stringify({ api_key: newKey }),
      }),
    onSuccess: () => {
      setEditKey(false);
      setNewKey("");
      qc.invalidateQueries({ queryKey: ["models"] });
    },
  });

  const toggleMut = useMutation({
    mutationFn: () =>
      apiFetchRaw(`/api/models/${model.id}`, {
        method: "PATCH",
        body: JSON.stringify({ is_enabled: !model.is_enabled }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["models"] }),
  });

  const deleteMut = useMutation({
    mutationFn: () =>
      apiFetchRaw(`/api/models/${model.id}`, { method: "DELETE" }),
    onSuccess: () => {
      setConfirmDel(false);
      qc.invalidateQueries({ queryKey: ["models"] });
    },
    onError: (e: Error) => {
      // 409 — провайдер в истории: предлагаем soft-disable вместо удаления.
      setDelErr(
        e instanceof ApiError && e.status === 409
          ? "Провайдер используется в истории. Удаление невозможно — выключите его."
          : e.message,
      );
    },
  });

  return (
    <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-5 hover:border-slate-700 transition">
      <div className="flex items-start justify-between mb-3 gap-3">
        <div className="flex items-start gap-3">
          <div className="w-9 h-9 rounded-lg bg-cyan-500/10 flex items-center justify-center shrink-0">
            <Cpu className="w-4 h-4 text-cyan-300" strokeWidth={2} />
          </div>
          <div>
            <h3 className="font-medium">{model.name}</h3>
            <div className="text-xs text-slate-500 mt-0.5 font-mono">
              {model.adapter}
            </div>
          </div>
        </div>
        <div className="flex gap-1.5 flex-wrap justify-end">
          <span
            className={`text-xs px-2 py-0.5 rounded-full font-medium inline-flex items-center gap-1 ${
              model.trust_level === "trusted_local"
                ? "bg-emerald-500/15 text-emerald-300"
                : "bg-slate-800 text-slate-300"
            }`}
          >
            {model.trust_level === "trusted_local" && (
              <ShieldCheck className="w-3 h-3" strokeWidth={2.5} />
            )}
            {model.trust_level}
          </span>
          {model.has_api_key && (
            <span className="text-xs px-2 py-0.5 rounded-full bg-slate-800 text-slate-400 inline-flex items-center gap-1">
              <Key className="w-3 h-3" strokeWidth={2.5} /> ключ
            </span>
          )}
          {!model.is_enabled && (
            <span className="text-xs px-2 py-0.5 rounded-full bg-red-500/15 text-red-300 inline-flex items-center gap-1">
              <AlertTriangle className="w-3 h-3" strokeWidth={2.5} />
              выключен
            </span>
          )}
        </div>
      </div>
      <div className="text-sm text-slate-400 space-y-1 ml-12">
        <div className="font-mono text-xs text-slate-500 truncate">
          {model.endpoint}
        </div>
        {model.max_tokens !== null && (
          <div className="text-xs text-slate-500">
            max_tokens: {model.max_tokens}
          </div>
        )}
      </div>
      {canWrite && (
        <div className="mt-3 pt-3 border-t border-slate-800 flex items-center gap-4 flex-wrap">
          <button
            onClick={() => toggleMut.mutate()}
            disabled={toggleMut.isPending}
            className="text-xs inline-flex items-center gap-1 text-slate-300 hover:text-white disabled:opacity-40"
          >
            <Power className="w-3.5 h-3.5" strokeWidth={2.5} />
            {model.is_enabled ? "Выключить" : "Включить"}
          </button>
          {!confirmDel ? (
            <button
              onClick={() => {
                setDelErr(null);
                setConfirmDel(true);
              }}
              className="text-xs inline-flex items-center gap-1 text-red-400 hover:text-red-300"
            >
              <Trash2 className="w-3.5 h-3.5" strokeWidth={2.5} />
              Удалить
            </button>
          ) : (
            <span className="text-xs inline-flex items-center gap-2">
              <span className="text-slate-400">Удалить безвозвратно?</span>
              <button
                onClick={() => deleteMut.mutate()}
                disabled={deleteMut.isPending}
                className="px-2 py-0.5 rounded bg-red-500 text-slate-950 font-medium disabled:opacity-40"
              >
                Да
              </button>
              <button
                onClick={() => setConfirmDel(false)}
                className="text-slate-500"
              >
                Отмена
              </button>
            </span>
          )}
        </div>
      )}
      {delErr && (
        <div className="mt-2 text-xs text-red-300 flex items-center gap-2 ml-12">
          <AlertTriangle className="w-3.5 h-3.5" strokeWidth={2.5} />
          {delErr}
          <button
            onClick={() => {
              setConfirmDel(false);
              setDelErr(null);
              if (model.is_enabled) toggleMut.mutate();
            }}
            className="underline hover:text-red-200"
          >
            Выключить вместо удаления
          </button>
        </div>
      )}
      {canWrite && (
        <div className="mt-3 pt-3 border-t border-slate-800">
          {!editKey ? (
            <button
              onClick={() => setEditKey(true)}
              className="text-xs text-cyan-400 hover:text-cyan-300"
            >
              Изменить API-ключ
            </button>
          ) : (
            <div className="flex gap-2">
              <input
                type="password"
                value={newKey}
                onChange={(e) => setNewKey(e.target.value)}
                placeholder="новый ключ"
                className="flex-1 bg-slate-800 border border-slate-700 rounded px-2 py-1 text-sm"
              />
              <button
                onClick={() => updMut.mutate()}
                disabled={!newKey || updMut.isPending}
                className="text-xs px-2 rounded bg-cyan-500 text-slate-950 disabled:opacity-40"
              >
                Сохранить
              </button>
              <button
                onClick={() => {
                  setEditKey(false);
                  setNewKey("");
                }}
                className="text-xs text-slate-500"
              >
                Отмена
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

interface ModelForm {
  name: string;
  trust_level: string;
  adapter: string;
  endpoint: string;
  api_key: string;
}

function CreateModelModal({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: () => void;
}) {
  const [form, setForm] = useState<ModelForm>({
    name: "",
    trust_level: "external",
    adapter: "openai",
    endpoint: "",
    api_key: "",
  });
  const [err, setErr] = useState<string | null>(null);

  const mut = useMutation({
    mutationFn: () =>
      apiFetchRaw("/api/models", {
        method: "POST",
        body: JSON.stringify({
          name: form.name,
          trust_level: form.trust_level,
          adapter: form.adapter,
          endpoint: form.endpoint,
          api_key: form.api_key || null,
        }),
      }),
    onSuccess: onCreated,
    onError: (e: Error) => setErr(e.message),
  });

  return (
    <div
      className="fixed inset-0 bg-black/60 flex items-center justify-center"
      onClick={onClose}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="bg-slate-900 border border-slate-700 rounded-lg p-6 w-[480px]"
      >
        <h2 className="font-semibold mb-4">Новый провайдер</h2>
        <div className="space-y-3 text-sm">
          <Field label="Имя">
            <input
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1"
            />
          </Field>
          <Field label="Trust level">
            <select
              value={form.trust_level}
              onChange={(e) =>
                setForm({ ...form, trust_level: e.target.value })
              }
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1"
            >
              <option value="external">external</option>
              <option value="trusted_local">trusted_local</option>
              <option value="internal">internal</option>
            </select>
          </Field>
          <Field label="Adapter">
            <select
              value={form.adapter}
              onChange={(e) => setForm({ ...form, adapter: e.target.value })}
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1"
            >
              <option value="openai">openai</option>
              <option value="mock">mock</option>
            </select>
          </Field>
          <Field label="Endpoint">
            <input
              value={form.endpoint}
              onChange={(e) => setForm({ ...form, endpoint: e.target.value })}
              placeholder="http://localhost:1234/v1"
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1"
            />
          </Field>
          <Field label="API key (опционально)">
            <input
              type="password"
              value={form.api_key}
              onChange={(e) => setForm({ ...form, api_key: e.target.value })}
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1"
            />
          </Field>
          {err && <div className="text-red-300 text-xs">{err}</div>}
        </div>
        <div className="flex gap-2 mt-6 justify-end">
          <button onClick={onClose} className="px-3 py-1.5 text-sm text-slate-400">
            Отмена
          </button>
          <button
            onClick={() => mut.mutate()}
            disabled={mut.isPending || !form.name || !form.endpoint}
            className="px-3 py-1.5 text-sm rounded bg-cyan-500 text-slate-950 disabled:opacity-40"
          >
            Создать
          </button>
        </div>
      </div>
    </div>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <label className="block text-xs text-slate-400 mb-1">{label}</label>
      {children}
    </div>
  );
}

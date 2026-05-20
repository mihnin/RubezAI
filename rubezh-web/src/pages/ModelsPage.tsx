import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { apiFetch, apiFetchRaw } from "../api/client";
import { useAuth } from "../auth/context";
import { ModelListSchema, type Model } from "../api/schemas";

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
    <div className="p-6 max-w-4xl">
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-xl font-semibold">LLM-провайдеры</h1>
        {canWrite && (
          <button
            onClick={() => setShowCreate(true)}
            className="text-sm px-3 py-1.5 rounded bg-cyan-500 hover:bg-cyan-400 text-slate-950"
          >
            + Добавить
          </button>
        )}
      </div>

      {isLoading && <div className="text-slate-500">Загрузка…</div>}

      <div className="space-y-3">
        {data?.map((m: Model) => (
          <ModelRow key={m.id} model={m} canWrite={canWrite} />
        ))}
        {!isLoading && (data?.length ?? 0) === 0 && (
          <div className="text-slate-500">Провайдеры не настроены</div>
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

  return (
    <div className="bg-slate-900 border border-slate-700 rounded-lg p-4">
      <div className="flex items-center justify-between mb-2">
        <h3 className="font-medium">{model.name}</h3>
        <div className="flex gap-2">
          <span
            className={`text-xs px-2 py-0.5 rounded ${
              model.trust_level === "trusted_local"
                ? "bg-emerald-500/20 text-emerald-300"
                : "bg-slate-700 text-slate-300"
            }`}
          >
            {model.trust_level}
          </span>
          {model.has_api_key && (
            <span className="text-xs px-2 py-0.5 rounded bg-slate-700 text-slate-300">
              api_key set
            </span>
          )}
          {!model.is_enabled && (
            <span className="text-xs px-2 py-0.5 rounded bg-red-500/20 text-red-300">
              disabled
            </span>
          )}
        </div>
      </div>
      <div className="text-sm text-slate-400 space-y-0.5">
        <div>Adapter: {model.adapter}</div>
        <div className="truncate">Endpoint: {model.endpoint}</div>
        {model.max_tokens !== null && (
          <div>max_tokens: {model.max_tokens}</div>
        )}
      </div>
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

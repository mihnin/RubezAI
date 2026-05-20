import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { apiFetch, apiFetchRaw } from "../api/client";
import { useAuth } from "../auth/context";
import { ModelListSchema, type Model } from "../api/schemas";

/** ModelsPage (Итерация 15). admin/security_officer пишет; user читает. */
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
        {data?.items?.map((m) => (
          <ModelRow key={m.id} model={m} canWrite={canWrite} />
        ))}
        {!isLoading && (data?.items?.length ?? 0) === 0 && (
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
        method: "PUT",
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
          {model.trusted_local && (
            <span className="text-xs px-2 py-0.5 rounded bg-emerald-500/20 text-emerald-300">
              trusted_local
            </span>
          )}
          {model.has_api_key && (
            <span className="text-xs px-2 py-0.5 rounded bg-slate-700 text-slate-300">
              api_key set
            </span>
          )}
        </div>
      </div>
      <div className="text-sm text-slate-400 space-y-0.5">
        <div>Тип: {model.provider_type}</div>
        <div className="truncate">URL: {model.base_url}</div>
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

function CreateModelModal({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: () => void;
}) {
  const [form, setForm] = useState({
    name: "",
    provider_type: "openai",
    base_url: "",
    api_key: "",
    trusted_local: false,
  });
  const [err, setErr] = useState<string | null>(null);

  const mut = useMutation({
    mutationFn: () => apiFetchRaw("/api/models", { method: "POST", body: JSON.stringify(form) }),
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
          <Field label="Тип">
            <select
              value={form.provider_type}
              onChange={(e) =>
                setForm({ ...form, provider_type: e.target.value })
              }
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1"
            >
              <option value="openai">openai</option>
              <option value="mock">mock</option>
            </select>
          </Field>
          <Field label="Base URL">
            <input
              value={form.base_url}
              onChange={(e) => setForm({ ...form, base_url: e.target.value })}
              placeholder="http://localhost:1234/v1"
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1"
            />
          </Field>
          <Field label="API key">
            <input
              type="password"
              value={form.api_key}
              onChange={(e) => setForm({ ...form, api_key: e.target.value })}
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1"
            />
          </Field>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={form.trusted_local}
              onChange={(e) =>
                setForm({ ...form, trusted_local: e.target.checked })
              }
            />
            trusted_local (можно отправлять raw)
          </label>
          {err && <div className="text-red-300 text-xs">{err}</div>}
        </div>
        <div className="flex gap-2 mt-6 justify-end">
          <button onClick={onClose} className="px-3 py-1.5 text-sm text-slate-400">
            Отмена
          </button>
          <button
            onClick={() => mut.mutate()}
            disabled={mut.isPending || !form.name || !form.base_url}
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

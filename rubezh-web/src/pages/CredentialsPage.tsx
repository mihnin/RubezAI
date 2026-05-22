import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { KeyRound, ShieldCheck, Trash2, Building2 } from "lucide-react";
import { apiFetch, apiFetchRaw } from "../api/client";
import {
  ModelListSchema,
  UserCredentialListSchema,
  type Model,
  type UserCredential,
} from "../api/schemas";
import { SkeletonRows } from "../components/Skeleton";

/** CredentialsPage (L) — «Мои подключения»: сотрудник подключает свой API-ключ
 *  к облачному провайдеру. Ключ шифруется, в чате используется его ключ. */
export default function CredentialsPage() {
  const qc = useQueryClient();
  const { data: models, isLoading } = useQuery({
    queryKey: ["models"],
    queryFn: () => apiFetch("/api/models", ModelListSchema),
  });
  const { data: creds } = useQuery({
    queryKey: ["my-credentials"],
    queryFn: () => apiFetch("/api/me/credentials", UserCredentialListSchema),
  });

  const cloud = (models ?? []).filter(
    (m: Model) => m.trust_level === "external" && m.is_enabled,
  );
  const byProvider = new Map<string, UserCredential>();
  for (const c of creds ?? []) byProvider.set(c.provider_id, c);

  return (
    <div className="p-8 max-w-3xl">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tight">Мои подключения</h1>
        <p className="text-sm text-slate-500 mt-1 leading-relaxed">
          Подключите свой API-ключ облачного провайдера — запросы в чате пойдут
          под вашим ключом. Это ключ из личного кабинета провайдера
          (platform.openai.com / console.anthropic.com / AI Studio / x.ai), а
          <b className="text-slate-300"> не</b> пароль от ChatGPT/Gemini. ПДн
          фильтруются как обычно; ключ хранится зашифрованным и не показывается.
        </p>
      </header>

      {isLoading && <SkeletonRows count={2} className="h-20" />}
      <div className="space-y-3">
        {cloud.map((m: Model) => (
          <CredentialRow
            key={m.id}
            provider={m}
            cred={byProvider.get(m.id)}
            onChanged={() =>
              qc.invalidateQueries({ queryKey: ["my-credentials"] })
            }
          />
        ))}
        {!isLoading && cloud.length === 0 && (
          <p className="text-sm text-slate-500">
            Нет включённых облачных провайдеров. Обратитесь к администратору.
          </p>
        )}
      </div>
    </div>
  );
}

function CredentialRow({
  provider,
  cred,
  onChanged,
}: {
  provider: Model;
  cred: UserCredential | undefined;
  onChanged: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [key, setKey] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const connected = !!cred;

  const addMut = useMutation({
    mutationFn: () =>
      apiFetchRaw("/api/me/credentials", {
        method: "POST",
        body: JSON.stringify({ provider_id: provider.id, api_key: key }),
      }),
    onSuccess: () => {
      setEditing(false);
      setKey("");
      onChanged();
    },
    onError: (e: Error) => setErr(e.message),
  });
  const delMut = useMutation({
    mutationFn: () =>
      apiFetchRaw(`/api/me/credentials/${cred!.id}`, { method: "DELETE" }),
    onSuccess: onChanged,
  });

  return (
    <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-5">
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="w-9 h-9 rounded-lg bg-cyan-500/10 flex items-center justify-center shrink-0">
            <KeyRound className="w-4 h-4 text-cyan-300" strokeWidth={2} />
          </div>
          <div>
            <h3 className="font-medium">{provider.name}</h3>
            <div className="text-xs mt-0.5 inline-flex items-center gap-1">
              {connected ? (
                <span className="text-emerald-300 inline-flex items-center gap-1">
                  <ShieldCheck className="w-3.5 h-3.5" strokeWidth={2} />
                  подключён мой ключ
                </span>
              ) : (
                <span className="text-slate-500 inline-flex items-center gap-1">
                  <Building2 className="w-3.5 h-3.5" strokeWidth={2} />
                  используется ключ организации
                </span>
              )}
            </div>
          </div>
        </div>
        {connected && (
          <button
            onClick={() => delMut.mutate()}
            disabled={delMut.isPending}
            title="Отключить мой ключ"
            className="text-xs inline-flex items-center gap-1 text-red-400 hover:text-red-300"
          >
            <Trash2 className="w-3.5 h-3.5" strokeWidth={2} /> Отключить
          </button>
        )}
      </div>

      <div className="mt-3 pt-3 border-t border-slate-800">
        {!editing ? (
          <button
            onClick={() => {
              setErr(null);
              setEditing(true);
            }}
            className="text-xs text-cyan-400 hover:text-cyan-300"
          >
            {connected ? "Заменить ключ" : "Подключить свой ключ"}
          </button>
        ) : (
          <div className="flex gap-2 items-center">
            <input
              type="password"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              placeholder="API-ключ провайдера (sk-…)"
              className="flex-1 bg-slate-800 border border-slate-700 rounded px-2 py-1 text-sm"
            />
            <button
              onClick={() => addMut.mutate()}
              disabled={!key || addMut.isPending}
              className="text-xs px-2 py-1 rounded bg-cyan-500 text-slate-950 disabled:opacity-40"
            >
              Сохранить
            </button>
            <button
              onClick={() => {
                setEditing(false);
                setKey("");
              }}
              className="text-xs text-slate-500"
            >
              Отмена
            </button>
          </div>
        )}
        {err && <div className="mt-2 text-xs text-red-300">{err}</div>}
      </div>
    </div>
  );
}

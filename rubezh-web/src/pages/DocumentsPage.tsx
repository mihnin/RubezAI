import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { apiFetch, apiFetchRaw } from "../api/client";
import { DocumentListSchema, type Document as DocItem } from "../api/schemas";

/** DocumentsPage (Итерация 14). docs/design/ui/documents.md. */
export default function DocumentsPage() {
  const qc = useQueryClient();
  const fileRef = useRef<HTMLInputElement>(null);
  const [uploadError, setUploadError] = useState<string | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ["documents"],
    queryFn: () => apiFetch("/api/documents", DocumentListSchema),
    refetchInterval: 5000,
  });

  const uploadMut = useMutation({
    mutationFn: async (file: File) => {
      const fd = new FormData();
      fd.append("file", file);
      await apiFetchRaw("/api/documents", { method: "POST", body: fd });
    },
    onSuccess: () => {
      setUploadError(null);
      qc.invalidateQueries({ queryKey: ["documents"] });
      if (fileRef.current) fileRef.current.value = "";
    },
    onError: (e: Error) => setUploadError(e.message),
  });

  const delMut = useMutation({
    mutationFn: (id: string) =>
      apiFetchRaw(`/api/documents/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["documents"] }),
  });

  return (
    <div className="p-6 max-w-4xl">
      <h1 className="text-xl font-semibold mb-4">Документы</h1>

      <div className="bg-slate-900 border border-slate-700 rounded-lg p-4 mb-6">
        <h2 className="text-sm font-medium mb-2">Загрузить документ</h2>
        <p className="text-xs text-slate-500 mb-3">
          PDF / DOCX, до 50 МБ. Будет обезличен автоматически.
        </p>
        <div className="flex gap-2">
          <input
            ref={fileRef}
            type="file"
            accept=".pdf,.docx"
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) uploadMut.mutate(f);
            }}
            disabled={uploadMut.isPending}
            className="flex-1 text-sm file:mr-3 file:py-2 file:px-3 file:rounded file:border-0 file:bg-cyan-500 file:text-slate-950"
          />
        </div>
        {uploadError && (
          <div className="mt-2 text-sm text-red-300">{uploadError}</div>
        )}
        {uploadMut.isPending && (
          <div className="mt-2 text-sm text-cyan-300">Загрузка…</div>
        )}
      </div>

      <div className="bg-slate-900 border border-slate-700 rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-slate-800 text-slate-400 text-xs uppercase">
            <tr>
              <th className="p-3 text-left">Имя</th>
              <th className="p-3 text-left">Статус</th>
              <th className="p-3 text-right">Размер</th>
              <th className="p-3"></th>
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              <tr>
                <td colSpan={4} className="p-6 text-center text-slate-500">
                  Загрузка…
                </td>
              </tr>
            )}
            {!isLoading && (data?.items?.length ?? 0) === 0 && (
              <tr>
                <td colSpan={4} className="p-6 text-center text-slate-500">
                  Документов пока нет
                </td>
              </tr>
            )}
            {data?.items?.map((d: DocItem) => (
              <tr key={d.id} className="border-t border-slate-800">
                <td className="p-3 truncate max-w-xs">{d.filename}</td>
                <td className="p-3">
                  <StatusBadge status={d.status} />
                </td>
                <td className="p-3 text-right text-slate-400">
                  {(d.size_bytes / 1024).toFixed(1)} KB
                </td>
                <td className="p-3 text-right">
                  <button
                    onClick={() => {
                      if (confirm(`Удалить ${d.filename}?`))
                        delMut.mutate(d.id);
                    }}
                    className="text-red-400 hover:text-red-300 text-xs"
                  >
                    Удалить
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const map: Record<string, string> = {
    pending: "bg-slate-700 text-slate-300",
    processing: "bg-cyan-500/20 text-cyan-300",
    done: "bg-emerald-500/20 text-emerald-300",
    failed: "bg-red-500/20 text-red-300",
  };
  const cls = map[status] ?? "bg-slate-700 text-slate-300";
  return (
    <span className={`px-2 py-0.5 rounded text-xs ${cls}`}>{status}</span>
  );
}

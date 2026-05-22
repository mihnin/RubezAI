import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { FileText, Upload, Trash2, FilePlus, Download, ShieldCheck } from "lucide-react";
import { apiFetch, apiFetchRaw, apiDownload } from "../api/client";
import { DocumentListSchema, type Document as DocItem } from "../api/schemas";
import { SkeletonRows } from "../components/Skeleton";
import { EmptyState } from "../components/EmptyState";

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
    <div className="p-8 max-w-5xl">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tight">Документы</h1>
        <p className="text-sm text-slate-500 mt-1">
          PDF / DOCX обезличиваются автоматически перед индексацией.
        </p>
      </header>

      <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-5 mb-6">
        <div className="flex items-start gap-3">
          <div className="w-9 h-9 rounded-lg bg-cyan-500/15 flex items-center justify-center shrink-0">
            <Upload
              className="w-4 h-4 text-cyan-300"
              strokeWidth={2}
            />
          </div>
          <div className="flex-1">
            <div className="text-sm font-medium mb-1">Загрузить документ</div>
            <p className="text-xs text-slate-500 mb-3">
              PDF / DOCX, до 50&nbsp;МБ. ПДн и секреты будут замаскированы.
            </p>
            <input
              ref={fileRef}
              type="file"
              accept=".pdf,.docx"
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) uploadMut.mutate(f);
              }}
              disabled={uploadMut.isPending}
              className="text-sm text-slate-300 file:mr-3 file:py-1.5 file:px-3 file:rounded-md file:border-0 file:bg-cyan-500 file:text-slate-950 file:font-medium file:cursor-pointer hover:file:bg-cyan-400"
            />
            {uploadError && (
              <div className="mt-2 text-sm text-red-300">{uploadError}</div>
            )}
            {uploadMut.isPending && (
              <div className="mt-2 text-sm text-cyan-300">Загрузка…</div>
            )}
          </div>
        </div>
      </div>

      {isLoading ? (
        <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-4">
          <SkeletonRows count={4} />
        </div>
      ) : (data?.documents?.length ?? 0) === 0 ? (
        <div className="bg-slate-900/60 border border-slate-800 rounded-xl">
          <EmptyState
            icon={FilePlus}
            title="Документов пока нет"
            hint="Загрузите PDF или DOCX — он будет проиндексирован и доступен для RAG-поиска."
          />
        </div>
      ) : (
        <div className="bg-slate-900/60 border border-slate-800 rounded-xl overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-slate-900 text-slate-400 text-xs uppercase tracking-wider">
              <tr>
                <th className="p-3 text-left font-medium">Имя</th>
                <th className="p-3 text-left font-medium">Статус</th>
                <th className="p-3 text-right font-medium">Размер</th>
                <th className="p-3"></th>
              </tr>
            </thead>
            <tbody>
              {data?.documents?.map((d: DocItem) => (
                <tr
                  key={d.id}
                  className="border-t border-slate-800 hover:bg-slate-800/30"
                >
                  <td className="p-3 truncate max-w-xs">
                    <span className="inline-flex items-center gap-2">
                      <FileText
                        className="w-3.5 h-3.5 text-slate-500 shrink-0"
                        strokeWidth={2}
                      />
                      {d.filename}
                    </span>
                  </td>
                  <td className="p-3">
                    <StatusBadge status={d.status} />
                  </td>
                  <td className="p-3 text-right text-slate-400 tabular-nums">
                    {d.size_bytes !== null
                      ? `${(d.size_bytes / 1024).toFixed(1)} KB`
                      : "—"}
                  </td>
                  <td className="p-3 text-right">
                    <div className="inline-flex items-center gap-1">
                      {d.status === "done" && (
                        <>
                          <button
                            onClick={() =>
                              apiDownload(
                                `/api/documents/${d.id}/download`,
                                d.filename,
                              )
                            }
                            title="Скачать оригинал"
                            className="inline-flex items-center justify-center w-7 h-7 rounded text-slate-500 hover:text-cyan-300 hover:bg-cyan-500/10 transition"
                          >
                            <Download className="w-3.5 h-3.5" strokeWidth={2} />
                          </button>
                          <button
                            onClick={() =>
                              apiDownload(
                                `/api/documents/${d.id}/masked`,
                                `${d.filename}-masked.txt`,
                              )
                            }
                            title="Скачать обезличенный (.txt)"
                            className="inline-flex items-center justify-center w-7 h-7 rounded text-slate-500 hover:text-emerald-300 hover:bg-emerald-500/10 transition"
                          >
                            <ShieldCheck className="w-3.5 h-3.5" strokeWidth={2} />
                          </button>
                        </>
                      )}
                      <button
                        onClick={() => {
                          if (confirm(`Удалить ${d.filename}?`))
                            delMut.mutate(d.id);
                        }}
                        title="Удалить"
                        className="inline-flex items-center justify-center w-7 h-7 rounded text-slate-500 hover:text-red-400 hover:bg-red-500/10 transition"
                      >
                        <Trash2 className="w-3.5 h-3.5" strokeWidth={2} />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const map: Record<string, { cls: string; label: string }> = {
    pending: { cls: "bg-slate-700/50 text-slate-300", label: "ожидает" },
    processing: { cls: "bg-cyan-500/20 text-cyan-300 animate-pulse", label: "обработка" },
    done: { cls: "bg-emerald-500/20 text-emerald-300", label: "готов" },
    failed: { cls: "bg-red-500/20 text-red-300", label: "ошибка" },
  };
  const m = map[status] ?? { cls: "bg-slate-700/50 text-slate-300", label: status };
  return (
    <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${m.cls}`}>
      {m.label}
    </span>
  );
}

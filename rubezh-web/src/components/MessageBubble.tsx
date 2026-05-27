import { useEffect, useRef, useState } from "react";
import {
  ShieldAlert,
  Eye,
  Copy,
  Check,
  BookOpen,
  Download,
  Paperclip,
  Activity,
  CheckCircle2,
  Circle,
  Loader2,
  ChevronDown,
  ChevronRight,
} from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { ChatStatusPayload, RagHitMeta } from "../api/schemas";

export type ChatStatusView = ChatStatusPayload & {
  receivedAt?: number;
};

export interface Message {
  role: "user" | "assistant";
  content: string;
  decision?: string;
  reasons?: string[];
  id?: string; // id сообщения ассистента (из SSE done) — для reveal (J.2)
  revealed?: boolean; // раскрыты ли реальные данные
  // RAG-источники: метаданные чанков, попавших в LLM-context (Итерация 11 §Р4).
  ragHits?: RagHitMeta[];
  // statusEvents — live-телеметрия SSE event: status (policy/RAG/remote CLI/audit).
  statusEvents?: ChatStatusView[];
}

// FileAttachment — отдельная download-кнопка, вытащенная из текста ответа.
// Контракт строки: `[📎 имя](data:mime;base64,...)` — формирует adapter
// ssh_cli в rubezh-api (internal/llm/ssh_cli.go::appendFilesToContent).
export interface FileAttachment {
  name: string;
  mime: string;
  dataUrl: string;
}

// Регексп под Markdown-link с data:-URL внутри блока «📎 Файлы:» — точнее,
// просто любой `[📎 имя](data:...)` где-либо в content. Жадный mime/base64
// до закрывающей скобки. Multi-line флаг не нужен — base64 в одной строке.
const FILE_LINK_RE =
  /\[📎\s*([^\]]+)\]\(data:([^;]+);base64,([A-Za-z0-9+/=]+)\)/g;

// extractFileAttachments вытаскивает все file-ссылки И возвращает «чистый»
// текст без них (включая удаление лидирующего «📎 Файлы:» если он остался
// один). Сделано чисто-функционально, без сайд-эффектов.
export function extractFileAttachments(content: string): {
  stripped: string;
  files: FileAttachment[];
} {
  if (!content || !content.includes("data:")) {
    return { stripped: content, files: [] };
  }
  const matches = Array.from(content.matchAll(FILE_LINK_RE));
  if (matches.length === 0) {
    return { stripped: content, files: [] };
  }
  const files: FileAttachment[] = matches.map((m) => ({
    name: m[1].trim(),
    mime: m[2].trim() || "application/octet-stream",
    dataUrl: `data:${m[2]};base64,${m[3]}`,
  }));
  let stripped = content.replace(FILE_LINK_RE, "");
  // После вырезания ссылки часто остаётся пустой list-маркер `- ` в строке.
  stripped = stripped.replace(/(^|\n)[*\-+]\s*(?=\n|$)/g, "$1");
  // «📎 Файлы:» без элементов — мусор.
  stripped = stripped
    .replace(/\n?📎\s*Файлы:?\s*\n?/g, "\n")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
  return { stripped, files };
}

export function MessageBubble({
  message,
  streaming,
  onReveal,
}: {
  message: Message;
  streaming: boolean;
  onReveal?: () => void;
}) {
  const isUser = message.role === "user";
  const [copied, setCopied] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  // attachments: data:-ссылки вырезаются из текста и рендерятся как
  // download-кнопки. У user-сообщений не парсим (там не должно быть).
  const { stripped, files } = isUser
    ? { stripped: message.content, files: [] as FileAttachment[] }
    : extractFileAttachments(message.content);
  const statusEvents = message.statusEvents ?? [];
  const latestStatus = statusEvents[statusEvents.length - 1];
  const [statusExpanded, setStatusExpanded] = useState(() => streaming);
  const previousStreaming = useRef(streaming);
  useEffect(() => {
    if (!streaming || statusEvents.length === 0) return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [streaming, statusEvents.length]);
  useEffect(() => {
    if (statusEvents.length === 0) {
      previousStreaming.current = streaming;
      return;
    }
    if (previousStreaming.current && !streaming) {
      setStatusExpanded(false);
    } else if (!previousStreaming.current && streaming) {
      setStatusExpanded(true);
    }
    previousStreaming.current = streaming;
  }, [streaming, statusEvents.length]);
  // J.2: раскрытие доступно для записанного masked-ответа, ещё не раскрытого
  const canReveal =
    !isUser &&
    !!message.id &&
    message.decision === "allow_masked" &&
    !message.revealed;
  const canCopy = !isUser && !!stripped && !streaming;
  const decisionBad =
    message.decision &&
    (message.decision === "deny" || message.decision === "escalate");
  const decisionWarn = message.decision && message.decision === "allow_masked";

  async function copy() {
    try {
      await navigator.clipboard.writeText(stripped);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard может быть недоступен (не-https) — тихо игнорируем */
    }
  }

  return (
    <div className={`max-w-3xl ${isUser ? "ml-auto" : ""}`}>
      <div
        className={`px-4 py-3 rounded-2xl ${
          isUser
            ? "bg-cyan-500/15 border border-cyan-500/30 rounded-br-md"
            : "bg-slate-800/40 border border-slate-700/60 rounded-bl-md"
        }`}
      >
        {isUser ? (
          <div className="whitespace-pre-wrap text-sm leading-relaxed text-slate-100">
            {message.content}
          </div>
        ) : (
          <div className="md-body text-sm leading-relaxed text-slate-100">
            {stripped ? (
              <ReactMarkdown remarkPlugins={[remarkGfm]}>
                {stripped}
              </ReactMarkdown>
            ) : files.length === 0 ? (
              streaming &&
              (latestStatus ? (
                <div className="inline-flex items-center gap-2 text-slate-200">
                  <Loader2
                    className="w-4 h-4 text-cyan-300 animate-spin"
                    strokeWidth={2}
                  />
                  <span>В работе: {statusTitle(latestStatus.stage)}</span>
                </div>
              ) : (
                "…"
              ))
            ) : null}
          </div>
        )}

        {!isUser && statusEvents.length > 0 && (
          <div
            className="mt-2.5 pt-2 border-t border-slate-700/50 text-xs"
            data-testid="message-status"
            aria-live={streaming ? "polite" : "off"}
          >
            <div className="flex flex-wrap items-center justify-between gap-2 text-slate-400 mb-2">
              <button
                type="button"
                onClick={() => setStatusExpanded((v) => !v)}
                aria-expanded={statusExpanded}
                aria-label={
                  statusExpanded
                    ? "Свернуть ход выполнения"
                    : "Развернуть ход выполнения"
                }
                title={
                  statusExpanded
                    ? "Свернуть ход выполнения"
                    : "Развернуть ход выполнения"
                }
                className="inline-flex items-center gap-1.5 rounded-md text-slate-400 transition hover:text-cyan-200 focus:outline-none focus:ring-2 focus:ring-cyan-400/40 focus:ring-offset-2 focus:ring-offset-slate-900"
              >
                {statusExpanded ? (
                  <ChevronDown className="h-3.5 w-3.5" strokeWidth={2} />
                ) : (
                  <ChevronRight className="h-3.5 w-3.5" strokeWidth={2} />
                )}
                <Activity
                  className={`w-3.5 h-3.5 ${
                    streaming
                      ? "text-cyan-300 animate-pulse"
                      : "text-emerald-300"
                  }`}
                  strokeWidth={2}
                />
                <span>Ход выполнения</span>
              </button>
              <span className="font-mono text-[0.68rem] text-slate-500">
                шаг {statusEvents.length} ·{" "}
                {totalStatusTime(statusEvents, now, streaming)}
              </span>
            </div>

            {!statusExpanded && latestStatus && (
              <div className="rounded-md border border-slate-700/70 bg-slate-900/35 px-3 py-2">
                <div className="flex flex-wrap items-center gap-2">
                  <span
                    className={`inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[0.68rem] font-medium ${
                      streaming
                        ? "border-cyan-400/40 bg-cyan-400/10 text-cyan-200"
                        : "border-emerald-400/40 bg-emerald-400/10 text-emerald-200"
                    }`}
                  >
                    {streaming ? (
                      <Loader2 className="w-3 h-3 animate-spin" />
                    ) : (
                      <CheckCircle2 className="w-3 h-3" />
                    )}
                    {streaming ? "идёт" : "готово"}
                  </span>
                  <span className="font-medium text-slate-100">
                    {statusTitle(latestStatus.stage)}
                  </span>
                  <span className="min-w-0 flex-1 truncate text-slate-400">
                    {latestStatus.message}
                  </span>
                </div>
              </div>
            )}

            {statusExpanded && latestStatus && (
              <div className="mb-2 rounded-md border border-cyan-500/25 bg-cyan-500/10 px-3 py-2">
                <div className="flex flex-wrap items-center gap-2">
                  <span
                    className={`inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[0.68rem] font-medium ${
                      streaming
                        ? "border-cyan-400/40 bg-cyan-400/10 text-cyan-200"
                        : "border-emerald-400/40 bg-emerald-400/10 text-emerald-200"
                    }`}
                  >
                    {streaming ? (
                      <Loader2 className="w-3 h-3 animate-spin" />
                    ) : (
                      <CheckCircle2 className="w-3 h-3" />
                    )}
                    {streaming ? "сейчас" : "последний этап"}
                  </span>
                  <span className="font-medium text-slate-100">
                    {statusTitle(latestStatus.stage)}
                  </span>
                  <span className="font-mono text-[0.68rem] text-slate-500">
                    {latestStatus.stage}
                  </span>
                </div>
                <div className="mt-1 text-slate-300 leading-snug">
                  {latestStatus.message}
                </div>
              </div>
            )}

            {statusExpanded && (
              <div className="space-y-1.5" data-testid="message-status-steps">
                {statusEvents.map((s, idx) => {
                  const active = streaming && idx === statusEvents.length - 1;
                  const done = !active;
                  const next = statusEvents[idx + 1];
                  const elapsed = statusDuration(s, next, active ? now : null);
                  return (
                    <div
                      key={`${s.request_id}:${s.stage}:${idx}`}
                      className="grid grid-cols-[1.25rem_1fr_auto] items-start gap-2 text-slate-300"
                    >
                      <span className="relative flex h-full min-h-8 justify-center">
                        {idx < statusEvents.length - 1 && (
                          <span className="absolute top-4 bottom-[-0.45rem] w-px bg-slate-700" />
                        )}
                        <span
                          className={`relative mt-0.5 inline-flex h-4 w-4 items-center justify-center rounded-full ${
                            active
                              ? "bg-cyan-300/15 text-cyan-200 ring-1 ring-cyan-300/50"
                              : done
                                ? "bg-emerald-300/10 text-emerald-300"
                                : "bg-slate-700 text-slate-500"
                          }`}
                        >
                          {active ? (
                            <Loader2 className="h-3 w-3 animate-spin" />
                          ) : done ? (
                            <CheckCircle2 className="h-3 w-3" />
                          ) : (
                            <Circle className="h-2.5 w-2.5" />
                          )}
                        </span>
                      </span>
                      <span className="min-w-0">
                        <span className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                          <span className="font-medium text-slate-200">
                            {idx + 1}. {statusTitle(s.stage)}
                          </span>
                          <span className="font-mono text-[0.68rem] text-slate-500">
                            {s.stage}
                          </span>
                        </span>
                        <span className="block leading-snug text-slate-400">
                          {s.message}
                        </span>
                        {(s.provider || s.model) && (
                          <span className="mt-0.5 block truncate font-mono text-[0.68rem] text-slate-600">
                            {s.provider}
                            {s.model ? ` · ${s.model}` : ""}
                          </span>
                        )}
                      </span>
                      <span
                        className={`mt-0.5 whitespace-nowrap rounded-md border px-1.5 py-0.5 text-[0.68rem] ${
                          active
                            ? "border-cyan-400/30 bg-cyan-400/10 text-cyan-200"
                            : "border-emerald-400/20 bg-emerald-400/5 text-emerald-300"
                        }`}
                      >
                        {active ? "идёт" : "готово"}
                        {elapsed ? ` · ${elapsed}` : ""}
                      </span>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}

        {files.length > 0 && (
          <div
            className="mt-2.5 pt-2 border-t border-slate-700/50"
            data-testid="message-attachments"
          >
            <div className="flex items-center gap-1.5 text-xs text-slate-400 mb-1.5">
              <Paperclip className="w-3.5 h-3.5" strokeWidth={2} />
              <span>Файлы от модели:</span>
            </div>
            <div className="flex flex-wrap gap-1.5">
              {files.map((f, idx) => (
                <a
                  key={`${f.name}-${idx}`}
                  href={f.dataUrl}
                  download={f.name}
                  className="inline-flex items-center gap-1.5 bg-slate-800/70 border border-slate-700 hover:border-cyan-500/60 rounded-md px-2.5 py-1 text-xs text-slate-200 transition"
                  title={`${f.name} · ${f.mime}`}
                >
                  <Download className="w-3.5 h-3.5" strokeWidth={2} />
                  <span className="truncate max-w-[16rem]">{f.name}</span>
                </a>
              ))}
            </div>
          </div>
        )}

        {(canCopy || canReveal || message.revealed) && (
          <div className="mt-2.5 flex items-center gap-3">
            {canCopy && (
              <button
                onClick={copy}
                aria-label="Скопировать ответ"
                title="Скопировать ответ"
                className="text-xs inline-flex items-center gap-1 text-slate-400 hover:text-cyan-300"
              >
                {copied ? (
                  <>
                    <Check className="w-3.5 h-3.5" strokeWidth={2} />{" "}
                    скопировано
                  </>
                ) : (
                  <>
                    <Copy className="w-3.5 h-3.5" strokeWidth={2} /> Копировать
                  </>
                )}
              </button>
            )}
            {canReveal && (
              <button
                onClick={onReveal}
                className="text-xs inline-flex items-center gap-1 text-cyan-400 hover:text-cyan-300"
              >
                <Eye className="w-3.5 h-3.5" strokeWidth={2} />
                Показать реальные данные
              </button>
            )}
            {message.revealed && (
              <span className="text-xs inline-flex items-center gap-1 text-emerald-300">
                <Eye className="w-3.5 h-3.5" strokeWidth={2} />
                раскрыто · записано в аудит
              </span>
            )}
          </div>
        )}

        {!isUser && message.ragHits && message.ragHits.length > 0 && (
          <div
            className="mt-2.5 pt-2 border-t border-slate-700/50 text-xs"
            data-testid="rag-sources"
          >
            <div className="flex items-center gap-1.5 text-slate-400 mb-1.5">
              <BookOpen className="w-3.5 h-3.5" strokeWidth={2} />
              <span>Источники:</span>
            </div>
            <div className="flex flex-wrap gap-1.5">
              {message.ragHits.map((h, idx) => (
                <span
                  key={`${h.document_id}:${h.chunk_index}:${idx}`}
                  className="inline-flex items-center gap-1 bg-slate-800/70 border border-slate-700 rounded-md px-2 py-0.5 text-slate-300"
                  title={`${h.filename} · фрагмент #${h.chunk_index} · релевантность ${Math.round(h.relevance * 100)}%`}
                >
                  <span className="truncate max-w-[12rem]">{h.filename}</span>
                  <span className="text-slate-500">
                    · {Math.round(h.relevance * 100)}%
                  </span>
                </span>
              ))}
            </div>
          </div>
        )}

        {message.decision && message.decision !== "allow_raw" && (
          <div
            className={`mt-2.5 pt-2 border-t border-slate-700/50 text-xs flex items-start gap-1.5 ${
              decisionBad
                ? "text-red-300"
                : decisionWarn
                  ? "text-amber-300"
                  : "text-slate-400"
            }`}
            data-testid="decision-banner"
          >
            <ShieldAlert
              className="w-3.5 h-3.5 mt-0.5 shrink-0"
              strokeWidth={2}
            />
            <div>
              <strong>{message.decision}</strong>
              {message.reasons && message.reasons.length > 0 && (
                <span className="text-slate-500">
                  {" "}
                  · {message.reasons.join(", ")}
                </span>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

const STATUS_TITLES: Record<string, string> = {
  client_prepare: "Подготовка запроса",
  sse_connect: "Подключение к SSE",
  policy_checked: "Проверка политики",
  rag_search: "Поиск по документам",
  rag_done: "RAG завершён",
  policy_revised: "Политика пересчитана",
  blocked: "Остановлено политикой",
  llm_call: "Вызов основной модели",
  llm_done: "Черновик получен",
  review_started: "Ревизия запущена",
  review_round: "Раунд ревизии",
  review_call: "Вызов модели-ревизора",
  review_done: "Ревизор завершил проверку",
  review_revise: "Правки основной модели",
  review_revised: "Черновик доработан",
  review_complete: "Ревизия завершена",
  review_fallback: "Последний вариант",
  review_failed: "Ревизия не принята",
  streaming_answer: "Передача ответа",
  done: "Ответ доставлен",
};

function statusTitle(stage: string): string {
  return STATUS_TITLES[stage] ?? stage.replaceAll("_", " ");
}

function statusDuration(
  current: ChatStatusView,
  next: ChatStatusView | undefined,
  now: number | null,
): string {
  if (typeof current.receivedAt !== "number") return "";
  const end = next?.receivedAt ?? now;
  if (!end || end < current.receivedAt) return "";
  return formatElapsed(end - current.receivedAt);
}

function totalStatusTime(
  statuses: ChatStatusView[],
  now: number,
  streaming: boolean,
): string {
  const first = statuses.find((s) => typeof s.receivedAt === "number");
  if (typeof first?.receivedAt !== "number") return "время не замерено";
  const last = statuses[statuses.length - 1];
  const end =
    streaming || typeof last?.receivedAt !== "number" ? now : last.receivedAt;
  return formatElapsed(Math.max(0, end - first.receivedAt));
}

function formatElapsed(ms: number): string {
  const seconds = Math.max(0, Math.round(ms / 1000));
  if (seconds < 1) return "<1с";
  if (seconds < 60) return `${seconds}с`;
  const minutes = Math.floor(seconds / 60);
  const rest = String(seconds % 60).padStart(2, "0");
  return `${minutes}м ${rest}с`;
}

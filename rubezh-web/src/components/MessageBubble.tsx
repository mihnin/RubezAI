import { useState } from "react";
import { ShieldAlert, Eye, Copy, Check } from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

export interface Message {
  role: "user" | "assistant";
  content: string;
  decision?: string;
  reasons?: string[];
  id?: string; // id сообщения ассистента (из SSE done) — для reveal (J.2)
  revealed?: boolean; // раскрыты ли реальные данные
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
  // J.2: раскрытие доступно для записанного masked-ответа, ещё не раскрытого
  const canReveal =
    !isUser &&
    !!message.id &&
    message.decision === "allow_masked" &&
    !message.revealed;
  const canCopy = !isUser && !!message.content && !streaming;
  const decisionBad =
    message.decision &&
    (message.decision === "deny" || message.decision === "escalate");
  const decisionWarn = message.decision && message.decision === "allow_masked";

  async function copy() {
    try {
      await navigator.clipboard.writeText(message.content);
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
            {message.content ? (
              <ReactMarkdown remarkPlugins={[remarkGfm]}>
                {message.content}
              </ReactMarkdown>
            ) : (
              streaming && "…"
            )}
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
                    <Check className="w-3.5 h-3.5" strokeWidth={2} /> скопировано
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
            <ShieldAlert className="w-3.5 h-3.5 mt-0.5 shrink-0" strokeWidth={2} />
            <div>
              <strong>{message.decision}</strong>
              {message.reasons && message.reasons.length > 0 && (
                <span className="text-slate-500"> · {message.reasons.join(", ")}</span>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

import { useState, useRef, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { streamChat } from "../api/sse";
import { apiFetch } from "../api/client";
import { ModelListSchema, type ChatEvent } from "../api/schemas";

interface Message {
  role: "user" | "assistant";
  content: string;
  decision?: string;
  reasons?: string[];
}

/** ChatPage (Итерация 13).
 *  Реальный SSE-формат — event: meta/delta/done/error (RFC 6202),
 *  см. rubezh-api/internal/api/chat.go и chat.schema.json. */
export default function ChatPage() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const [sessionId] = useState<string>(() => crypto.randomUUID());
  const [streaming, setStreaming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const abortRef = useRef<AbortController | null>(null);

  // Выбираем первого активного провайдера автоматически (MVP). После MVP —
  // селектор в UI с памятью выбора в localStorage.
  const { data: models } = useQuery({
    queryKey: ["models"],
    queryFn: () => apiFetch("/api/models", ModelListSchema),
  });
  const activeProvider = models?.find((m) => m.is_enabled);

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight });
  }, [messages]);

  useEffect(
    () => () => {
      abortRef.current?.abort();
    },
    [],
  );

  async function send() {
    if (!input.trim() || streaming) return;
    if (!activeProvider) {
      setError(
        "Нет активных LLM-провайдеров. Откройте раздел «Модели» и создайте провайдера.",
      );
      return;
    }
    const userMsg: Message = { role: "user", content: input };
    setMessages((m) => [...m, userMsg, { role: "assistant", content: "" }]);
    setInput("");
    setStreaming(true);
    setError(null);

    const ctrl = new AbortController();
    abortRef.current = ctrl;
    try {
      await streamChat({
        sessionId,
        message: userMsg.content,
        provider: activeProvider.name,
        model: activeProvider.name,
        signal: ctrl.signal,
        onEvent: (ev) => applyEvent(ev, setMessages, setError),
      });
    } catch (e) {
      if ((e as Error).name !== "AbortError") {
        setError(e instanceof Error ? e.message : "Сетевая ошибка");
      }
    } finally {
      setStreaming(false);
      abortRef.current = null;
    }
  }

  return (
    <div className="h-screen flex flex-col">
      <header className="border-b border-slate-800 p-4">
        <h1 className="text-lg font-semibold">Чат</h1>
        <p className="text-xs text-slate-500">
          Сессия: <span data-testid="session-id">{sessionId.slice(0, 8)}</span>
          {activeProvider && (
            <span className="ml-3">
              Провайдер: <strong>{activeProvider.name}</strong>
            </span>
          )}
        </p>
      </header>
      <div ref={scrollRef} className="flex-1 overflow-auto p-4 space-y-3">
        {messages.length === 0 && (
          <div className="text-slate-500 text-center mt-12">
            Задайте вопрос. ПДн и секреты будут обезличены автоматически.
          </div>
        )}
        {messages.map((m, i) => (
          <MessageBubble key={i} message={m} />
        ))}
      </div>
      {error && (
        <div
          role="alert"
          className="bg-red-900/30 border-t border-red-700 p-2 text-sm text-red-200"
        >
          {error}
        </div>
      )}
      <footer className="border-t border-slate-800 p-4 flex gap-2">
        <textarea
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              send();
            }
          }}
          disabled={streaming}
          rows={2}
          aria-label="Сообщение"
          className="flex-1 bg-slate-900 border border-slate-700 rounded p-2 text-sm resize-none"
          placeholder="Введите сообщение (Enter — отправить)…"
        />
        <button
          onClick={send}
          disabled={streaming || !input.trim() || !activeProvider}
          className="px-4 rounded bg-cyan-500 hover:bg-cyan-400 text-slate-950 font-medium disabled:opacity-40"
        >
          {streaming ? "…" : "→"}
        </button>
      </footer>
    </div>
  );
}

function applyEvent(
  ev: ChatEvent,
  setMessages: React.Dispatch<React.SetStateAction<Message[]>>,
  setError: React.Dispatch<React.SetStateAction<string | null>>,
): void {
  if (ev.type === "delta") {
    setMessages((m) => appendDelta(m, ev.payload.content));
  } else if (ev.type === "meta") {
    setMessages((m) =>
      annotateLast(m, ev.payload.decision, ev.payload.reasons),
    );
  } else if (ev.type === "error") {
    setError(`${ev.payload.message} (req=${ev.payload.request_id})`);
  }
}

function appendDelta(list: Message[], delta: string): Message[] {
  const last = list[list.length - 1];
  if (!last || last.role !== "assistant") return list;
  return [...list.slice(0, -1), { ...last, content: last.content + delta }];
}

function annotateLast(
  list: Message[],
  decision: string,
  reasons: string[],
): Message[] {
  const last = list[list.length - 1];
  if (!last) return list;
  return [...list.slice(0, -1), { ...last, decision, reasons }];
}

function MessageBubble({ message }: { message: Message }) {
  const isUser = message.role === "user";
  return (
    <div className={`max-w-2xl ${isUser ? "ml-auto" : ""}`}>
      <div
        className={`p-3 rounded-lg ${
          isUser
            ? "bg-cyan-500/15 border border-cyan-500/30"
            : "bg-slate-800/50 border border-slate-700"
        }`}
      >
        <div className="whitespace-pre-wrap text-sm">
          {message.content || "…"}
        </div>
        {message.decision && message.decision !== "allow_raw" && (
          <div
            className="mt-2 text-xs text-amber-300"
            data-testid="decision-banner"
          >
            ⚠ Решение: <strong>{message.decision}</strong>
            {message.reasons && message.reasons.length > 0 && (
              <span> · {message.reasons.join(", ")}</span>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

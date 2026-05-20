import { useState, useRef, useEffect, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Send,
  ShieldAlert,
  Sparkles,
  AlertCircle,
  ChevronDown,
} from "lucide-react";
import { streamChat } from "../api/sse";
import { apiFetch } from "../api/client";
import {
  ModelListSchema,
  ChatSessionSchema,
  type ChatEvent,
  type Model,
} from "../api/schemas";

interface Message {
  role: "user" | "assistant";
  content: string;
  decision?: string;
  reasons?: string[];
}

const STORAGE_PROVIDER = "rubezh.chat.provider";
const STORAGE_MODEL = "rubezh.chat.model";

/** ChatPage. Реальный SSE — event: meta/delta/done/error (RFC 6202).
 *  Switcher провайдер+модель (state в localStorage). */
export default function ChatPage() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  // session_id создаётся при первом send (lazy) через POST /api/chat/sessions.
  // Backend требует существующую запись в chat_sessions, локальный UUID не годится.
  const [sessionId, setSessionId] = useState<string>("");
  const [streaming, setStreaming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const abortRef = useRef<AbortController | null>(null);

  const { data: models } = useQuery({
    queryKey: ["models"],
    queryFn: () => apiFetch("/api/models", ModelListSchema),
  });

  const enabled = useMemo(
    () => models?.filter((m) => m.is_enabled) ?? [],
    [models],
  );

  const [providerName, setProviderName] = useState<string>(
    () => localStorage.getItem(STORAGE_PROVIDER) ?? "",
  );
  const [modelName, setModelName] = useState<string>(
    () => localStorage.getItem(STORAGE_MODEL) ?? "",
  );

  // Если ещё ничего не выбрано или сохранённый провайдер пропал — берём первый.
  useEffect(() => {
    if (enabled.length === 0) return;
    const found = enabled.find((m) => m.name === providerName);
    if (!found) {
      const first = enabled[0];
      setProviderName(first.name);
      setModelName(defaultModelFor(first));
    }
  }, [enabled, providerName]);

  useEffect(() => {
    if (providerName) localStorage.setItem(STORAGE_PROVIDER, providerName);
    if (modelName) localStorage.setItem(STORAGE_MODEL, modelName);
  }, [providerName, modelName]);

  const activeProvider = enabled.find((m) => m.name === providerName);

  useEffect(() => {
    scrollRef.current?.scrollTo({
      top: scrollRef.current.scrollHeight,
      behavior: "smooth",
    });
  }, [messages]);

  useEffect(() => () => abortRef.current?.abort(), []);

  async function send() {
    if (!input.trim() || streaming) return;
    if (!activeProvider) {
      setError(
        "Нет активных LLM-провайдеров. Откройте «Модели» и создайте провайдера.",
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
      // Lazy-создание сессии при первом сообщении — backend ожидает
      // существующую запись в chat_sessions (FK), локальный UUID не годится.
      let sid = sessionId;
      if (!sid) {
        const created = await apiFetch(
          "/api/chat/sessions",
          ChatSessionSchema,
          {
            method: "POST",
            body: JSON.stringify({ title: "web-ui" }),
          },
        );
        sid = created.id;
        setSessionId(sid);
      }
      await streamChat({
        sessionId: sid,
        message: userMsg.content,
        provider: activeProvider.name,
        model: modelName || activeProvider.name,
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
      <header className="border-b border-slate-800 px-6 py-4 flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold tracking-tight">Чат</h1>
          <p className="text-xs text-slate-500">
            Сессия{" "}
            <span
              className="font-mono text-slate-400"
              data-testid="session-id"
            >
              {sessionId ? sessionId.slice(0, 8) : "—"}
            </span>
          </p>
        </div>
        <ProviderModelPicker
          providers={enabled}
          providerName={providerName}
          modelName={modelName}
          onProviderChange={(p) => {
            setProviderName(p.name);
            setModelName(defaultModelFor(p));
          }}
          onModelChange={setModelName}
        />
      </header>
      <div
        ref={scrollRef}
        className="flex-1 overflow-auto p-6 space-y-3 bg-gradient-to-b from-slate-950 to-slate-950/60"
      >
        {messages.length === 0 && (
          <div className="text-center mt-16 max-w-md mx-auto">
            <div className="inline-flex w-12 h-12 rounded-full bg-cyan-500/10 items-center justify-center mb-4">
              <Sparkles
                className="w-6 h-6 text-cyan-300"
                strokeWidth={1.5}
              />
            </div>
            <div className="text-slate-300 font-medium mb-2">
              Готов к работе
            </div>
            <p className="text-sm text-slate-500">
              Задайте вопрос. ПДн, секреты и коммерческая тайна будут
              автоматически замаскированы перед отправкой в LLM.
            </p>
          </div>
        )}
        {messages.map((m, i) => (
          <MessageBubble
            key={i}
            message={m}
            streaming={streaming && i === messages.length - 1}
          />
        ))}
      </div>
      {error && (
        <div
          role="alert"
          className="bg-red-900/30 border-t border-red-800/60 px-4 py-2.5 text-sm text-red-200 flex items-center gap-2"
        >
          <AlertCircle className="w-4 h-4 shrink-0" strokeWidth={2} />
          {error}
        </div>
      )}
      <footer className="border-t border-slate-800 p-4 bg-slate-950">
        <div className="flex gap-2 max-w-4xl mx-auto">
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
            className="flex-1 bg-slate-900 border border-slate-800 rounded-lg p-3 text-sm resize-none focus:outline-none focus:border-cyan-500 focus:ring-2 focus:ring-cyan-500/20 transition placeholder:text-slate-600"
            placeholder="Введите сообщение… (Enter — отправить, Shift+Enter — новая строка)"
          />
          <button
            onClick={send}
            disabled={streaming || !input.trim() || !activeProvider}
            className="px-4 self-stretch rounded-lg bg-cyan-500 hover:bg-cyan-400 text-slate-950 font-medium disabled:opacity-40 disabled:cursor-not-allowed transition shadow-lg shadow-cyan-500/20 flex items-center gap-1.5"
          >
            {streaming ? (
              <Sparkles className="w-4 h-4 animate-spin" strokeWidth={2.5} />
            ) : (
              <Send className="w-4 h-4" strokeWidth={2.5} />
            )}
          </button>
        </div>
      </footer>
    </div>
  );
}

function defaultModelFor(p: Model): string {
  // Heuristics: для openai_compatible имя модели часто совпадает с именем
  // провайдера, но для LM Studio это имя загруженной модели (другое).
  // Стартуем с имени провайдера, пользователь правит в picker'е.
  return p.adapter === "openai_compatible" && p.name.startsWith("deepseek")
    ? "deepseek-r1-distill-qwen-7b"
    : p.name;
}

interface PickerProps {
  providers: Model[];
  providerName: string;
  modelName: string;
  onProviderChange: (p: Model) => void;
  onModelChange: (m: string) => void;
}

function ProviderModelPicker({
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
  return (
    <div className="relative">
      <button
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-2 text-xs bg-slate-900/60 border border-slate-800 rounded-full px-3 py-1.5 hover:border-slate-700 transition"
      >
        <Sparkles className="w-3.5 h-3.5 text-cyan-400" strokeWidth={2} />
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
          <div className="text-[10px] uppercase tracking-wider text-slate-500 mb-1.5">
            Провайдер
          </div>
          <div className="space-y-1 mb-3">
            {providers.map((p) => (
              <button
                key={p.id}
                onClick={() => {
                  onProviderChange(p);
                  setOpen(false);
                }}
                className={`w-full text-left px-2.5 py-1.5 rounded text-xs ${
                  p.name === providerName
                    ? "bg-cyan-500/15 text-cyan-300"
                    : "hover:bg-slate-800/60 text-slate-300"
                }`}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium">{p.name}</span>
                  <span className="text-[10px] text-slate-500">
                    {p.trust_level}
                  </span>
                </div>
                <div className="font-mono text-[10px] text-slate-600 truncate">
                  {p.endpoint}
                </div>
              </button>
            ))}
          </div>
          <div className="text-[10px] uppercase tracking-wider text-slate-500 mb-1.5">
            Модель
          </div>
          <input
            value={modelName}
            onChange={(e) => onModelChange(e.target.value)}
            placeholder="например: deepseek-r1-distill-qwen-7b"
            className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-xs font-mono focus:outline-none focus:border-cyan-500"
          />
          <p className="text-[10px] text-slate-500 mt-1.5 leading-relaxed">
            Имя модели передаётся как поле <code>model</code> в OpenAI-
            совместимый endpoint провайдера. Сохраняется в&nbsp;localStorage.
          </p>
        </div>
      )}
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

function MessageBubble({
  message,
  streaming,
}: {
  message: Message;
  streaming: boolean;
}) {
  const isUser = message.role === "user";
  const decisionBad =
    message.decision &&
    (message.decision === "deny" || message.decision === "escalate");
  const decisionWarn =
    message.decision && message.decision === "allow_masked";
  return (
    <div className={`max-w-3xl ${isUser ? "ml-auto" : ""}`}>
      <div
        className={`px-4 py-3 rounded-2xl ${
          isUser
            ? "bg-cyan-500/15 border border-cyan-500/30 rounded-br-md"
            : "bg-slate-800/40 border border-slate-700/60 rounded-bl-md"
        }`}
      >
        <div className="whitespace-pre-wrap text-sm leading-relaxed text-slate-100">
          {message.content || (streaming ? "…" : "")}
        </div>
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

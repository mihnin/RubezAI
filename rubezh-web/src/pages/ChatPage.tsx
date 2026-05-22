import { useState, useRef, useEffect, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Send,
  ShieldAlert,
  Sparkles,
  AlertCircle,
  Eye,
} from "lucide-react";
import { streamChat } from "../api/sse";
import { apiFetch } from "../api/client";
import { CloudGate } from "../components/CloudGate";
import { ProviderModelPicker } from "../components/ProviderModelPicker";
import {
  ModelListSchema,
  ChatSessionSchema,
  RevealSchema,
  ChatPreviewSchema,
  type ChatEvent,
  type ChatPreview,
  type Model,
} from "../api/schemas";

interface Message {
  role: "user" | "assistant";
  content: string;
  decision?: string;
  reasons?: string[];
  id?: string; // id сообщения ассистента (из SSE done) — для reveal (J.2)
  revealed?: boolean; // раскрыты ли реальные данные
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
  // Гейт предпросмотра перед отправкой в облако (J.1) и индикатор обработки.
  const [gate, setGate] = useState<(ChatPreview & { input: string }) | null>(null);
  const [previewing, setPreviewing] = useState(false);
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
    if (!input.trim() || streaming || previewing) return;
    if (!activeProvider) {
      setError(
        "Нет активных LLM-провайдеров. Откройте «Модели» и создайте провайдера.",
      );
      return;
    }
    const text = input;
    // Облачная модель (external) → гейт предпросмотра: показываем обезличенный
    // текст и просим подтвердить (данные выходят за контур). Локальная
    // (trusted_local) → сразу, raw остаётся в периметре.
    if (activeProvider.trust_level === "external") {
      setError(null);
      setPreviewing(true);
      try {
        const data = await apiFetch("/api/chat/preview", ChatPreviewSchema, {
          method: "POST",
          body: JSON.stringify({ text, provider: activeProvider.name }),
        });
        setGate({ ...data, input: text });
      } catch (e) {
        setError(e instanceof Error ? e.message : "ошибка обезличивания");
      } finally {
        setPreviewing(false);
      }
      return;
    }
    setInput("");
    await doSend(text, "");
  }

  // doSend — фактический стрим в LLM. previewToken (для cloud) гарантирует,
  // что отправлен ровно подтверждённый обезличенный текст (J.0).
  async function doSend(text: string, previewToken: string) {
    const userMsg: Message = { role: "user", content: text };
    setMessages((m) => [...m, userMsg, { role: "assistant", content: "" }]);
    setStreaming(true);
    setError(null);

    const ctrl = new AbortController();
    abortRef.current = ctrl;
    try {
      let sid = sessionId;
      if (!sid) {
        const created = await apiFetch(
          "/api/chat/sessions",
          ChatSessionSchema,
          { method: "POST", body: JSON.stringify({ title: "web-ui" }) },
        );
        sid = created.id;
        setSessionId(sid);
      }
      await streamChat({
        sessionId: sid,
        message: text,
        provider: activeProvider!.name,
        model: modelName || activeProvider!.name,
        previewToken,
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

  // J.2: раскрытие реальных данных в ответе ассистента по кнопке
  // (детерминированно на бэкенде; raw приходит только здесь, аудируется).
  async function revealMessage(index: number) {
    const msg = messages[index];
    if (!msg?.id || msg.revealed) return;
    try {
      const data = await apiFetch(
        `/api/chat/messages/${msg.id}/reveal`,
        RevealSchema,
        { method: "POST" },
      );
      setMessages((arr) =>
        arr.map((m, j) =>
          j === index
            ? { ...m, content: data.revealed_text, revealed: true }
            : m,
        ),
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "не удалось раскрыть данные");
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
            onReveal={() => revealMessage(i)}
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
      {previewing && (
        <div
          aria-live="polite"
          className="border-t border-slate-800 px-4 py-2.5 text-sm text-cyan-300 flex items-center gap-2 bg-slate-900/40"
        >
          <Sparkles className="w-4 h-4 animate-spin" strokeWidth={2} />
          Обрабатываем персональные данные…
        </div>
      )}
      {gate && (
        <CloudGate
          gate={gate}
          provider={providerName}
          onCancel={() => setGate(null)}
          onConfirm={() => {
            const g = gate;
            setGate(null);
            setInput("");
            doSend(g.input, g.preview_token);
          }}
        />
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
  } else if (ev.type === "done") {
    // J.2: запоминаем id сообщения ассистента для кнопки reveal
    if (ev.payload.assistant_message_id) {
      setMessages((m) => attachLastID(m, ev.payload.assistant_message_id));
    }
  } else if (ev.type === "error") {
    setError(`${ev.payload.message} (req=${ev.payload.request_id})`);
  }
}

function attachLastID(list: Message[], id: string): Message[] {
  const last = list[list.length - 1];
  if (!last || last.role !== "assistant") return list;
  return [...list.slice(0, -1), { ...last, id }];
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
  onReveal,
}: {
  message: Message;
  streaming: boolean;
  onReveal?: () => void;
}) {
  const isUser = message.role === "user";
  // J.2: раскрытие доступно для записанного masked-ответа, ещё не раскрытого
  const canReveal =
    !isUser && !!message.id && message.decision === "allow_masked" &&
    !message.revealed;
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
        {canReveal && (
          <button
            onClick={onReveal}
            className="mt-2.5 text-xs inline-flex items-center gap-1 text-cyan-400 hover:text-cyan-300"
          >
            <Eye className="w-3.5 h-3.5" strokeWidth={2} />
            Показать реальные данные
          </button>
        )}
        {message.revealed && (
          <div className="mt-2.5 text-xs inline-flex items-center gap-1 text-emerald-300">
            <Eye className="w-3.5 h-3.5" strokeWidth={2} />
            раскрыто · записано в аудит
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

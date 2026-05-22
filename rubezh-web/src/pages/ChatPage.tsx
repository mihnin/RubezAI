import { useState, useRef, useEffect, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Send, Sparkles, AlertCircle, Paperclip, X } from "lucide-react";
import { streamChat } from "../api/sse";
import { apiFetch } from "../api/client";
import { CloudGate } from "../components/CloudGate";
import { ProviderModelPicker } from "../components/ProviderModelPicker";
import { MessageBubble, type Message } from "../components/MessageBubble";
import {
  ModelListSchema,
  ChatSessionSchema,
  RevealSchema,
  ChatPreviewSchema,
  DocumentSchema,
  type ChatEvent,
  type ChatPreview,
  type Model,
} from "../api/schemas";

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
  // Прикреплённый документ (J.3): загружается и обрабатывается, затем его
  // обезличенный текст отправляется в чат вместо/вместе с вводом.
  const [doc, setDoc] = useState<
    { id: string; filename: string; status: string } | null
  >(null);
  const fileRef = useRef<HTMLInputElement>(null);
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
    if (streaming || previewing) return;
    const readyDoc = doc?.status === "done" ? doc : null;
    if (!input.trim() && !readyDoc) return;
    if (!activeProvider) {
      setError(
        "Нет активных LLM-провайдеров. Откройте «Модели» и создайте провайдера.",
      );
      return;
    }
    const text = input;
    // Документ имеет приоритет: в чат уходит его обезличенный текст. В ленте
    // показываем «📎 имя файла».
    const displayText = readyDoc ? `📎 ${readyDoc.filename}` : text;
    // Preview нужен, если облако (для гейта) ИЛИ есть документ (его текст
    // отправляется по preview_token, а не через message).
    if (activeProvider.trust_level === "external" || readyDoc) {
      setError(null);
      setPreviewing(true);
      try {
        const body = readyDoc
          ? { document_id: readyDoc.id, provider: activeProvider.name }
          : { text, provider: activeProvider.name };
        const data = await apiFetch("/api/chat/preview", ChatPreviewSchema, {
          method: "POST",
          body: JSON.stringify(body),
        });
        if (activeProvider.trust_level === "external") {
          setGate({ ...data, input: displayText });
        } else {
          // локальная модель + документ: гейт не нужен, но шлём через токен
          setInput("");
          setDoc(null);
          await doSend(displayText, data.preview_token);
        }
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

  // J.3: загрузка документа в чат — upload + опрос статуса обработки.
  async function handleAttach(file: File) {
    setError(null);
    try {
      const fd = new FormData();
      fd.append("file", file);
      const created = await apiFetch("/api/documents", DocumentSchema, {
        method: "POST",
        body: fd,
      });
      setDoc({ id: created.id, filename: created.filename, status: created.status });
      for (let i = 0; i < 60; i++) {
        await new Promise((r) => setTimeout(r, 1500));
        const d = await apiFetch(`/api/documents/${created.id}`, DocumentSchema);
        setDoc((cur) =>
          cur && cur.id === created.id ? { ...cur, status: d.status } : cur,
        );
        if (d.status === "done" || d.status === "failed") break;
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "не удалось загрузить документ");
      setDoc(null);
    } finally {
      if (fileRef.current) fileRef.current.value = "";
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
            setDoc(null);
            doSend(g.input, g.preview_token);
          }}
        />
      )}
      <footer className="border-t border-slate-800 p-4 bg-slate-950">
        <div className="flex gap-2 max-w-4xl mx-auto items-stretch">
          <input
            ref={fileRef}
            type="file"
            accept=".pdf,.docx,.txt"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) handleAttach(f);
            }}
          />
          <button
            onClick={() => fileRef.current?.click()}
            disabled={streaming || !!doc}
            title="Прикрепить документ"
            aria-label="Прикрепить документ"
            className="px-3 self-stretch rounded-lg border border-slate-800 text-slate-400 hover:text-cyan-300 hover:border-slate-700 disabled:opacity-40 transition"
          >
            <Paperclip className="w-4 h-4" strokeWidth={2} />
          </button>
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
            placeholder={
              doc
                ? "Документ будет отправлен (можно без текста)"
                : "Введите сообщение… (Enter — отправить, Shift+Enter — новая строка)"
            }
          />
          <button
            onClick={send}
            aria-label="Отправить"
            disabled={
              streaming ||
              (!input.trim() && doc?.status !== "done") ||
              !activeProvider
            }
            className="px-4 self-stretch rounded-lg bg-cyan-500 hover:bg-cyan-400 text-slate-950 font-medium disabled:opacity-40 disabled:cursor-not-allowed transition shadow-lg shadow-cyan-500/20 flex items-center gap-1.5"
          >
            {streaming ? (
              <Sparkles className="w-4 h-4 animate-spin" strokeWidth={2.5} />
            ) : (
              <Send className="w-4 h-4" strokeWidth={2.5} />
            )}
          </button>
        </div>
        {doc && (
          <div className="max-w-4xl mx-auto mt-2 text-xs inline-flex items-center gap-2 bg-slate-900/60 border border-slate-800 rounded-lg px-3 py-1.5">
            <Paperclip className="w-3.5 h-3.5 text-slate-400" strokeWidth={2} />
            <span className="text-slate-300">{doc.filename}</span>
            <span
              className={
                doc.status === "done"
                  ? "text-emerald-300"
                  : doc.status === "failed"
                    ? "text-red-300"
                    : "text-amber-300"
              }
            >
              {doc.status === "done"
                ? "✓ обработан"
                : doc.status === "failed"
                  ? "✗ ошибка"
                  : "обрабатывается…"}
            </span>
            <button
              onClick={() => setDoc(null)}
              aria-label="Убрать документ"
              className="text-slate-500 hover:text-red-400"
            >
              <X className="w-3.5 h-3.5" strokeWidth={2} />
            </button>
          </div>
        )}
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


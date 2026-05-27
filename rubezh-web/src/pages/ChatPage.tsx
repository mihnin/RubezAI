import { useState, useRef, useEffect, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Send,
  Sparkles,
  AlertCircle,
  Paperclip,
  X,
  BookOpen,
  ShieldCheck,
  SlidersHorizontal,
  RotateCcw,
} from "lucide-react";
import { streamChat } from "../api/sse";
import { apiFetch } from "../api/client";
import { CloudGate } from "../components/CloudGate";
import { ProviderModelPicker } from "../components/ProviderModelPicker";
import {
  MessageBubble,
  type ChatStatusView,
  type Message,
} from "../components/MessageBubble";
import {
  ModelListSchema,
  ChatSessionSchema,
  RevealSchema,
  ChatPreviewSchema,
  DocumentSchema,
  type ChatEvent,
  type ChatPreview,
  type Model,
  type RagHitMeta,
} from "../api/schemas";

const STORAGE_PROVIDER = "rubezh.chat.provider";
const STORAGE_MODEL = "rubezh.chat.model";
const STORAGE_USE_RAG = "rubezh.chat.useRag";
const STORAGE_USE_REVIEW = "rubezh.chat.useReview";
const STORAGE_SYSTEM_PROMPT_PRIMARY = "rubezh.chat.systemPrompt.primary";
const STORAGE_SYSTEM_PROMPT_REVIEW_1 = "rubezh.chat.systemPrompt.review1";
const STORAGE_SYSTEM_PROMPT_REVIEW_2 = "rubezh.chat.systemPrompt.review2";
const STORAGE_REVIEW_MAX_ROUNDS = "rubezh.chat.reviewMaxRounds";
const REVIEW_PROVIDER_PRIORITY = [
  "claude-code-cli",
  "codex-cli",
  "grok-build",
  "gemini-cli",
];

/** ChatPage. Реальный SSE — event: meta/status/delta/done/error (RFC 6202).
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
  const [gate, setGate] = useState<(ChatPreview & { input: string }) | null>(
    null,
  );
  const [previewing, setPreviewing] = useState(false);
  // Прикреплённый документ (J.3): загружается и обрабатывается, затем его
  // обезличенный текст отправляется в чат вместо/вместе с вводом.
  const [doc, setDoc] = useState<{
    id: string;
    filename: string;
    status: string;
  } | null>(null);
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
  // useRag — глобальный toggle «Искать по документам» (Итерация 11 §Р4 Ф5).
  // Состояние сохраняется в localStorage, чтобы пережить reload и выбор сессии.
  const [useRag, setUseRag] = useState<boolean>(
    () => localStorage.getItem(STORAGE_USE_RAG) === "1",
  );
  const [useReview, setUseReview] = useState<boolean>(
    () => localStorage.getItem(STORAGE_USE_REVIEW) === "1",
  );
  const [promptSettingsOpen, setPromptSettingsOpen] = useState(false);
  const [primarySystemPrompt, setPrimarySystemPrompt] = useState<string>(
    () => localStorage.getItem(STORAGE_SYSTEM_PROMPT_PRIMARY) ?? "",
  );
  const [reviewSystemPrompt1, setReviewSystemPrompt1] = useState<string>(
    () => localStorage.getItem(STORAGE_SYSTEM_PROMPT_REVIEW_1) ?? "",
  );
  const [reviewSystemPrompt2, setReviewSystemPrompt2] = useState<string>(
    () => localStorage.getItem(STORAGE_SYSTEM_PROMPT_REVIEW_2) ?? "",
  );
  const [reviewMaxRounds, setReviewMaxRounds] = useState<number>(() =>
    clampReviewRounds(Number(localStorage.getItem(STORAGE_REVIEW_MAX_ROUNDS) ?? 3)),
  );
  useEffect(() => {
    localStorage.setItem(STORAGE_USE_RAG, useRag ? "1" : "0");
  }, [useRag]);
  useEffect(() => {
    localStorage.setItem(STORAGE_USE_REVIEW, useReview ? "1" : "0");
  }, [useReview]);
  useEffect(() => {
    localStorage.setItem(STORAGE_SYSTEM_PROMPT_PRIMARY, primarySystemPrompt);
  }, [primarySystemPrompt]);
  useEffect(() => {
    localStorage.setItem(STORAGE_SYSTEM_PROMPT_REVIEW_1, reviewSystemPrompt1);
  }, [reviewSystemPrompt1]);
  useEffect(() => {
    localStorage.setItem(STORAGE_SYSTEM_PROMPT_REVIEW_2, reviewSystemPrompt2);
  }, [reviewSystemPrompt2]);
  useEffect(() => {
    localStorage.setItem(STORAGE_REVIEW_MAX_ROUNDS, String(reviewMaxRounds));
  }, [reviewMaxRounds]);

  // Если ещё ничего не выбрано или сохранённый провайдер пропал — берём первый.
  useEffect(() => {
    if (enabled.length === 0) return;
    const found = enabled.find((m) => m.name === providerName);
    if (!found) {
      const first = enabled[0];
      setProviderName(first.name);
      setModelName(defaultModelFor(first));
      return;
    }
    const repaired = repairStaleModel(found, modelName);
    if (repaired !== modelName) {
      setModelName(repaired);
    }
  }, [enabled, providerName, modelName]);

  useEffect(() => {
    if (providerName) localStorage.setItem(STORAGE_PROVIDER, providerName);
    if (modelName) localStorage.setItem(STORAGE_MODEL, modelName);
  }, [providerName, modelName]);

  const activeProvider = enabled.find((m) => m.name === providerName);
  const reviewProviderNames = useMemo(
    () => pickReviewProviders(enabled, providerName),
    [enabled, providerName],
  );

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
    const provider = activeProvider!;
    const selectedModel = repairStaleModel(provider, modelName);
    const reviewers = useReview ? reviewProviderNames : [];
    const reviewSystemPrompts = buildReviewSystemPromptMap(reviewers, [
      reviewSystemPrompt1,
      reviewSystemPrompt2,
    ]);
    const userMsg: Message = { role: "user", content: text };
    setMessages((m) => [
      ...m,
      userMsg,
      {
        role: "assistant",
        content: "",
        statusEvents: [
          {
            request_id: "client",
            stage: "client_prepare",
            message: "Готовлю запрос и открываю поток",
            provider: provider.name,
            model: selectedModel,
            receivedAt: Date.now(),
          },
        ],
      },
    ]);
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
      setMessages((m) =>
        attachLastStatus(m, {
          request_id: "client",
          stage: "sse_connect",
          message: "Подключаюсь к серверному SSE-потоку",
          provider: provider.name,
          model: selectedModel,
        }),
      );
      await streamChat({
        sessionId: sid,
        message: text,
        provider: provider.name,
        model: selectedModel,
        systemPrompt: primarySystemPrompt,
        previewToken,
        rag: useRag ? { enabled: true } : undefined,
        review:
          reviewers.length > 0
            ? {
                enabled: true,
                providers: reviewers,
                max_rounds: reviewMaxRounds,
                system_prompts: reviewSystemPrompts,
              }
            : undefined,
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
      setDoc({
        id: created.id,
        filename: created.filename,
        status: created.status,
      });
      for (let i = 0; i < 60; i++) {
        await new Promise((r) => setTimeout(r, 1500));
        const d = await apiFetch(
          `/api/documents/${created.id}`,
          DocumentSchema,
        );
        setDoc((cur) =>
          cur && cur.id === created.id ? { ...cur, status: d.status } : cur,
        );
        if (d.status === "done" || d.status === "failed") break;
      }
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "не удалось загрузить документ",
      );
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
            <span className="font-mono text-slate-400" data-testid="session-id">
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
              <Sparkles className="w-6 h-6 text-cyan-300" strokeWidth={1.5} />
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
      {promptSettingsOpen && (
        <SystemPromptDialog
          providerName={providerName}
          reviewerNames={reviewProviderNames}
          primaryPrompt={primarySystemPrompt}
          reviewPrompt1={reviewSystemPrompt1}
          reviewPrompt2={reviewSystemPrompt2}
          reviewMaxRounds={reviewMaxRounds}
          onPrimaryPromptChange={setPrimarySystemPrompt}
          onReviewPrompt1Change={setReviewSystemPrompt1}
          onReviewPrompt2Change={setReviewSystemPrompt2}
          onReviewMaxRoundsChange={setReviewMaxRounds}
          onReset={() => {
            setPrimarySystemPrompt("");
            setReviewSystemPrompt1("");
            setReviewSystemPrompt2("");
            setReviewMaxRounds(3);
          }}
          onClose={() => setPromptSettingsOpen(false)}
        />
      )}
      <footer className="border-t border-slate-800 p-4 bg-slate-950">
        <div className="max-w-4xl mx-auto mb-2 flex flex-wrap items-center gap-2 text-xs text-slate-400">
          <label className="inline-flex items-center gap-2 cursor-pointer select-none">
            <input
              type="checkbox"
              role="switch"
              aria-label="Искать по документам"
              checked={useRag}
              onChange={(e) => setUseRag(e.target.checked)}
              className="accent-cyan-500"
              data-testid="rag-toggle"
            />
            <BookOpen className="w-3.5 h-3.5" strokeWidth={2} />
            <span>Искать по документам</span>
          </label>
          {useRag && (
            <span className="text-slate-500">
              · RAG включён, источники появятся под ответом
            </span>
          )}
          <label
            className={`inline-flex items-center gap-2 select-none ${
              reviewProviderNames.length > 0
                ? "cursor-pointer"
                : "cursor-not-allowed opacity-50"
            }`}
          >
            <input
              type="checkbox"
              role="switch"
              aria-label="Ревизия моделями"
              checked={useReview && reviewProviderNames.length > 0}
              disabled={reviewProviderNames.length === 0}
              onChange={(e) => setUseReview(e.target.checked)}
              className="accent-cyan-500"
              data-testid="review-toggle"
            />
            <ShieldCheck className="w-3.5 h-3.5" strokeWidth={2} />
            <span>Ревизия моделями</span>
          </label>
          {useReview && reviewProviderNames.length > 0 && (
            <span
              className="text-slate-500 truncate max-w-full"
              title={reviewProviderNames.join(" → ")}
            >
              · {reviewProviderNames.join(" → ")}
            </span>
          )}
          <button
            type="button"
            onClick={() => setPromptSettingsOpen(true)}
            title="Системные промты моделей"
            className="inline-flex items-center gap-1.5 rounded-md border border-slate-800 px-2 py-1 text-slate-400 hover:border-cyan-500/50 hover:text-cyan-300 transition"
          >
            <SlidersHorizontal className="w-3.5 h-3.5" strokeWidth={2} />
            <span>Промты</span>
          </button>
        </div>
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

function SystemPromptDialog({
  providerName,
  reviewerNames,
  primaryPrompt,
  reviewPrompt1,
  reviewPrompt2,
  reviewMaxRounds,
  onPrimaryPromptChange,
  onReviewPrompt1Change,
  onReviewPrompt2Change,
  onReviewMaxRoundsChange,
  onReset,
  onClose,
}: {
  providerName: string;
  reviewerNames: string[];
  primaryPrompt: string;
  reviewPrompt1: string;
  reviewPrompt2: string;
  reviewMaxRounds: number;
  onPrimaryPromptChange: (v: string) => void;
  onReviewPrompt1Change: (v: string) => void;
  onReviewPrompt2Change: (v: string) => void;
  onReviewMaxRoundsChange: (v: number) => void;
  onReset: () => void;
  onClose: () => void;
}) {
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="system-prompts-title"
      className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/75 p-4 backdrop-blur-sm"
    >
      <div className="w-full max-w-3xl rounded-lg border border-slate-700 bg-slate-950 shadow-2xl shadow-black/50">
        <div className="flex items-center justify-between gap-3 border-b border-slate-800 px-5 py-4">
          <div>
            <h2
              id="system-prompts-title"
              className="text-base font-semibold tracking-tight"
            >
              Системные промты
            </h2>
            <div className="text-xs text-slate-500">
              {providerName || "модель не выбрана"}
            </div>
          </div>
          <button
            type="button"
            onClick={onReset}
            title="Сбросить промты"
            className="inline-flex items-center gap-1.5 rounded-md border border-slate-800 px-2.5 py-1.5 text-xs text-slate-400 hover:border-amber-500/50 hover:text-amber-300 transition"
          >
            <RotateCcw className="h-3.5 w-3.5" strokeWidth={2} />
            <span>Сбросить</span>
          </button>
        </div>
        <div className="max-h-[70vh] space-y-4 overflow-auto px-5 py-4">
          <PromptTextarea
            label={`Модель 1 · ${providerName || "основная"}`}
            ariaLabel="Системный промт модели 1"
            value={primaryPrompt}
            onChange={onPrimaryPromptChange}
            placeholder="Например: отвечай как senior-разработчик, сначала дай решение, потом риски."
          />
          <PromptTextarea
            label={`Модель 2 · ${reviewerNames[0] ?? "ревизор 1"}`}
            ariaLabel="Системный промт модели 2"
            value={reviewPrompt1}
            onChange={onReviewPrompt1Change}
            placeholder="Например: проверь факты, полноту и противоречия, затем верни финальную версию."
          />
          <PromptTextarea
            label={`Модель 3 · ${reviewerNames[1] ?? "ревизор 2"}`}
            ariaLabel="Системный промт модели 3"
            value={reviewPrompt2}
            onChange={onReviewPrompt2Change}
            placeholder="Например: проверь безопасность, стиль и практичность ответа."
          />
          <label className="block">
            <span className="mb-1.5 block text-xs font-medium text-slate-300">
              Циклы ревизии
            </span>
            <input
              type="number"
              min={1}
              max={5}
              value={reviewMaxRounds}
              onChange={(e) =>
                onReviewMaxRoundsChange(clampReviewRounds(Number(e.target.value)))
              }
              aria-label="Максимум циклов ревизии"
              className="w-24 rounded-md border border-slate-800 bg-slate-900 px-3 py-2 text-sm text-slate-100 outline-none transition focus:border-cyan-500 focus:ring-2 focus:ring-cyan-500/20"
            />
            <span className="ml-3 text-xs text-slate-500">
              1-5: ревизоры проверяют, модель 1 правит замечания, затем проверка повторяется.
            </span>
          </label>
        </div>
        <div className="flex justify-end gap-2 border-t border-slate-800 px-5 py-4">
          <button
            type="button"
            onClick={onClose}
            className="rounded-md bg-cyan-500 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-cyan-400 transition"
          >
            Готово
          </button>
        </div>
      </div>
    </div>
  );
}

function PromptTextarea({
  label,
  ariaLabel,
  value,
  onChange,
  placeholder,
}: {
  label: string;
  ariaLabel: string;
  value: string;
  onChange: (v: string) => void;
  placeholder: string;
}) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-xs font-medium text-slate-300">
        {label}
      </span>
      <textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        rows={5}
        maxLength={8192}
        aria-label={ariaLabel}
        className="w-full resize-y rounded-md border border-slate-800 bg-slate-900 p-3 text-sm leading-relaxed text-slate-100 outline-none transition placeholder:text-slate-600 focus:border-cyan-500 focus:ring-2 focus:ring-cyan-500/20"
        placeholder={placeholder}
      />
      <span className="mt-1 block text-right font-mono text-[0.68rem] text-slate-600">
        {value.length}/8192
      </span>
    </label>
  );
}

// defaultModelFor — берёт provider.default_model (миграция 000019 хранит
// его в БД и управляется через PATCH /api/models/:id). Если default_model
// пуст (старая запись / openai_compatible / anthropic), используем
// разумный fallback по имени провайдера.
function defaultModelFor(p: Model): string {
  if (p.default_model) {
    return p.default_model;
  }
  // Fallback для openai_compatible/anthropic / без default_model.
  // DeepSeek: облако принимает deepseek-v4-* , а deepseek-r1-distill-qwen-7b —
  // это ЛОКАЛЬНАЯ модель (LM Studio), облако её не знает.
  const n = p.name.toLowerCase();
  if (n.includes("deepseek")) {
    return p.trust_level === "trusted_local"
      ? "deepseek-r1-distill-qwen-7b"
      : "deepseek-v4-flash";
  }
  return p.name;
}

function pickReviewProviders(
  providers: Model[],
  primaryName: string,
): string[] {
  return providers
    .filter(
      (p) =>
        p.is_enabled &&
        p.name !== primaryName &&
        p.adapter === "ssh_cli" &&
        REVIEW_PROVIDER_PRIORITY.includes(p.name),
    )
    .sort(
      (a, b) =>
        REVIEW_PROVIDER_PRIORITY.indexOf(a.name) -
        REVIEW_PROVIDER_PRIORITY.indexOf(b.name),
    )
    .slice(0, 2)
    .map((p) => p.name);
}

function buildReviewSystemPromptMap(
  reviewers: string[],
  prompts: string[],
): Record<string, string> | undefined {
  const out: Record<string, string> = {};
  reviewers.forEach((name, idx) => {
    const prompt = prompts[idx]?.trim();
    if (name && prompt) out[name] = prompt;
  });
  return Object.keys(out).length > 0 ? out : undefined;
}

function clampReviewRounds(value: number): number {
  if (!Number.isFinite(value)) return 3;
  return Math.min(5, Math.max(1, Math.round(value)));
}

// staleModelAliases — старые значения, которые могли осесть в localStorage
// до миграции 000019 (или были именем провайдера). При обнаружении
// заменяем на свежий p.default_model. Расширяй при появлении новых alias.
const staleModelAliases = new Set<string>([
  "gpt-5-codex",
  "codex-cli",
  "claude-code-cli",
  "gemini-cli",
  "gemini-2.5-pro",
  "gemini-3.5-flash",
  "grok",
  "grok-cli",
]);

function repairStaleModel(p: Model, selected: string): string {
  const fallback = defaultModelFor(p);
  // Пусто → дефолт.
  if (!selected) {
    return fallback;
  }
  // Имя провайдера, осевшее в localStorage до миграции 000019.
  if (selected === p.name) {
    return fallback;
  }
  // Известный устаревший alias.
  if (staleModelAliases.has(selected)) {
    return fallback;
  }
  return selected;
}

function applyEvent(
  ev: ChatEvent,
  setMessages: React.Dispatch<React.SetStateAction<Message[]>>,
  setError: React.Dispatch<React.SetStateAction<string | null>>,
): void {
  if (ev.type === "delta") {
    setMessages((m) => appendDelta(m, ev.payload.content));
  } else if (ev.type === "status") {
    setMessages((m) => attachLastStatus(m, ev.payload));
  } else if (ev.type === "meta") {
    setMessages((m) =>
      annotateLast(m, ev.payload.decision, ev.payload.reasons),
    );
  } else if (ev.type === "done") {
    // J.2: запоминаем id сообщения ассистента для кнопки reveal
    setMessages((m) => {
      const withID = ev.payload.assistant_message_id
        ? attachLastID(m, ev.payload.assistant_message_id)
        : m;
      return attachLastDoneStatus(withID, ev.payload.request_id);
    });
  } else if (ev.type === "rag_hits") {
    // Итерация 11 §Р4 Ф5: источники retrieval'а кладём на текущее
    // assistant-сообщение, чтобы MessageBubble отрисовал их chip-list'ом.
    setMessages((m) => attachLastRagHits(m, ev.payload.hits));
  } else if (ev.type === "error") {
    setError(`${ev.payload.message} (req=${ev.payload.request_id})`);
  }
}

function attachLastStatus(
  list: Message[],
  status: Extract<ChatEvent, { type: "status" }>["payload"],
): Message[] {
  const last = list[list.length - 1];
  if (!last || last.role !== "assistant") return list;
  const withTime: ChatStatusView = { ...status, receivedAt: Date.now() };
  const statusEvents = [...(last.statusEvents ?? []), withTime];
  return [...list.slice(0, -1), { ...last, statusEvents }];
}

function attachLastDoneStatus(list: Message[], requestID: string): Message[] {
  const last = list[list.length - 1];
  if (!last || last.role !== "assistant") return list;
  const previous = last.statusEvents?.[last.statusEvents.length - 1];
  return attachLastStatus(list, {
    request_id: requestID,
    stage: "done",
    message: "Ответ доставлен в чат",
    provider: previous?.provider ?? "",
    model: previous?.model ?? "",
  });
}

function attachLastRagHits(list: Message[], hits: RagHitMeta[]): Message[] {
  const last = list[list.length - 1];
  if (!last || last.role !== "assistant") return list;
  return [...list.slice(0, -1), { ...last, ragHits: hits }];
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

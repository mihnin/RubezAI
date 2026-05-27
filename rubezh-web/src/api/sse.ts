import {
  ChatMetaPayloadSchema,
  ChatStatusPayloadSchema,
  ChatDeltaPayloadSchema,
  ChatDonePayloadSchema,
  ChatErrorPayloadSchema,
  ChatRagHitsPayloadSchema,
  type ChatEvent,
  type RagRequestParams,
  type ReviewRequestParams,
} from "./schemas";

/**
 * SSE-клиент для /api/chat. Использует fetch+ReadableStream
 * (EventSource не поддерживает кастомные заголовки → нельзя Bearer).
 *
 * Формат backend (rubezh-api/internal/api/chat.go: writeEvent):
 *   event: meta|status|delta|done|error|rag_hits
 *   data: {...json...}
 *   (пустая строка-разделитель)
 *
 * Каждое событие парсится с учётом event-name + Zod-валидации payload
 * по соответствующей схеме. Невалидные events игнорируются.
 */

const STORAGE_TOKEN = "rubezh.auth.token";

export interface ChatStreamOptions {
  sessionId?: string;
  message: string;
  provider: string;
  model: string;
  systemPrompt?: string; // системная инструкция основной модели
  previewToken?: string; // токен подтверждённого предпросмотра (J.0)
  rag?: RagRequestParams; // RAG-параметры (Итерация 11 §Р4)
  review?: ReviewRequestParams; // server-side ревизия несколькими моделями
  onEvent: (event: ChatEvent) => void;
  signal?: AbortSignal;
}

export async function streamChat(opts: ChatStreamOptions): Promise<void> {
  const token = localStorage.getItem(STORAGE_TOKEN);
  const body: Record<string, unknown> = {
    message: opts.message,
    provider: opts.provider,
    model: opts.model,
  };
  if (opts.sessionId) body.session_id = opts.sessionId;
  if (opts.systemPrompt?.trim()) body.system_prompt = opts.systemPrompt.trim();
  if (opts.previewToken) body.preview_token = opts.previewToken;
  if (opts.rag && opts.rag.enabled) body.rag = opts.rag;
  if (opts.review && opts.review.enabled) body.review = opts.review;

  const resp = await fetch("/api/chat", {
    method: "POST",
    signal: opts.signal,
    headers: {
      "Content-Type": "application/json",
      Authorization: token ? `Bearer ${token}` : "",
      Accept: "text/event-stream",
    },
    body: JSON.stringify(body),
  });

  if (!resp.ok || !resp.body) {
    const text = await resp.text().catch(() => "");
    throw new Error(`HTTP ${resp.status}: ${text || resp.statusText}`);
  }

  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  // W2.1: backend SSE-контракт обещает терминальный done | error в конце
  // (см. chat.schema.json). Если поток закрылся раньше — это обрыв
  // (proxy timeout / network / OOM на стороне модели), UI должен
  // явно показать ошибку, а не «съесть» half-streamed ответ как успех.
  let sawTerminal = false;
  let lastRequestID = "";

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      const blocks = buf.split("\n\n");
      buf = blocks.pop() ?? "";
      for (const block of blocks) {
        const ev = parseBlock(block);
        if (!ev) continue;
        if (ev.type === "meta" || ev.type === "done" || ev.type === "error") {
          lastRequestID = ev.payload.request_id || lastRequestID;
        }
        if (ev.type === "done" || ev.type === "error") {
          sawTerminal = true;
        }
        opts.onEvent(ev);
      }
    }
  } catch (e) {
    // AbortError при пользовательской отмене — НЕ ошибка протокола.
    // Детектим по двум каналам: signal.aborted (надёжно) и name (legacy
    // path для DOMException / TypeError: terminated в некоторых
    // undici-сборках). MN-1 ревью W2.
    const aborted =
      opts.signal?.aborted ||
      (e as Error).name === "AbortError" ||
      ((e as Error).name === "TypeError" &&
        /terminated|aborted/i.test((e as Error).message ?? ""));
    if (!sawTerminal && !aborted) {
      opts.onEvent({
        type: "error",
        payload: {
          message: `поток оборван: ${(e as Error).message || "network error"}`,
          request_id: lastRequestID,
        },
      });
    }
    throw e;
  }
  if (!sawTerminal) {
    opts.onEvent({
      type: "error",
      payload: {
        message: "поток оборван без терминального события (truncated)",
        request_id: lastRequestID,
      },
    });
  }
}

function parseBlock(block: string): ChatEvent | null {
  let name = "";
  let dataLine = "";
  for (const ln of block.split("\n")) {
    if (ln.startsWith("event: ")) name = ln.slice(7).trim();
    else if (ln.startsWith("data: ")) dataLine = ln.slice(6);
  }
  if (!name || !dataLine) return null;
  const json = safeJson(dataLine);
  if (json === null) return null;

  if (name === "meta") {
    const r = ChatMetaPayloadSchema.safeParse(json);
    return r.success ? { type: "meta", payload: r.data } : null;
  }
  if (name === "delta") {
    const r = ChatDeltaPayloadSchema.safeParse(json);
    return r.success ? { type: "delta", payload: r.data } : null;
  }
  if (name === "status") {
    const r = ChatStatusPayloadSchema.safeParse(json);
    return r.success ? { type: "status", payload: r.data } : null;
  }
  if (name === "done") {
    const r = ChatDonePayloadSchema.safeParse(json);
    return r.success ? { type: "done", payload: r.data } : null;
  }
  if (name === "error") {
    const r = ChatErrorPayloadSchema.safeParse(json);
    return r.success ? { type: "error", payload: r.data } : null;
  }
  if (name === "rag_hits") {
    const r = ChatRagHitsPayloadSchema.safeParse(json);
    return r.success ? { type: "rag_hits", payload: r.data } : null;
  }
  return null;
}

function safeJson(s: string): unknown {
  try {
    return JSON.parse(s);
  } catch {
    return null;
  }
}

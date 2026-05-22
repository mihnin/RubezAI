import {
  ChatMetaPayloadSchema,
  ChatDeltaPayloadSchema,
  ChatDonePayloadSchema,
  ChatErrorPayloadSchema,
  type ChatEvent,
} from "./schemas";

/**
 * SSE-клиент для /api/chat. Использует fetch+ReadableStream
 * (EventSource не поддерживает кастомные заголовки → нельзя Bearer).
 *
 * Формат backend (rubezh-api/internal/api/chat.go: writeEvent):
 *   event: meta|delta|done|error
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
  previewToken?: string; // токен подтверждённого предпросмотра (J.0)
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
  if (opts.previewToken) body.preview_token = opts.previewToken;

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

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    const blocks = buf.split("\n\n");
    buf = blocks.pop() ?? "";
    for (const block of blocks) {
      const ev = parseBlock(block);
      if (ev) opts.onEvent(ev);
    }
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
  if (name === "done") {
    const r = ChatDonePayloadSchema.safeParse(json);
    return r.success ? { type: "done", payload: r.data } : null;
  }
  if (name === "error") {
    const r = ChatErrorPayloadSchema.safeParse(json);
    return r.success ? { type: "error", payload: r.data } : null;
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

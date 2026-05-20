import { ChatEventSchema, type ChatEvent } from "./schemas";

/**
 * SSE-клиент для /api/chat. Использует fetch+ReadableStream
 * (EventSource не поддерживает кастомные заголовки → нельзя Bearer).
 *
 * Поток событий парсится по RFC 6202 (data: ...\n\n) и валидируется
 * Zod-схемой. Невалидные события игнорируются (на боевом контракте
 * этого не должно быть; защита от мисматча версии).
 */

const STORAGE_TOKEN = "rubezh.auth.token";

export interface ChatStreamOptions {
  sessionId: string;
  messages: Array<{ role: "user" | "assistant"; content: string }>;
  onEvent: (event: ChatEvent) => void;
  signal?: AbortSignal;
}

export async function streamChat({
  sessionId,
  messages,
  onEvent,
  signal,
}: ChatStreamOptions): Promise<void> {
  const token = localStorage.getItem(STORAGE_TOKEN);
  const resp = await fetch("/api/chat", {
    method: "POST",
    signal,
    headers: {
      "Content-Type": "application/json",
      Authorization: token ? `Bearer ${token}` : "",
      Accept: "text/event-stream",
    },
    body: JSON.stringify({ session_id: sessionId, messages }),
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
      dispatchBlock(block, onEvent);
    }
  }
}

function dispatchBlock(block: string, onEvent: (e: ChatEvent) => void): void {
  for (const ln of block.split("\n")) {
    if (!ln.startsWith("data: ")) continue;
    const payload = ln.slice(6);
    const parsed = ChatEventSchema.safeParse(safeJson(payload));
    if (parsed.success) onEvent(parsed.data);
  }
}

function safeJson(s: string): unknown {
  try {
    return JSON.parse(s);
  } catch {
    return null;
  }
}

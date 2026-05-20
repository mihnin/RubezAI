import { useState, useRef, useEffect } from "react";

interface Message {
  role: "user" | "assistant";
  content: string;
  decision?: string;
  entities?: Array<{ type: string; pseudonym: string }>;
}

/** ChatPage (Итерация 13). SSE-стрим через /api/chat по chat.schema.json. */
export default function ChatPage() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const [sessionId] = useState<string>(() => crypto.randomUUID());
  const [streaming, setStreaming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight });
  }, [messages]);

  async function send() {
    if (!input.trim() || streaming) return;
    const userMsg: Message = { role: "user", content: input };
    setMessages((m) => [...m, userMsg, { role: "assistant", content: "" }]);
    setInput("");
    setStreaming(true);
    setError(null);

    const token = localStorage.getItem("rubezh.auth.token");
    try {
      const resp = await fetch("/api/chat", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
          Accept: "text/event-stream",
        },
        body: JSON.stringify({
          session_id: sessionId,
          messages: [...messages, userMsg].map((m) => ({
            role: m.role,
            content: m.content,
          })),
        }),
      });

      if (!resp.ok || !resp.body) {
        setError(`HTTP ${resp.status}`);
        setStreaming(false);
        return;
      }

      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      let buf = "";
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        const lines = buf.split("\n\n");
        buf = lines.pop() ?? "";
        for (const block of lines) {
          for (const ln of block.split("\n")) {
            if (!ln.startsWith("data: ")) continue;
            try {
              const ev = JSON.parse(ln.slice(6));
              if (ev.type === "delta") {
                setMessages((m) => {
                  const last = m[m.length - 1];
                  if (!last || last.role !== "assistant") return m;
                  return [
                    ...m.slice(0, -1),
                    { ...last, content: last.content + (ev.text ?? "") },
                  ];
                });
              } else if (ev.type === "decision") {
                setMessages((m) => {
                  const last = m[m.length - 1];
                  if (!last) return m;
                  return [
                    ...m.slice(0, -1),
                    {
                      ...last,
                      decision: ev.decision,
                      entities: ev.entities,
                    },
                  ];
                });
              } else if (ev.type === "error") {
                setError(ev.message ?? "Ошибка");
              }
            } catch {
              // ignore parse errors
            }
          }
        }
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Сетевая ошибка");
    } finally {
      setStreaming(false);
    }
  }

  return (
    <div className="h-screen flex flex-col">
      <header className="border-b border-slate-800 p-4">
        <h1 className="text-lg font-semibold">Чат</h1>
        <p className="text-xs text-slate-500">Сессия: {sessionId.slice(0, 8)}</p>
      </header>
      <div ref={scrollRef} className="flex-1 overflow-auto p-4 space-y-3">
        {messages.length === 0 && (
          <div className="text-slate-500 text-center mt-12">
            Задайте вопрос. ПДн и секреты будут обезличены автоматически.
          </div>
        )}
        {messages.map((m, i) => (
          <div
            key={i}
            className={`max-w-2xl ${
              m.role === "user" ? "ml-auto" : ""
            }`}
          >
            <div
              className={`p-3 rounded-lg ${
                m.role === "user"
                  ? "bg-cyan-500/15 border border-cyan-500/30"
                  : "bg-slate-800/50 border border-slate-700"
              }`}
            >
              <div className="whitespace-pre-wrap text-sm">{m.content || "…"}</div>
              {m.decision && m.decision !== "allow_raw" && (
                <div
                  className="mt-2 text-xs text-amber-300"
                  title="Решение policy engine"
                >
                  ⚠ Решение: <strong>{m.decision}</strong>
                  {m.entities && m.entities.length > 0 && (
                    <span> · Обезличено: {m.entities.length}</span>
                  )}
                </div>
              )}
            </div>
          </div>
        ))}
      </div>
      {error && (
        <div className="bg-red-900/30 border-t border-red-700 p-2 text-sm text-red-200">
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
          className="flex-1 bg-slate-900 border border-slate-700 rounded p-2 text-sm resize-none"
          placeholder="Введите сообщение (Enter — отправить)…"
        />
        <button
          onClick={send}
          disabled={streaming || !input.trim()}
          className="px-4 rounded bg-cyan-500 hover:bg-cyan-400 text-slate-950 font-medium disabled:opacity-40"
        >
          {streaming ? "…" : "→"}
        </button>
      </footer>
    </div>
  );
}

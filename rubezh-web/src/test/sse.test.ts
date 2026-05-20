import { describe, it, expect, vi, beforeEach } from "vitest";
import { streamChat } from "../api/sse";
import type { ChatEvent } from "../api/schemas";

function sseStream(chunks: string[]): ReadableStream<Uint8Array> {
  const enc = new TextEncoder();
  return new ReadableStream({
    start(ctrl) {
      for (const c of chunks) ctrl.enqueue(enc.encode(c));
      ctrl.close();
    },
  });
}

function mockResponse(body: ReadableStream<Uint8Array>, status = 200): Response {
  return new Response(body, { status });
}

describe("streamChat (SSE-клиент)", () => {
  beforeEach(() => {
    localStorage.setItem("rubezh.auth.token", "test-token");
  });

  it("парсит delta + decision + игнорирует неизвестные события", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      mockResponse(
        sseStream([
          'data: {"type":"delta","text":"Hi"}\n\n',
          'data: {"type":"delta","text":" world"}\n\n',
          'data: {"type":"unknown","x":1}\n\n',
          'data: {"type":"decision","decision":"allow_masked","entities":[]}\n\n',
        ]),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const events: ChatEvent[] = [];
    await streamChat({
      sessionId: "s1",
      messages: [{ role: "user", content: "hi" }],
      onEvent: (e) => events.push(e),
    });

    expect(events).toHaveLength(3);
    expect(events[0]).toEqual({ type: "delta", text: "Hi" });
    expect(events[1]).toEqual({ type: "delta", text: " world" });
    expect(events[2].type).toBe("decision");
  });

  it("отправляет Bearer-токен", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      mockResponse(sseStream(['data: {"type":"done"}\n\n'])),
    );
    vi.stubGlobal("fetch", fetchMock);

    await streamChat({ sessionId: "s2", messages: [], onEvent: () => {} });

    const call = fetchMock.mock.calls[0];
    expect(call[1].headers.Authorization).toBe("Bearer test-token");
    expect(call[1].method).toBe("POST");
  });

  it("бросает при non-2xx", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response("nope", { status: 500, statusText: "Server Error" }),
      ),
    );
    await expect(
      streamChat({ sessionId: "s3", messages: [], onEvent: () => {} }),
    ).rejects.toThrow(/HTTP 500/);
  });

  it("обрабатывает чанки, разделённые по половине события", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      mockResponse(
        sseStream([
          'data: {"type":"delt',
          'a","text":"AB"}\n',
          "\n",
          'data: {"type":"delta","text":"CD"}\n\n',
        ]),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const events: ChatEvent[] = [];
    await streamChat({
      sessionId: "s4",
      messages: [],
      onEvent: (e) => events.push(e),
    });

    expect(events.map((e) => e.type === "delta" && e.text)).toEqual([
      "AB",
      "CD",
    ]);
  });
});

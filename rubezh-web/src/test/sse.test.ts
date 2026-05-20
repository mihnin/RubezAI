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

const VALID_META =
  '{"decision":"allow_masked","risk":{"level":"medium","score":0.5,"classes":["PII"]},"provider":"mock","reasons":[],"request_id":"req-1"}';

describe("streamChat (SSE-клиент по RFC 6202)", () => {
  beforeEach(() => {
    localStorage.setItem("rubezh.auth.token", "test-token");
  });

  it("парсит named events meta/delta/done с Zod-валидацией", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      mockResponse(
        sseStream([
          `event: meta\ndata: ${VALID_META}\n\n`,
          'event: delta\ndata: {"content":"Hi"}\n\n',
          'event: delta\ndata: {"content":" world"}\n\n',
          'event: done\ndata: {"request_id":"req-1"}\n\n',
        ]),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const events: ChatEvent[] = [];
    await streamChat({
      sessionId: "s1",
      message: "hi",
      provider: "mock",
      model: "mock",
      onEvent: (e) => events.push(e),
    });

    expect(events.map((e) => e.type)).toEqual([
      "meta",
      "delta",
      "delta",
      "done",
    ]);
    expect(events[0].type === "meta" && events[0].payload.decision).toBe(
      "allow_masked",
    );
    expect(events[1].type === "delta" && events[1].payload.content).toBe("Hi");
  });

  it("отправляет правильное тело: {session_id, message, provider, model}", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      mockResponse(sseStream(['event: done\ndata: {"request_id":"r"}\n\n'])),
    );
    vi.stubGlobal("fetch", fetchMock);

    await streamChat({
      sessionId: "S",
      message: "M",
      provider: "P",
      model: "MM",
      onEvent: () => {},
    });

    const call = fetchMock.mock.calls[0];
    const body = JSON.parse(call[1].body);
    expect(body).toEqual({
      session_id: "S",
      message: "M",
      provider: "P",
      model: "MM",
    });
    expect(call[1].headers.Authorization).toBe("Bearer test-token");
  });

  it("игнорирует невалидный (Zod) payload", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      mockResponse(
        sseStream([
          'event: delta\ndata: {"text":"wrong-field"}\n\n', // нет content
          'event: delta\ndata: {"content":"ok"}\n\n',
        ]),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const events: ChatEvent[] = [];
    await streamChat({
      message: "x",
      provider: "p",
      model: "m",
      onEvent: (e) => events.push(e),
    });
    expect(events).toHaveLength(1);
    expect(events[0].type === "delta" && events[0].payload.content).toBe("ok");
  });

  it("бросает при non-2xx", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response("nope", { status: 500, statusText: "Server Error" }),
      ),
    );
    await expect(
      streamChat({
        message: "x",
        provider: "p",
        model: "m",
        onEvent: () => {},
      }),
    ).rejects.toThrow(/HTTP 500/);
  });

  it("обрабатывает чанки, разделённые по границе блока", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      mockResponse(
        sseStream([
          'event: delta\ndata: {"con',
          'tent":"AB"}\n',
          "\n",
          'event: delta\ndata: {"content":"CD"}\n\n',
        ]),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const events: ChatEvent[] = [];
    await streamChat({
      message: "x",
      provider: "p",
      model: "m",
      onEvent: (e) => events.push(e),
    });
    expect(events.length).toBe(2);
  });
});

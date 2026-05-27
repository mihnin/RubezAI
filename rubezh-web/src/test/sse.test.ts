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

function mockResponse(
  body: ReadableStream<Uint8Array>,
  status = 200,
): Response {
  return new Response(body, { status });
}

const VALID_META =
  '{"decision":"allow_masked","risk":{"level":"medium","score":0.5,"classes":["PII"]},"provider":"mock","reasons":[],"request_id":"req-1"}';

describe("streamChat (SSE-клиент по RFC 6202)", () => {
  beforeEach(() => {
    localStorage.setItem("rubezh.auth.token", "test-token");
  });

  it("парсит named events meta/status/delta/done с Zod-валидацией", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        mockResponse(
          sseStream([
            `event: meta\ndata: ${VALID_META}\n\n`,
            'event: status\ndata: {"request_id":"req-1","stage":"llm_call","message":"ждём модель","provider":"mock","model":"mock"}\n\n',
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
      "status",
      "delta",
      "delta",
      "done",
    ]);
    expect(events[0].type === "meta" && events[0].payload.decision).toBe(
      "allow_masked",
    );
    expect(events[1].type === "status" && events[1].payload.stage).toBe(
      "llm_call",
    );
    expect(events[2].type === "delta" && events[2].payload.content).toBe("Hi");
  });

  it("отправляет правильное тело: {session_id, message, provider, model}", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
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

  it("отправляет review-параметры для server-side ревизии", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        mockResponse(sseStream(['event: done\ndata: {"request_id":"r"}\n\n'])),
      );
    vi.stubGlobal("fetch", fetchMock);

    await streamChat({
      message: "M",
      provider: "codex-cli",
      model: "gpt-5.3-codex",
      systemPrompt: "primary prompt",
      review: {
        enabled: true,
        providers: ["claude-code-cli", "grok-build"],
        max_rounds: 4,
        system_prompts: { "claude-code-cli": "review prompt" },
      },
      onEvent: () => {},
    });

    const body = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(body.system_prompt).toBe("primary prompt");
    expect(body.review).toEqual({
      enabled: true,
      providers: ["claude-code-cli", "grok-build"],
      max_rounds: 4,
      system_prompts: { "claude-code-cli": "review prompt" },
    });
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
    // Один валидный delta + один синтезированный error (нет done в потоке).
    const dataEvents = events.filter((e) => e.type !== "error");
    expect(dataEvents).toHaveLength(1);
    if (dataEvents[0].type === "delta") {
      expect(dataEvents[0].payload.content).toBe("ok");
    }
  });

  it("бросает при non-2xx", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
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

  // W2.1: EOF без терминального done/error должен синтезировать error-event
  // «поток оборвался», иначе UI «съест» обрыв и снимет streaming как успех.
  it("при EOF без done/error синтезирует error-event 'поток оборван'", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        mockResponse(
          sseStream([
            `event: meta\ndata: ${VALID_META}\n\n`,
            'event: delta\ndata: {"content":"hi"}\n\n',
            // ВНИМАНИЕ: нет done/error — поток обрывается на этом.
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
    const last = events[events.length - 1];
    expect(last?.type).toBe("error");
    if (last?.type === "error") {
      expect(last.payload.message.toLowerCase()).toMatch(/обор|truncat|incomplete/);
      // request_id из meta должен перейти в синтезированный error.
      expect(last.payload.request_id).toBe("req-1");
    }
  });

  // W2.1: чистый случай — done пришёл, никакого «фантомного» error.
  it("при нормальном завершении done — error НЕ синтезируется", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        mockResponse(
          sseStream([
            `event: meta\ndata: ${VALID_META}\n\n`,
            'event: delta\ndata: {"content":"hi"}\n\n',
            'event: done\ndata: {"request_id":"req-1","assistant_message_id":"m1"}\n\n',
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
    expect(events.some((e) => e.type === "error")).toBe(false);
    expect(events[events.length - 1].type).toBe("done");
  });

  it("обрабатывает чанки, разделённые по границе блока", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
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
    // 2 delta + синтезированный error на EOF без done (W2.1 truncation guard).
    expect(events.filter((e) => e.type === "delta").length).toBe(2);
    expect(events[events.length - 1].type).toBe("error");
  });
});

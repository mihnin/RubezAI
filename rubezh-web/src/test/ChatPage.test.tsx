import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import ChatPage from "../pages/ChatPage";

function model(over: Record<string, unknown>) {
  return {
    id: "00000000-0000-0000-0000-000000000000",
    name: "model",
    trust_level: "external",
    adapter: "openai_compatible",
    endpoint: "https://x/v1",
    max_tokens: null,
    rate_limit_per_min: null,
    is_enabled: true,
    has_api_key: true,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  };
}

// Имя провайдера deepseek* даёт отдельное имя модели (defaultModelFor) — текст
// «deepseek-cloud» в picker'е уникален (модель отображается отдельной строкой).
const ENABLED_A = model({
  id: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  name: "deepseek-cloud",
});
const ENABLED_B = model({
  id: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
  name: "claude-cloud",
  adapter: "anthropic",
});
const DISABLED = model({
  id: "cccccccc-cccc-cccc-cccc-cccccccccccc",
  name: "mock-local",
  is_enabled: false,
});

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function stubModels(list: unknown[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.resolve(jsonResponse(list))),
  );
}

function renderChat() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ChatPage />
    </QueryClientProvider>,
  );
}

// sseResponse — Response с SSE-телом (ReadableStream) для мока /api/chat.
function sseResponse(events: string): Response {
  const stream = new ReadableStream({
    start(c) {
      c.enqueue(new TextEncoder().encode(events));
      c.close();
    },
  });
  return new Response(stream, {
    status: 200,
    headers: { "Content-Type": "text/event-stream" },
  });
}

describe("ChatPage — reveal реальных данных (J.2)", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  it("стримит ответ с псевдонимами и раскрывает реальные данные по кнопке", async () => {
    const sse =
      'event: meta\ndata: {"decision":"allow_masked","risk":{"level":"high","score":0.8,"classes":["pii"]},"provider":"deepseek-cloud","reasons":[],"request_id":"r1"}\n\n' +
      'event: delta\ndata: {"content":"Ответ про ФИО_001"}\n\n' +
      'event: done\ndata: {"request_id":"r1","assistant_message_id":"msg-1"}\n\n';

    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        const method = init?.method ?? "GET";
        if (url.endsWith("/api/models")) {
          return Promise.resolve(jsonResponse([ENABLED_A]));
        }
        if (url.endsWith("/api/chat/sessions") && method === "POST") {
          return Promise.resolve(
            jsonResponse({
              id: "11111111-1111-1111-1111-111111111111",
              user_id: "22222222-2222-2222-2222-222222222222",
              title: "web-ui",
              created_at: "2026-01-01T00:00:00Z",
              updated_at: "2026-01-01T00:00:00Z",
            }),
          );
        }
        if (url.endsWith("/api/chat/preview") && method === "POST") {
          return Promise.resolve(
            jsonResponse({
              preview_token: "tok-1",
              sanitized_text: "обезличенный запрос ФИО_001",
              entities: [
                {
                  type: "PERSON",
                  category: "pii",
                  pseudonym: "ФИО_001",
                  confidence: 0.9,
                  detector: "regex",
                },
              ],
              risk: { level: "high", score: 0.8, classes: ["pii"] },
            }),
          );
        }
        if (url.endsWith("/api/chat") && method === "POST") {
          return Promise.resolve(sseResponse(sse));
        }
        if (url.includes("/reveal") && method === "POST") {
          return Promise.resolve(
            jsonResponse({ revealed_text: "Ответ про Иванова Ивана" }),
          );
        }
        return Promise.resolve(jsonResponse({}));
      }),
    );

    renderChat();
    await screen.findByText("deepseek-cloud"); // picker готов

    fireEvent.change(screen.getByLabelText("Сообщение"), {
      target: { value: "тест" },
    });
    fireEvent.keyDown(screen.getByLabelText("Сообщение"), { key: "Enter" });

    // облачная модель → гейт предпросмотра; подтверждаем отправку
    const sendCloud = await screen.findByRole("button", {
      name: /Отправить в облако/i,
    });
    fireEvent.click(sendCloud);

    // ответ пришёл с псевдонимами + появилась кнопка reveal
    await screen.findByText("Ответ про ФИО_001");
    const revealBtn = await screen.findByRole("button", {
      name: /Показать реальные данные/i,
    });

    fireEvent.click(revealBtn);

    // после reveal — реальные данные + бейдж «раскрыто»
    await screen.findByText("Ответ про Иванова Ивана");
    expect(await screen.findByText(/раскрыто/i)).toBeInTheDocument();
  });
});

describe("ChatPage — provider/model picker (G.2c)", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  it("показывает только включённых провайдеров и переключает выбор", async () => {
    stubModels([ENABLED_A, ENABLED_B, DISABLED]);
    renderChat();

    // авто-выбран первый включённый провайдер (имя уникально в picker'е)
    const pickerLabel = await screen.findByText("deepseek-cloud");
    fireEvent.click(pickerLabel); // открыть выпадающий список

    // выключенный провайдер не предлагается; второй включённый — да
    expect(screen.queryByText("mock-local")).toBeNull();
    fireEvent.click(await screen.findByText("claude-cloud"));

    await waitFor(() =>
      expect(localStorage.getItem("rubezh.chat.provider")).toBe("claude-cloud"),
    );
  });

  it("без включённых провайдеров блокирует отправку и сообщает об этом", async () => {
    stubModels([DISABLED]);
    renderChat();

    expect(
      await screen.findByText(/нет активных провайдеров/i),
    ).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Сообщение"), {
      target: { value: "привет" },
    });
    expect(screen.getByRole("button", { name: "Отправить" })).toBeDisabled();
  });
});

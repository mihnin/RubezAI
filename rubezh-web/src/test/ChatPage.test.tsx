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
    default_model: "",
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

describe("ChatPage — RAG toggle + источники (Итерация 11 §Р4 Ф5)", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  it("toggle сохраняется в localStorage и передаёт rag.enabled=true в /api/chat", async () => {
    let capturedChatBody: unknown = null;
    const sse =
      'event: meta\ndata: {"decision":"allow_raw","risk":{"level":"low","score":0,"classes":[]},"provider":"deepseek-cloud","reasons":[],"request_id":"r1"}\n\n' +
      'event: rag_hits\ndata: {"request_id":"r1","hits":[{"document_id":"11111111-1111-1111-1111-111111111111","filename":"contract.txt","chunk_index":3,"relevance":0.91}]}\n\n' +
      'event: delta\ndata: {"content":"Ответ"}\n\n' +
      'event: done\ndata: {"request_id":"r1","assistant_message_id":"msg-1"}\n\n';

    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        const method = init?.method ?? "GET";
        if (url.endsWith("/api/models")) {
          // делаем trusted_local чтобы не было гейта/preview (упрощает поток)
          return Promise.resolve(
            jsonResponse([
              model({ name: "local", trust_level: "trusted_local" }),
            ]),
          );
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
        if (url.endsWith("/api/chat") && method === "POST") {
          capturedChatBody = JSON.parse(String(init?.body));
          return Promise.resolve(sseResponse(sse));
        }
        return Promise.resolve(jsonResponse({}));
      }),
    );

    renderChat();
    const toggle = await screen.findByTestId("rag-toggle"); // дождёмся UI
    expect((toggle as HTMLInputElement).checked).toBe(false);
    fireEvent.click(toggle);
    await waitFor(() =>
      expect(localStorage.getItem("rubezh.chat.useRag")).toBe("1"),
    );

    fireEvent.change(screen.getByLabelText("Сообщение"), {
      target: { value: "что в договоре" },
    });
    fireEvent.keyDown(screen.getByLabelText("Сообщение"), { key: "Enter" });

    await screen.findByText("Ответ");
    const body = capturedChatBody as { rag?: { enabled: boolean } };
    expect(body.rag).toEqual({ enabled: true });

    // источники должны появиться в bubble
    const sources = await screen.findByTestId("rag-sources");
    expect(sources.textContent).toMatch(/contract\.txt/);
    expect(sources.textContent).toMatch(/91%/);
  });

  it("без включённого toggle rag-параметры не уходят на сервер", async () => {
    let capturedChatBody: unknown = null;
    const sse =
      'event: meta\ndata: {"decision":"allow_raw","risk":{"level":"low","score":0,"classes":[]},"provider":"local","reasons":[],"request_id":"r2"}\n\n' +
      'event: delta\ndata: {"content":"ok"}\n\n' +
      'event: done\ndata: {"request_id":"r2","assistant_message_id":"msg-2"}\n\n';
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        const method = init?.method ?? "GET";
        if (url.endsWith("/api/models")) {
          return Promise.resolve(
            jsonResponse([
              model({ name: "local", trust_level: "trusted_local" }),
            ]),
          );
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
        if (url.endsWith("/api/chat") && method === "POST") {
          capturedChatBody = JSON.parse(String(init?.body));
          return Promise.resolve(sseResponse(sse));
        }
        return Promise.resolve(jsonResponse({}));
      }),
    );

    renderChat();
    await screen.findByTestId("rag-toggle"); // дождёмся UI
    fireEvent.change(screen.getByLabelText("Сообщение"), {
      target: { value: "вопрос" },
    });
    fireEvent.keyDown(screen.getByLabelText("Сообщение"), { key: "Enter" });
    await screen.findByText("ok");

    const body = capturedChatBody as Record<string, unknown>;
    expect(body.rag).toBeUndefined();
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

  it("использует provider.default_model когда localStorage пуст", async () => {
    // Имитируем «свежего» пользователя: провайдер уже выбран автоматически
    // первым из списка, model в localStorage отсутствует.
    localStorage.setItem("rubezh.chat.provider", "claude-code-cli");
    let capturedChatBody: unknown = null;
    // trusted_local — обходит CloudGate (для теста default_model
    // нужно дойти прямо до doSend без preview-flow).
    const claude = model({
      name: "claude-code-cli",
      adapter: "ssh_cli",
      endpoint: "claude",
      trust_level: "trusted_local",
      default_model: "claude-opus-4-7",
    });
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        const method = init?.method ?? "GET";
        if (url.endsWith("/api/models")) {
          return Promise.resolve(jsonResponse([claude]));
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
        if (url.endsWith("/api/chat") && method === "POST") {
          capturedChatBody = JSON.parse(String(init?.body));
          return Promise.resolve(
            sseResponse('event: delta\ndata: {"content":"OK"}\n\n'),
          );
        }
        return Promise.resolve(jsonResponse({}));
      }),
    );

    renderChat();
    await screen.findByText("claude-code-cli");
    fireEvent.change(screen.getByLabelText("Сообщение"), {
      target: { value: "hi" },
    });
    fireEvent.keyDown(screen.getByLabelText("Сообщение"), { key: "Enter" });

    await waitFor(() =>
      expect(capturedChatBody).toMatchObject({ model: "claude-opus-4-7" }),
    );
  });

  it("для ssh_cli Codex отправляет рабочий model hint вместо старого alias", async () => {
    localStorage.setItem("rubezh.chat.provider", "codex-cli");
    localStorage.setItem("rubezh.chat.model", "gpt-5-codex");
    let capturedChatBody: unknown = null;
    const codex = model({
      name: "codex-cli",
      adapter: "ssh_cli",
      endpoint: "codex",
      trust_level: "trusted_local",
      default_model: "gpt-5.3-codex",
    });
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        const method = init?.method ?? "GET";
        if (url.endsWith("/api/models")) {
          return Promise.resolve(jsonResponse([codex]));
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
        if (url.endsWith("/api/chat") && method === "POST") {
          capturedChatBody = JSON.parse(String(init?.body));
          return Promise.resolve(
            sseResponse('event: delta\ndata: {"content":"OK"}\n\n'),
          );
        }
        return Promise.resolve(jsonResponse({}));
      }),
    );

    renderChat();
    await screen.findByText("codex-cli");
    fireEvent.change(screen.getByLabelText("Сообщение"), {
      target: { value: "тест" },
    });
    fireEvent.keyDown(screen.getByLabelText("Сообщение"), { key: "Enter" });

    await waitFor(() =>
      expect(capturedChatBody).toMatchObject({ model: "gpt-5.3-codex" }),
    );
  });

  it("режим ревизии отправляет две проверяющие ssh_cli модели", async () => {
    localStorage.setItem("rubezh.chat.provider", "codex-cli");
    let capturedChatBody: unknown = null;
    const codex = model({
      name: "codex-cli",
      adapter: "ssh_cli",
      endpoint: "codex",
      trust_level: "trusted_local",
      default_model: "gpt-5.3-codex",
    });
    const claude = model({
      id: "33333333-3333-3333-3333-333333333333",
      name: "claude-code-cli",
      adapter: "ssh_cli",
      endpoint: "claude",
      trust_level: "trusted_local",
      default_model: "claude-opus-4-7",
    });
    const grok = model({
      id: "44444444-4444-4444-4444-444444444444",
      name: "grok-build",
      adapter: "ssh_cli",
      endpoint: "grok-build",
      trust_level: "trusted_local",
      default_model: "grok-build",
    });
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        const method = init?.method ?? "GET";
        if (url.endsWith("/api/models")) {
          return Promise.resolve(jsonResponse([codex, claude, grok]));
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
        if (url.endsWith("/api/chat") && method === "POST") {
          capturedChatBody = JSON.parse(String(init?.body));
          return Promise.resolve(
            sseResponse('event: delta\ndata: {"content":"OK"}\n\n'),
          );
        }
        return Promise.resolve(jsonResponse({}));
      }),
    );

    renderChat();
    await screen.findByText("codex-cli");
    fireEvent.click(await screen.findByTestId("review-toggle"));
    fireEvent.change(screen.getByLabelText("Сообщение"), {
      target: { value: "проверь" },
    });
    fireEvent.keyDown(screen.getByLabelText("Сообщение"), { key: "Enter" });

    await waitFor(() =>
      expect(capturedChatBody).toMatchObject({
        review: {
          enabled: true,
          providers: ["claude-code-cli", "grok-build"],
        },
      }),
    );
  });

  it("окно промтов отправляет system_prompt для модели и ревизоров", async () => {
    localStorage.setItem("rubezh.chat.provider", "codex-cli");
    let capturedChatBody: unknown = null;
    const codex = model({
      name: "codex-cli",
      adapter: "ssh_cli",
      endpoint: "codex",
      trust_level: "trusted_local",
      default_model: "gpt-5.3-codex",
    });
    const claude = model({
      id: "33333333-3333-3333-3333-333333333333",
      name: "claude-code-cli",
      adapter: "ssh_cli",
      endpoint: "claude",
      trust_level: "trusted_local",
      default_model: "claude-opus-4-7",
    });
    const grok = model({
      id: "44444444-4444-4444-4444-444444444444",
      name: "grok-build",
      adapter: "ssh_cli",
      endpoint: "grok-build",
      trust_level: "trusted_local",
      default_model: "grok-build",
    });
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        const method = init?.method ?? "GET";
        if (url.endsWith("/api/models")) {
          return Promise.resolve(jsonResponse([codex, claude, grok]));
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
        if (url.endsWith("/api/chat") && method === "POST") {
          capturedChatBody = JSON.parse(String(init?.body));
          return Promise.resolve(
            sseResponse('event: delta\ndata: {"content":"OK"}\n\n'),
          );
        }
        return Promise.resolve(jsonResponse({}));
      }),
    );

    renderChat();
    await screen.findByText("codex-cli");
    fireEvent.click(screen.getByRole("button", { name: /Промты/i }));
    fireEvent.change(screen.getByLabelText("Системный промт модели 1"), {
      target: { value: "Основная модель пишет коротко" },
    });
    fireEvent.change(screen.getByLabelText("Системный промт модели 2"), {
      target: { value: "Вторая модель проверяет факты" },
    });
    fireEvent.change(screen.getByLabelText("Системный промт модели 3"), {
      target: { value: "Третья модель проверяет безопасность" },
    });
    fireEvent.change(screen.getByLabelText("Максимум циклов ревизии"), {
      target: { value: "4" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Готово/i }));
    fireEvent.click(await screen.findByTestId("review-toggle"));

    fireEvent.change(screen.getByLabelText("Сообщение"), {
      target: { value: "проверь промты" },
    });
    fireEvent.keyDown(screen.getByLabelText("Сообщение"), { key: "Enter" });

    await waitFor(() =>
      expect(capturedChatBody).toMatchObject({
        system_prompt: "Основная модель пишет коротко",
        review: {
          providers: ["claude-code-cli", "grok-build"],
          max_rounds: 4,
          system_prompts: {
            "claude-code-cli": "Вторая модель проверяет факты",
            "grok-build": "Третья модель проверяет безопасность",
          },
        },
      }),
    );
  });
});

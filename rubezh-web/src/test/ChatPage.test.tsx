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
    // picker без активного провайдера — это div, поэтому Send единственная кнопка
    expect(screen.getByRole("button")).toBeDisabled();
  });
});

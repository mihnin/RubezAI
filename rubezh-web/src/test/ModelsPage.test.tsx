import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import ModelsPage from "../pages/ModelsPage";
import { AuthProvider } from "../auth/context";

const PROVIDER = {
  id: "11111111-1111-1111-1111-111111111111",
  name: "OpenAI GPT",
  trust_level: "external",
  adapter: "openai_compatible",
  endpoint: "https://api.openai.com/v1",
  max_tokens: null,
  rate_limit_per_min: null,
  is_enabled: true,
  has_api_key: true,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

function loginAs(role: string) {
  localStorage.setItem("rubezh.auth.token", "TKN");
  localStorage.setItem(
    "rubezh.auth.user",
    JSON.stringify({ role, user_id: "u1", expires_at: "2099-01-01T00:00:00Z" }),
  );
}

function renderModels() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <ModelsPage />
        </AuthProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

/** jsonResponse — Response с JSON-телом и заданным статусом. */
function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("ModelsPage — toggle и delete (G.2)", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  it("admin видит провайдера и кнопку выключения; PATCH шлёт is_enabled=false", async () => {
    loginAs("admin");
    const calls: { url: string; method: string; body?: string }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        const method = init?.method ?? "GET";
        calls.push({ url, method, body: init?.body as string | undefined });
        if (url.endsWith("/api/models") && method === "GET") {
          return Promise.resolve(jsonResponse([PROVIDER]));
        }
        if (method === "PATCH") {
          return Promise.resolve(jsonResponse({ ...PROVIDER, is_enabled: false }));
        }
        return Promise.resolve(jsonResponse([PROVIDER]));
      }),
    );

    renderModels();
    expect(await screen.findByText("OpenAI GPT")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Выключить/i }));

    await waitFor(() => {
      const patch = calls.find((c) => c.method === "PATCH");
      expect(patch).toBeDefined();
      expect(patch!.url).toContain("/api/models/" + PROVIDER.id);
      expect(JSON.parse(patch!.body!)).toEqual({ is_enabled: false });
    });
  });

  it("при 409 на удаление показывает подсказку о выключении вместо удаления", async () => {
    loginAs("admin");
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        const method = init?.method ?? "GET";
        if (url.endsWith("/api/models") && method === "GET") {
          return Promise.resolve(jsonResponse([PROVIDER]));
        }
        if (method === "DELETE") {
          return Promise.resolve(
            jsonResponse({ error: "используется", suggestion: "disable" }, 409),
          );
        }
        return Promise.resolve(jsonResponse([PROVIDER]));
      }),
    );

    renderModels();
    await screen.findByText("OpenAI GPT");

    fireEvent.click(screen.getByRole("button", { name: /Удалить/i }));
    fireEvent.click(screen.getByRole("button", { name: /^Да$/ }));

    expect(
      await screen.findByText(/Удаление невозможно/i),
    ).toBeInTheDocument();
  });

  it("обычный user не видит кнопок управления провайдером", async () => {
    loginAs("user");
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(jsonResponse([PROVIDER]))),
    );

    renderModels();
    await screen.findByText("OpenAI GPT");

    expect(screen.queryByRole("button", { name: /Выключить/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /Удалить/i })).toBeNull();
  });
});

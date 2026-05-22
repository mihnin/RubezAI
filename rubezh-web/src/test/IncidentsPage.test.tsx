import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import IncidentsPage from "../pages/IncidentsPage";
import { AuthProvider } from "../auth/context";

const INCIDENT = {
  id: "22222222-2222-2222-2222-222222222222",
  audit_event_id: null,
  user_id: "u1",
  reporter_id: null,
  assignee_id: null,
  severity: "high" as const,
  status: "open" as const,
  trigger: "response_leak_detected",
  title: "Утечка ПДн в ответе",
  summary: "Обнаружен возможный leak",
  resolution: null,
  closed_at: null,
  created_at: "2026-05-01T10:00:00Z",
  updated_at: "2026-05-01T10:00:00.123456789Z",
};

function listBody() {
  return { incidents: [INCIDENT], next_cursor: null };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function renderIncidents() {
  localStorage.setItem("rubezh.auth.token", "TKN");
  localStorage.setItem(
    "rubezh.auth.user",
    JSON.stringify({
      role: "security_officer",
      user_id: "u1",
      expires_at: "2099-01-01T00:00:00Z",
    }),
  );
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <IncidentsPage />
        </AuthProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

describe("IncidentsPage — переходы статусов и If-Match (G.2c)", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  it("нетерминальный переход шлёт PATCH с If-Match и статусом", async () => {
    const calls: { url: string; method: string; ifMatch?: string; body?: string }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        const method = init?.method ?? "GET";
        if (url.includes("/api/incidents") && method === "GET") {
          return Promise.resolve(jsonResponse(listBody()));
        }
        const headers = new Headers(init?.headers);
        calls.push({
          url,
          method,
          ifMatch: headers.get("If-Match") ?? undefined,
          body: init?.body as string | undefined,
        });
        return Promise.resolve(jsonResponse({}, 200));
      }),
    );

    renderIncidents();
    await screen.findByText("Утечка ПДн в ответе");

    fireEvent.click(screen.getByRole("button", { name: /расследование/i }));

    await waitFor(() => {
      const patch = calls.find((c) => c.method === "PATCH");
      expect(patch).toBeDefined();
      expect(patch!.url).toContain("/api/incidents/" + INCIDENT.id);
      expect(patch!.ifMatch).toBe(INCIDENT.updated_at);
      expect(JSON.parse(patch!.body!)).toEqual({ status: "investigating" });
    });
  });

  it("терминальный переход открывает ResolutionDialog и требует резолюцию", async () => {
    const calls: { method: string; body?: string }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        const method = init?.method ?? "GET";
        if (url.includes("/api/incidents") && method === "GET") {
          return Promise.resolve(jsonResponse(listBody()));
        }
        calls.push({ method, body: init?.body as string | undefined });
        return Promise.resolve(jsonResponse({}, 200));
      }),
    );

    renderIncidents();
    await screen.findByText("Утечка ПДн в ответе");

    fireEvent.click(screen.getByRole("button", { name: /закрыт/i }));

    // диалог открыт; кнопка «Подтвердить» заблокирована без резолюции
    const confirm = await screen.findByRole("button", { name: /Подтвердить/i });
    expect(confirm).toBeDisabled();
    expect(calls.find((c) => c.method === "PATCH")).toBeUndefined();

    fireEvent.change(screen.getByPlaceholderText(/Опишите принятые меры/i), {
      target: { value: "Заблокирован пользователь, инцидент устранён" },
    });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);

    await waitFor(() => {
      const patch = calls.find((c) => c.method === "PATCH");
      expect(patch).toBeDefined();
      const parsed = JSON.parse(patch!.body!);
      expect(parsed.status).toBe("resolved");
      expect(parsed.resolution).toMatch(/Заблокирован/);
    });
  });

  it("ошибка 412 показывает подсказку о конфликте версий", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        const method = init?.method ?? "GET";
        if (url.includes("/api/incidents") && method === "GET") {
          return Promise.resolve(jsonResponse(listBody()));
        }
        return Promise.resolve(new Response("412 precondition failed", { status: 412 }));
      }),
    );

    renderIncidents();
    await screen.findByText("Утечка ПДн в ответе");

    fireEvent.click(screen.getByRole("button", { name: /расследование/i }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toMatch(/изменён другим пользователем/i);
  });
});

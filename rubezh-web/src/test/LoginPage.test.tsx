import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import LoginPage from "../pages/LoginPage";
import { AuthProvider } from "../auth/context";

function renderApp() {
  return render(
    <MemoryRouter initialEntries={["/login"]}>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/" element={<div data-testid="home">home</div>} />
        </Routes>
      </AuthProvider>
    </MemoryRouter>,
  );
}

describe("LoginPage smoke", () => {
  beforeEach(() => {
    localStorage.clear();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("после успешного входа сохраняет токен и перенаправляет", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            token: "TKN",
            role: "user",
            user_id: "uid-1",
            expires_at: "2099-01-01T00:00:00Z",
          }),
        ),
      ),
    );

    renderApp();
    fireEvent.click(screen.getByRole("button", { name: /Войти/i }));

    await waitFor(() =>
      expect(screen.getByTestId("home")).toBeInTheDocument(),
    );
    expect(localStorage.getItem("rubezh.auth.token")).toBe("TKN");
  });

  it("показывает ошибку при HTTP не-2xx", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("nope", { status: 500 })),
    );
    renderApp();
    fireEvent.click(screen.getByRole("button", { name: /Войти/i }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toMatch(/HTTP 500/);
    expect(localStorage.getItem("rubezh.auth.token")).toBeNull();
  });
});

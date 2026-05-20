import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { z } from "zod";
import { apiFetch, ApiError } from "../api/client";

const schema = z.object({ ok: z.boolean() });

describe("apiFetch", () => {
  beforeEach(() => {
    localStorage.clear();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("добавляет Authorization, когда токен в localStorage", async () => {
    localStorage.setItem("rubezh.auth.token", "T");
    const fetchMock = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ ok: true })));
    vi.stubGlobal("fetch", fetchMock);

    const out = await apiFetch("/api/x", schema);

    expect(out).toEqual({ ok: true });
    const headers = fetchMock.mock.calls[0][1].headers as Headers;
    expect(headers.get("authorization")).toBe("Bearer T");
  });

  it("не добавляет Authorization без токена", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true }))),
    );
    await apiFetch("/api/x", schema);
    const headers = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0][1]
      .headers as Headers;
    expect(headers.get("authorization")).toBeNull();
  });

  it("бросает ApiError при не-2xx", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("bad", { status: 500 })),
    );
    await expect(apiFetch("/x", schema)).rejects.toMatchObject({
      status: 500,
    });
  });

  it("при 401 чистит localStorage и редиректит", async () => {
    localStorage.setItem("rubezh.auth.token", "T");
    localStorage.setItem("rubezh.auth.user", "{}");
    // jsdom не поддерживает navigation; window.location.href = '...' просто пишет в свойство.
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("", { status: 401 })),
    );
    await expect(apiFetch("/x", schema)).rejects.toBeInstanceOf(ApiError);
    expect(localStorage.getItem("rubezh.auth.token")).toBeNull();
    expect(localStorage.getItem("rubezh.auth.user")).toBeNull();
  });

  it("отклоняет ответ, не соответствующий Zod-схеме", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(new Response(JSON.stringify({ ok: "yes" }))),
    );
    await expect(apiFetch("/x", schema)).rejects.toThrow();
  });
});

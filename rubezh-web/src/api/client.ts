import type { ZodType } from "zod";

/**
 * apiClient — fetch-обёртка с Authorization: Bearer + Zod-валидацией.
 * Token читается из localStorage (см. auth/context.tsx).
 * 401 → auto-logout (через redirect на /login через AuthContext).
 *
 * Все ответы валидируются по Zod-схеме, чтобы UI не доверял типу
 * без рантайм-проверки и падал детерминированно при расхождении.
 */

const STORAGE_TOKEN = "rubezh.auth.token";
const STORAGE_USER = "rubezh.auth.user";

export class ApiError extends Error {
  status: number;
  requestId: string | undefined;
  constructor(message: string, status: number, requestId?: string) {
    super(message);
    this.status = status;
    this.requestId = requestId;
  }
}

function authHeaders(extra?: HeadersInit): Headers {
  const token = localStorage.getItem(STORAGE_TOKEN);
  const h = new Headers(extra);
  if (token) h.set("Authorization", `Bearer ${token}`);
  return h;
}

function handle401(): never {
  localStorage.removeItem(STORAGE_TOKEN);
  localStorage.removeItem(STORAGE_USER);
  window.location.href = "/login";
  throw new ApiError("Unauthorized", 401);
}

async function rawFetch(path: string, options: RequestInit): Promise<Response> {
  const headers = authHeaders(options.headers);
  if (!headers.has("Content-Type") && options.body && !(options.body instanceof FormData)) {
    headers.set("Content-Type", "application/json");
  }
  const resp = await fetch(path, { ...options, headers });
  if (resp.status === 401) handle401();
  if (!resp.ok) {
    const text = await resp.text();
    const reqId = resp.headers.get("x-request-id") ?? undefined;
    throw new ApiError(text || resp.statusText, resp.status, reqId);
  }
  return resp;
}

export async function apiFetch<T>(
  path: string,
  schema: ZodType<T>,
  options: RequestInit = {},
): Promise<T> {
  const resp = await rawFetch(path, options);
  if (resp.status === 204) return undefined as T;
  const data = await resp.json();
  return schema.parse(data);
}

/** apiFetchRaw — для случаев когда тело не нужно валидировать (DELETE, PATCH без ответа). */
export async function apiFetchRaw(
  path: string,
  options: RequestInit = {},
): Promise<void> {
  await rawFetch(path, options);
}

/** apiDownload — скачивание blob с Bearer (CSV-экспорт и т.п.). */
export async function apiDownload(path: string, filename: string): Promise<void> {
  const resp = await rawFetch(path, { method: "GET" });
  const blob = await resp.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

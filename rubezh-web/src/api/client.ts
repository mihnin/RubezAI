/**
 * apiClient — fetch-обёртка с Authorization: Bearer.
 * Token читается из localStorage (см. auth/context.tsx).
 * 401 → auto-logout (через redirect на /login через AuthContext).
 */

const STORAGE_TOKEN = "rubezh.auth.token";

export class ApiError extends Error {
  status: number;
  requestId: string | undefined;
  constructor(message: string, status: number, requestId?: string) {
    super(message);
    this.status = status;
    this.requestId = requestId;
  }
}

export async function apiFetch<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const token = localStorage.getItem(STORAGE_TOKEN);
  const headers = new Headers(options.headers ?? {});
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (!headers.has("Content-Type") && options.body) {
    headers.set("Content-Type", "application/json");
  }
  const resp = await fetch(path, { ...options, headers });
  if (resp.status === 401) {
    // Auto-logout: токен невалиден.
    localStorage.removeItem(STORAGE_TOKEN);
    localStorage.removeItem("rubezh.auth.user");
    window.location.href = "/login";
    throw new ApiError("Unauthorized", 401);
  }
  if (!resp.ok) {
    const text = await resp.text();
    throw new ApiError(text || resp.statusText, resp.status);
  }
  if (resp.status === 204) return undefined as T;
  return (await resp.json()) as T;
}

import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { DevLoginResponseSchema } from "../api/schemas";

/**
 * Auth-контекст MVP. Token хранится в localStorage (см.
 * docs/design/identity.md §«MVP auth-flow»: localStorage + Bearer).
 * Замена на httpOnly cookie — пост-MVP вместе с OIDC.
 */

interface AuthUser {
  role: string;
  user_id: string;
  expires_at: string;
}

interface AuthContextValue {
  token: string | null;
  user: AuthUser | null;
  login(role: string): Promise<void>;
  logout(): void;
}

const STORAGE_TOKEN = "rubezh.auth.token";
const STORAGE_USER = "rubezh.auth.user";

const Ctx = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [token, setToken] = useState<string | null>(() =>
    localStorage.getItem(STORAGE_TOKEN),
  );
  const [user, setUser] = useState<AuthUser | null>(() => {
    const raw = localStorage.getItem(STORAGE_USER);
    return raw ? JSON.parse(raw) : null;
  });

  // Auto-logout при истёкшем токене.
  useEffect(() => {
    if (!user) return;
    const exp = new Date(user.expires_at).getTime();
    if (exp < Date.now()) {
      setToken(null);
      setUser(null);
      localStorage.removeItem(STORAGE_TOKEN);
      localStorage.removeItem(STORAGE_USER);
    }
  }, [user]);

  const login = useCallback(async (role: string) => {
    const resp = await fetch("/api/auth/dev-login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ role }),
    });
    if (!resp.ok) {
      throw new Error(`Не удалось войти: HTTP ${resp.status}`);
    }
    const data = DevLoginResponseSchema.parse(await resp.json());
    const nextUser: AuthUser = {
      role: data.role,
      user_id: data.user_id,
      expires_at: data.expires_at,
    };
    setToken(data.token);
    setUser(nextUser);
    localStorage.setItem(STORAGE_TOKEN, data.token);
    localStorage.setItem(STORAGE_USER, JSON.stringify(nextUser));
  }, []);

  const logout = useCallback(() => {
    setToken(null);
    setUser(null);
    localStorage.removeItem(STORAGE_TOKEN);
    localStorage.removeItem(STORAGE_USER);
  }, []);

  return (
    <Ctx.Provider value={{ token, user, login, logout }}>{children}</Ctx.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const v = useContext(Ctx);
  if (!v) throw new Error("useAuth: AuthProvider не настроен");
  return v;
}

import { Routes, Route, Navigate } from "react-router-dom";
import { useAuth } from "./auth/context";
import LoginPage from "./pages/LoginPage";
import HomePage from "./pages/HomePage";

/** Корневой router. /login публичный; всё остальное — за RequireAuth. */
export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/"
        element={
          <RequireAuth>
            <HomePage />
          </RequireAuth>
        }
      />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { token } = useAuth();
  if (!token) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

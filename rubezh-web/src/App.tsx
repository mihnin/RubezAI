import { Routes, Route, Navigate } from "react-router-dom";
import { useAuth } from "./auth/context";
import LoginPage from "./pages/LoginPage";
import ChatPage from "./pages/ChatPage";
import DocumentsPage from "./pages/DocumentsPage";
import PoliciesPage from "./pages/PoliciesPage";
import ModelsPage from "./pages/ModelsPage";
import AuditLogPage from "./pages/AuditLogPage";
import IncidentsPage from "./pages/IncidentsPage";
import HelpPage from "./pages/HelpPage";
import AppLayout from "./components/AppLayout";

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        element={
          <RequireAuth>
            <AppLayout />
          </RequireAuth>
        }
      >
        <Route path="/" element={<Navigate to="/chat" replace />} />
        <Route path="/chat" element={<ChatPage />} />
        <Route path="/documents" element={<DocumentsPage />} />
        <Route path="/policies" element={<PoliciesPage />} />
        <Route path="/models" element={<ModelsPage />} />
        <Route path="/audit-log" element={<AuditLogPage />} />
        <Route path="/incidents" element={<IncidentsPage />} />
        <Route path="/help" element={<HelpPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { token } = useAuth();
  if (!token) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

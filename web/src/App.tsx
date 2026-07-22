import { Navigate, Route, Routes } from 'react-router-dom';
import { useEffect } from 'react';
import { Loader2 } from 'lucide-react';
import AppLayout from './components/layout/AppLayout';
import OnboardingPage from './pages/Onboarding';
import DashboardPage from './pages/Dashboard';
import DockerPage from './pages/Docker';
import StoragePage from './pages/Storage';
import FilesPage from './pages/Files';
import TerminalPage from './pages/Terminal';
import VmsPage from './pages/Vms';
import SettingsPage from './pages/Settings';
import LoginPage from './pages/Login';
import { useAuthStore } from './stores/auth';
import { useSettingsStore } from './stores/settings';

export default function App() {
  const server = useAuthStore((s) => s.server);
  const onboardingDone = useSettingsStore((s) => s.onboardingDone);
  const uiAuthEnabled = useAuthStore((s) => s.uiAuthEnabled);
  const isUiAuthenticated = useAuthStore((s) => s.isUiAuthenticated);
  const authChecked = useAuthStore((s) => s.authChecked);
  const checkAuth = useAuthStore((s) => s.checkAuth);

  // Probe auth status on boot to determine if login is required.
  useEffect(() => {
    checkAuth();
  }, [checkAuth]);

  // Apply collapsed-sidebar class to <html> so Tailwind can react to it.
  const collapsed = useSettingsStore((s) => s.sidebarCollapsed);
  useEffect(() => {
    document.documentElement.classList.toggle('sidebar-collapsed', collapsed);
  }, [collapsed]);

  // Wait for the auth probe before rendering any routes. This prevents
  // a flash of the login page (or the main app) before we know whether
  // auth is enabled.
  if (!authChecked) {
    return (
      <div className="flex min-h-screen items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" /> 加载中…
      </div>
    );
  }

  const needsLogin = uiAuthEnabled && !isUiAuthenticated;

  return (
    <Routes>
      {/* Login page — standalone, outside the sidebar layout. */}
      <Route
        path="/login"
        element={needsLogin ? <LoginPage /> : <Navigate to="/" replace />}
      />

      {/* First-time wizard — bypasses the main layout.
          Requires auth if UI auth is enabled. */}
      <Route
        path="/onboarding"
        element={
          needsLogin ? (
            <Navigate to="/login" replace />
          ) : server && onboardingDone ? (
            <Navigate to="/" replace />
          ) : (
            <OnboardingPage />
          )
        }
      />

      {/* Everything else sits inside the sidebar layout. */}
      <Route
        path="/*"
        element={
          needsLogin ? (
            <Navigate to="/login" replace />
          ) : server ? (
            <AppLayout />
          ) : (
            <Navigate to="/onboarding" replace />
          )
        }
      >
        <Route index element={<DashboardPage />} />
        <Route path="docker" element={<DockerPage />} />
        <Route path="storage" element={<StoragePage />} />
        <Route path="files" element={<FilesPage />} />
        <Route path="terminal" element={<TerminalPage />} />
        <Route path="vms" element={<VmsPage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}

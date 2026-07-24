import { Navigate, Route, Routes } from 'react-router-dom';
import { useEffect } from 'react';
import { Loader2 } from 'lucide-react';
import AppLayout from './components/layout/AppLayout';
import { ErrorBoundary } from './components/ErrorBoundary';
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
import { useTranslation } from 'react-i18next';

export default function App() {
  const server = useAuthStore((s) => s.server);
  const servers = useAuthStore((s) => s.servers);
  const uiAuthEnabled = useAuthStore((s) => s.uiAuthEnabled);
  const isUiAuthenticated = useAuthStore((s) => s.isUiAuthenticated);
  const authChecked = useAuthStore((s) => s.authChecked);
  const checkAuth = useAuthStore((s) => s.checkAuth);

  const { t } = useTranslation();

  // Probe auth status + servers on boot, then keep transport flags fresh.
  const refreshServers = useAuthStore((s) => s.refreshServers);
  useEffect(() => {
    checkAuth();
  }, [checkAuth]);
  useEffect(() => {
    const id = window.setInterval(() => {
      void refreshServers();
    }, 15000);
    return () => window.clearInterval(id);
  }, [refreshServers]);

  // Apply collapsed-sidebar class to <html> so Tailwind can react to it.
  const collapsed = useSettingsStore((s) => s.sidebarCollapsed);
  useEffect(() => {
    document.documentElement.classList.toggle('sidebar-collapsed', collapsed);
  }, [collapsed]);

  // Apply theme via data-theme attribute on <html>.
  const theme = useSettingsStore((s) => s.theme);
  useEffect(() => {
    const html = document.documentElement;
    html.setAttribute('data-theme', theme);
    // Remove the old dark class — theming is now data-theme driven
    html.classList.remove('dark');
    // Update theme-color meta tag
    const meta = document.querySelector('meta[name="theme-color"]');
    if (meta) {
      const color = getComputedStyle(html).getPropertyValue('--theme-color').trim();
      if (color) meta.setAttribute('content', color);
    }
  }, [theme]);

  // Wait for the auth probe before rendering any routes.
  if (!authChecked) {
    return (
      <div className="flex min-h-screen items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" /> {t('common.loading')}
      </div>
    );
  }

  const needsLogin = uiAuthEnabled && !isUiAuthenticated;

  // v0.8+: If backend has saved servers with active connections, the user
  // goes straight to the dashboard. If no servers at all, go to onboarding.
  // If servers exist but none connected, still show main app (user can
  // reconnect from the server list).
  const hasServers = servers.length > 0;
  const showApp = server || hasServers;

  return (
    <Routes>
      {/* Login page */}
      <Route
        path="/login"
        element={needsLogin ? <LoginPage /> : <Navigate to="/" replace />}
      />

      {/* Onboarding wizard — always accessible (user may add more servers) */}
      <Route
        path="/onboarding"
        element={
          needsLogin ? (
            <Navigate to="/login" replace />
          ) : (
            <OnboardingPage />
          )
        }
      />

      {/* Main app */}
      <Route
        path="/*"
        element={
          needsLogin ? (
            <Navigate to="/login" replace />
          ) : showApp ? (
            <ErrorBoundary>
              <AppLayout />
            </ErrorBoundary>
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

import { Navigate, Route, Routes } from 'react-router-dom';
import { useEffect } from 'react';
import AppLayout from './components/layout/AppLayout';
import OnboardingPage from './pages/Onboarding';
import DashboardPage from './pages/Dashboard';
import DockerPage from './pages/Docker';
import StoragePage from './pages/Storage';
import FilesPage from './pages/Files';
import TerminalPage from './pages/Terminal';
import VmsPage from './pages/Vms';
import SettingsPage from './pages/Settings';
import { useAuthStore } from './stores/auth';
import { useSettingsStore } from './stores/settings';

export default function App() {
  const server = useAuthStore((s) => s.server);
  const onboardingDone = useSettingsStore((s) => s.onboardingDone);

  // Apply collapsed-sidebar class to <html> so Tailwind can react to it.
  const collapsed = useSettingsStore((s) => s.sidebarCollapsed);
  useEffect(() => {
    document.documentElement.classList.toggle('sidebar-collapsed', collapsed);
  }, [collapsed]);

  return (
    <Routes>
      {/* First-time wizard — bypasses the main layout. */}
      <Route
        path="/onboarding"
        element={
          server && onboardingDone ? <Navigate to="/" replace /> : <OnboardingPage />
        }
      />

      {/* Everything else sits inside the sidebar layout. */}
      <Route
        path="/*"
        element={
          server ? (
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

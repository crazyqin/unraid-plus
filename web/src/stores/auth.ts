import { create } from 'zustand';
import { api } from '@/lib/api';
import type { ServerConfig, ServerStatus } from '@/types';

interface AuthState {
  /** Currently connected server (null before onboarding). */
  server: ServerConfig | null;
  /** Whether onboarding has been completed at least once. */
  isConfigured: boolean;

  // --- UI Authentication (v0.5) ---
  /** Whether the backend has UNRAIDPP_UI_PASSWORD set. */
  uiAuthEnabled: boolean;
  /** Whether the current browser session is authenticated. */
  isUiAuthenticated: boolean;
  /** Whether checkAuth() has been called at least once (prevents login flash). */
  authChecked: boolean;

  setServer: (s: ServerConfig | null) => void;
  setStatus: (status: ServerStatus) => void;
  configure: (s: ServerConfig) => void;
  reset: () => void;

  /** Probe /api/auth/status on boot to determine if login is required. */
  checkAuth: () => Promise<void>;
  /** Attempt to log in with a password. Returns true on success. */
  login: (password: string) => Promise<boolean>;
  /** Log out the current session. */
  logout: () => Promise<void>;
}

export const useAuthStore = create<AuthState>((set) => ({
  server: null,
  isConfigured: false,

  uiAuthEnabled: false,
  isUiAuthenticated: true, // optimistic: assume authenticated until proven otherwise
  authChecked: false,

  setServer: (server) => set({ server }),
  setStatus: (status) =>
    set((state) =>
      state.server ? { server: { ...state.server, status } } : state,
    ),
  configure: (s) => set({ server: s, isConfigured: true }),
  reset: () =>
    set({
      server: null,
      isConfigured: false,
      uiAuthEnabled: false,
      isUiAuthenticated: true,
      authChecked: false,
    }),

  checkAuth: async () => {
    try {
      const res = await api.get<{ enabled: boolean; authenticated: boolean }>(
        '/auth/status',
      );
      set({
        uiAuthEnabled: res.enabled,
        isUiAuthenticated: res.enabled ? res.authenticated : true,
        authChecked: true,
      });
    } catch {
      // If the probe fails (backend down, network error), assume no auth
      // so the user can at least see the onboarding/connection error.
      set({ uiAuthEnabled: false, isUiAuthenticated: true, authChecked: true });
    }
  },

  login: async (password: string) => {
    const res = await api.post<{ ok: boolean; enabled?: boolean; message?: string }>(
      '/auth/login',
      { password },
    );
    if (res.ok) {
      set({ isUiAuthenticated: true });
      return true;
    }
    return false;
  },

  logout: async () => {
    try {
      await api.post('/auth/logout');
    } catch {
      // ignore — session may already be expired
    }
    set({ isUiAuthenticated: false });
  },
}));

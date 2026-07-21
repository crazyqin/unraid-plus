import { create } from 'zustand';
import type { ServerConfig, ServerStatus } from '@/types';

interface AuthState {
  /** Currently connected server (null before onboarding). */
  server: ServerConfig | null;
  /** Whether onboarding has been completed at least once. */
  isConfigured: boolean;

  setServer: (s: ServerConfig | null) => void;
  setStatus: (status: ServerStatus) => void;
  configure: (s: ServerConfig) => void;
  reset: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  server: null,
  isConfigured: false,

  setServer: (server) => set({ server }),
  setStatus: (status) =>
    set((state) =>
      state.server ? { server: { ...state.server, status } } : state,
    ),
  configure: (s) => set({ server: s, isConfigured: true }),
  reset: () => set({ server: null, isConfigured: false }),
}));

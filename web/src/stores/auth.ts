import { create } from 'zustand';
import { api, setActiveServerId } from '@/lib/api';
import type { ServerConfig, ServerInfo, ServerStatus } from '@/types';

interface AuthState {
  /** Currently selected server (null before any server is connected). */
  server: ServerConfig | null;
  /** Whether onboarding has been completed at least once. */
  isConfigured: boolean;

  // v0.8+: Multi-server support
  /** All saved servers from backend (GET /api/servers). */
  servers: ServerInfo[];
  /** Currently active server ID (for multi-server). */
  activeServerId: string | null;

  // Connection mode — default FALSE so we never flash "dual" before probe.
  /** Whether the active server has SSH available (terminal + SFTP). */
  sshAvailable: boolean;
  /** Whether the active server has WebGUI API session active. */
  apiAvailable: boolean;

  // --- UI Authentication (v0.5) ---
  uiAuthEnabled: boolean;
  isUiAuthenticated: boolean;
  authChecked: boolean;

  setServer: (s: ServerConfig | null) => void;
  setStatus: (status: ServerStatus) => void;
  setConnectionMode: (ssh: boolean, api: boolean) => void;
  configure: (s: ServerConfig, mode?: { sshAvailable?: boolean; apiAvailable?: boolean }) => void;
  setServers: (servers: ServerInfo[]) => void;
  setActiveServerId: (id: string | null) => void;
  reset: () => void;

  /** Probe /api/auth/status + /api/servers on boot. */
  checkAuth: () => Promise<void>;
  /** Refresh the server list from backend. */
  refreshServers: () => Promise<void>;
  /** Select a server by ID, updating the `server` config. */
  selectServer: (id: string) => void;
  login: (password: string) => Promise<boolean>;
  logout: () => Promise<void>;
}

function isServerOnline(s: ServerInfo): boolean {
  return !!(s.connected || s.sshAvailable || s.apiAvailable);
}

export const useAuthStore = create<AuthState>((set, get) => ({
  server: null,
  isConfigured: false,
  servers: [],
  activeServerId: null,
  // Must start false — true defaults caused false "dual channel" until manual select.
  sshAvailable: false,
  apiAvailable: false,

  uiAuthEnabled: false,
  isUiAuthenticated: true,
  authChecked: false,

  setServer: (server) => set({ server }),
  setStatus: (status) =>
    set((state) =>
      state.server ? { server: { ...state.server, status } } : state,
    ),
  setConnectionMode: (ssh, api) => set({ sshAvailable: ssh, apiAvailable: api }),
  configure: (s, mode) => {
    set({
      server: s,
      isConfigured: true,
      activeServerId: s.id ?? null,
      sshAvailable: mode?.sshAvailable ?? false,
      apiAvailable: mode?.apiAvailable ?? false,
    });
    if (s.id) setActiveServerId(s.id);
  },
  setServers: (servers) => set({ servers }),
  setActiveServerId: (id) => set({ activeServerId: id }),
  reset: () =>
    set({
      server: null,
      isConfigured: false,
      servers: [],
      activeServerId: null,
      uiAuthEnabled: false,
      isUiAuthenticated: true,
      authChecked: false,
      sshAvailable: false,
      apiAvailable: false,
    }),

  checkAuth: async () => {
    try {
      const res = await api.get<{ enabled: boolean; authenticated: boolean }>(
        '/auth/status',
      );
      set({
        uiAuthEnabled: res.enabled,
        isUiAuthenticated: res.enabled ? res.authenticated : true,
      });
    } catch {
      set({ uiAuthEnabled: false, isUiAuthenticated: true });
    }
    // v0.8+: Also fetch saved servers from backend.
    // CRITICAL: set authChecked AFTER refreshServers so that the route
    // guard doesn't evaluate showApp=false before servers are loaded.
    await get().refreshServers();
    set({ authChecked: true });
  },

  refreshServers: async () => {
    try {
      const res = await api.get<{ servers: ServerInfo[] }>('/servers');
      const servers = res.servers || [];
      set({ servers });

      const currentActiveId = get().activeServerId;
      const online = servers.find((s) => isServerOnline(s));

      if (!currentActiveId && online) {
        // Auto-select first online server (API-only or SSH or dual)
        get().selectServer(online.id);
      } else if (currentActiveId) {
        // Refresh the selected server's connected status + transport flags
        const current = servers.find((s) => s.id === currentActiveId);
        if (current) {
          set({
            server: {
              host: current.host,
              sshPort: current.port,
              user: current.user,
              authMode: (current.authMode === 'key' ? 'key' : 'password') as 'password' | 'key',
              status: isServerOnline(current) ? 'connected' : 'disconnected',
              label: current.label || current.host,
              id: current.id,
            },
            sshAvailable: !!current.sshAvailable,
            apiAvailable: !!current.apiAvailable,
          });
          setActiveServerId(currentActiveId);
        }
      }

      // If we have any servers, mark as configured
      if (servers.length > 0) {
        set({ isConfigured: true });
      }
    } catch {
      // Backend may not support /api/servers yet (pre-v0.8)
    }
  },

  selectServer: (id: string) => {
    if (!id) {
      set({
        activeServerId: null,
        server: null,
        sshAvailable: false,
        apiAvailable: false,
      });
      setActiveServerId(null);
      return;
    }
    const servers = get().servers;
    const info = servers.find((s) => s.id === id);
    if (!info) return;

    set({
      activeServerId: id,
      server: {
        host: info.host,
        sshPort: info.port,
        user: info.user,
        authMode: (info.authMode === 'key' ? 'key' : 'password') as 'password' | 'key',
        status: isServerOnline(info) ? 'connected' : 'disconnected',
        label: info.label || info.host,
        id: info.id,
      },
      isConfigured: true,
      sshAvailable: !!info.sshAvailable,
      apiAvailable: !!info.apiAvailable,
    });
    // Sync server ID to API client for data requests
    setActiveServerId(id);
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
      // ignore
    }
    set({ isUiAuthenticated: false });
  },
}));

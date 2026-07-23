import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type ChartRange = '60s' | '5m' | '30m' | '2h';

interface SettingsState {
  /** Whether the user already walked through the first-time guide. */
  onboardingDone: boolean;
  setOnboardingDone: (done: boolean) => void;

  /** Auto-refresh interval (ms) for live dashboards. */
  refreshInterval: number;
  setRefreshInterval: (ms: number) => void;

  /** Default chart history window. */
  chartRange: ChartRange;
  setChartRange: (r: ChartRange) => void;

  /** Sidebar collapsed state for the layout. */
  sidebarCollapsed: boolean;
  toggleSidebar: () => void;
}

export const useSettingsStore = create<SettingsState>()(
  persist(
    (set) => ({
      onboardingDone: false,
      setOnboardingDone: (done) => set({ onboardingDone: done }),

      refreshInterval: 2000,
      setRefreshInterval: (ms) => set({ refreshInterval: ms }),

      chartRange: '5m',
      setChartRange: (r) => set({ chartRange: r }),

      sidebarCollapsed: false,
      toggleSidebar: () =>
        set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
    }),
    { name: 'unraidpp-settings' },
  ),
);

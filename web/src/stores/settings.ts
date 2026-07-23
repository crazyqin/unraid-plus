import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type ChartRange = '60s' | '5m' | '30m' | '2h';

export type ThemeId = 'midnight' | 'ocean' | 'aurora' | 'sunset' | 'daylight';

export interface ThemeMeta {
  id: ThemeId;
  label: string;
  desc: string;
  accent: string; // CSS class for the preview swatch
}

export const THEMES: ThemeMeta[] = [
  { id: 'midnight', label: 'themes.midnight', desc: 'themes.midnightDesc', accent: 'bg-orange-500' },
  { id: 'ocean',    label: 'themes.ocean',    desc: 'themes.oceanDesc',    accent: 'bg-cyan-500' },
  { id: 'aurora',   label: 'themes.aurora',   desc: 'themes.auroraDesc',   accent: 'bg-emerald-500' },
  { id: 'sunset',   label: 'themes.sunset',   desc: 'themes.sunsetDesc',   accent: 'bg-amber-500' },
  { id: 'daylight', label: 'themes.daylight', desc: 'themes.daylightDesc', accent: 'bg-blue-500' },
];

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

  /** Active theme ID. */
  theme: ThemeId;
  setTheme: (t: ThemeId) => void;
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

      theme: 'midnight',
      setTheme: (t) => set({ theme: t }),
    }),
    { name: 'unraidpp-settings' },
  ),
);

import { useQuery } from '@tanstack/react-query';
import { Activity, Moon, RefreshCw, Sun, Wifi, WifiOff } from 'lucide-react';
import { useRef, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { useSettingsStore, THEMES } from '@/stores/settings';
import { useAuthStore } from '@/stores/auth';
import { cn } from '@/lib/utils';

export default function TopBar() {
  const server = useAuthStore((s) => s.server);
  const refreshInterval = useSettingsStore((s) => s.refreshInterval);
  const setRefreshInterval = useSettingsStore((s) => s.setRefreshInterval);
  const theme = useSettingsStore((s) => s.theme);
  const setTheme = useSettingsStore((s) => s.setTheme);

  const [themeOpen, setThemeOpen] = useState(false);
  const panelRef = useRef<HTMLDivElement>(null);

  const health = useQuery({
    queryKey: ['health'],
    queryFn: async () => {
      const res = await fetch('/health', { credentials: 'include' });
      if (!res.ok) throw new Error('health check failed');
      return res.json() as Promise<{ ok: boolean; uptime: number }>;
    },
    refetchInterval: refreshInterval,
  });

  const online = health.data?.ok ?? false;

  // Close theme panel on outside click
  // (blur is handled by the panel's onBlur)

  const currentTheme = THEMES.find((t) => t.id === theme);

  return (
    <header className="flex h-14 items-center justify-between border-b bg-card/40 px-4 backdrop-blur">
      <div className="flex items-center gap-3">
        <div
          className={cn(
            'flex items-center gap-2 rounded-full border px-3 py-1 text-xs font-medium',
            online
              ? 'border-success/40 bg-success/10 text-success'
              : 'border-destructive/40 bg-destructive/10 text-destructive',
          )}
        >
          {online ? (
            <Wifi className="h-3.5 w-3.5" />
          ) : (
            <WifiOff className="h-3.5 w-3.5" />
          )}
          {online ? '在线' : '离线'}
        </div>
        <div className="hidden items-center gap-2 text-xs text-muted-foreground sm:flex">
          <Activity className="h-3.5 w-3.5" />
          <span>刷新间隔</span>
          <select
            className="rounded border bg-background px-1.5 py-0.5 text-xs"
            value={refreshInterval}
            onChange={(e) => setRefreshInterval(Number(e.target.value))}
          >
            <option value={1000}>1s</option>
            <option value={2000}>2s</option>
            <option value={5000}>5s</option>
            <option value={15000}>15s</option>
            <option value={0}>暂停</option>
          </select>
        </div>
        <span className="max-w-[200px] truncate text-xs text-muted-foreground" title={server?.label || server?.host}>
          {server?.label || server?.host}
        </span>
      </div>

      <div className="flex items-center gap-2">
        {/* Theme quick switch */}
        <div className="relative" ref={panelRef}>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => setThemeOpen((v) => !v)}
                title="切换主题"
              >
                {theme === 'daylight' ? (
                  <Sun className="h-4 w-4" />
                ) : (
                  <Moon className="h-4 w-4" />
                )}
              </Button>
            </TooltipTrigger>
            <TooltipContent>主题：{currentTheme?.label}</TooltipContent>
          </Tooltip>

          {themeOpen && (
            <>
              {/* Backdrop to close on outside click */}
              <div className="fixed inset-0 z-40" onClick={() => setThemeOpen(false)} />
              <div
                className="absolute right-0 top-full z-50 mt-2 w-52 animate-fade-in rounded-lg border bg-card p-2 shadow-xl"
                onBlur={(e) => {
                  if (!e.currentTarget.contains(e.relatedTarget)) setThemeOpen(false);
                }}
              >
                <div className="mb-1.5 px-2 text-[11px] font-medium text-muted-foreground">
                  主题风格
                </div>
                <div className="space-y-0.5">
                  {THEMES.map((t) => (
                    <button
                      key={t.id}
                      onClick={() => { setTheme(t.id); setThemeOpen(false); }}
                      className={cn(
                        'flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left text-sm transition-colors',
                        theme === t.id
                          ? 'bg-primary/10 text-primary'
                          : 'text-foreground hover:bg-accent',
                      )}
                    >
                      <div className={`h-4 w-4 rounded-full ${t.accent} shrink-0 shadow-sm`} />
                      <div className="min-w-0 flex-1">
                        <div className="text-xs font-medium leading-tight">{t.label}</div>
                        <div className="text-[10px] text-muted-foreground leading-tight">{t.desc}</div>
                      </div>
                      {theme === t.id && (
                        <div className="h-1.5 w-1.5 rounded-full bg-primary" />
                      )}
                    </button>
                  ))}
                </div>
              </div>
            </>
          )}
        </div>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => health.refetch()}
              title="刷新"
            >
              <RefreshCw
                className={cn('h-4 w-4', health.isFetching && 'animate-spin')}
              />
            </Button>
          </TooltipTrigger>
          <TooltipContent>立即刷新</TooltipContent>
        </Tooltip>
      </div>
    </header>
  );
}

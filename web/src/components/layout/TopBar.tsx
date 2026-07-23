import { useQuery } from '@tanstack/react-query';
import { Activity, RefreshCw, Wifi, WifiOff } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { useSettingsStore } from '@/stores/settings';
import { useAuthStore } from '@/stores/auth';
import { cn } from '@/lib/utils';

export default function TopBar() {
  const server = useAuthStore((s) => s.server);
  const refreshInterval = useSettingsStore((s) => s.refreshInterval);
  const setRefreshInterval = useSettingsStore((s) => s.setRefreshInterval);

  const health = useQuery({
    queryKey: ['health'],
    queryFn: async () => {
      // /health is registered outside /api group, so we fetch directly
      const res = await fetch('/health', { credentials: 'include' });
      if (!res.ok) throw new Error('health check failed');
      return res.json() as Promise<{ ok: boolean; uptime: number }>;
    },
    refetchInterval: refreshInterval,
  });

  const online = health.data?.ok ?? false;

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
        <span className="text-xs text-muted-foreground">
          {server?.label || server?.host}
        </span>
      </div>

      <div className="flex items-center gap-2">
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

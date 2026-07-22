import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Boxes,
  Cpu,
  Loader2,
  MemoryStick,
  Network,
  Pause,
  Play,
  RotateCw,
  ScrollText,
  Square,
  Search,
} from 'lucide-react';
import { api, wsUrl } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Progress } from '@/components/ui/progress';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { cn, formatBytes, timeAgo, truncate } from '@/lib/utils';
import { useSettingsStore } from '@/stores/settings';
import type { ContainerStats, DockerContainer } from '@/types';

const STATUS_VARIANT: Record<DockerContainer['status'], 'success' | 'secondary' | 'warning' | 'destructive'> = {
  running: 'success',
  exited: 'secondary',
  paused: 'warning',
  restarting: 'warning',
  created: 'secondary',
  dead: 'destructive',
};

const STATUS_LABEL: Record<string, string> = {
  running: '运行中',
  exited: '已退出',
  paused: '已暂停',
  restarting: '重启中',
  created: '已创建',
  dead: '已停止',
};

export default function DockerPage() {
  const refresh = useSettingsStore((s) => s.refreshInterval);
  const qc = useQueryClient();
  const [filter, setFilter] = useState('');
  const [logsFor, setLogsFor] = useState<DockerContainer | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ['docker'],
    queryFn: () => api.get<DockerContainer[]>('/docker/containers'),
    refetchInterval: refresh || false,
  });

  // Resource stats — polled at the same interval as the container list.
  // The backend caches stats for 3s so this is cheap even at 1s polling.
  const { data: statsData } = useQuery({
    queryKey: ['docker-stats'],
    queryFn: () => api.get<ContainerStats[]>('/docker/stats'),
    refetchInterval: refresh || false,
  });

  const statsMap = new Map<string, ContainerStats>();
  (statsData ?? []).forEach((s) => statsMap.set(s.id, s));

  const act = async (id: string, action: 'start' | 'stop' | 'restart' | 'pause') => {
    await api.post(`/docker/containers/${id}/${action}`);
    qc.invalidateQueries({ queryKey: ['docker'] });
  };

  const containers = (data ?? []).filter((c) =>
    c.name.toLowerCase().includes(filter.toLowerCase()),
  );
  const running = (data ?? []).filter((c) => c.status === 'running').length;

  return (
    <div className="space-y-4 p-4 md:p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Docker 容器</h1>
          <p className="text-sm text-muted-foreground">
            {running} 个运行中 / {data?.length ?? 0} 个总计
          </p>
        </div>
        <div className="relative">
          <Search className="absolute left-2 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="pl-8"
            placeholder="搜索容器名…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        </div>
      </div>

      {isLoading ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" /> 加载容器列表…
        </div>
      ) : containers.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center gap-2 py-12 text-center text-sm text-muted-foreground">
            <Boxes className="h-8 w-8" />
            没有匹配的容器。
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {containers.map((c) => (
            <Card key={c.id}>
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <CardTitle className="truncate text-sm">
                      {c.name}
                    </CardTitle>
                    <div className="truncate text-xs text-muted-foreground">
                      {truncate(c.image, 38)}
                    </div>
                  </div>
                  <Badge variant={STATUS_VARIANT[c.status]}>{STATUS_LABEL[c.status] ?? c.status}</Badge>
                </div>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="flex flex-wrap gap-1">
                  {c.ports.slice(0, 3).map((p) => (
                    <Badge key={p} variant="outline" className="font-mono text-[10px]">
                      {p}
                    </Badge>
                  ))}
                  {c.ports.length > 3 && (
                    <Badge variant="outline" className="text-[10px]">
                      +{c.ports.length - 3}
                    </Badge>
                  )}
                </div>

                {/* Resource stats (only for running containers) */}
                {c.status === 'running' && statsMap.get(c.id) && (
                  <ContainerStatsBar stats={statsMap.get(c.id)!} />
                )}

                <div className="text-xs text-muted-foreground">
                  启动于 {c.startedAt ? timeAgo(c.startedAt) : '—'}
                </div>
                <div className="flex flex-wrap gap-2">
                  {c.status !== 'running' && (
                    <Button size="sm" variant="success" onClick={() => act(c.id, 'start')}>
                      <Play className="h-3.5 w-3.5" /> 启动
                    </Button>
                  )}
                  {c.status === 'running' && (
                    <Button size="sm" variant="destructive" onClick={() => act(c.id, 'stop')}>
                      <Square className="h-3.5 w-3.5" /> 停止
                    </Button>
                  )}
                  <Button size="sm" variant="outline" onClick={() => act(c.id, 'restart')}>
                    <RotateCw className="h-3.5 w-3.5" /> 重启
                  </Button>
                  {c.status === 'running' && (
                    <Button size="sm" variant="ghost" onClick={() => act(c.id, 'pause')}>
                      <Pause className="h-3.5 w-3.5" /> 暂停
                    </Button>
                  )}
                  <Button size="sm" variant="ghost" onClick={() => setLogsFor(c)}>
                    <ScrollText className="h-3.5 w-3.5" /> 日志
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <LogDialog container={logsFor} onClose={() => setLogsFor(null)} />
    </div>
  );
}

/* --------------------------- Container Stats Bar -------------------------- */

/**
 * Compact resource-usage display rendered inside each running container's
 * card. Shows CPU%, memory usage with a progress bar, and network I/O.
 *
 * Hidden for non-running containers because `docker stats` only reports
 * on active containers — showing zeros would be misleading.
 */
function ContainerStatsBar({ stats }: { stats: ContainerStats }) {
  const memPct = stats.memLimitBytes > 0
    ? (stats.memUsageBytes / stats.memLimitBytes) * 100
    : 0;

  return (
    <div className="space-y-1.5 rounded-md border bg-muted/30 p-2">
      {/* CPU */}
      <div className="flex items-center gap-2 text-xs">
        <Cpu className="h-3 w-3 shrink-0 text-muted-foreground" />
        <Progress
          className="h-1.5 flex-1"
          value={Math.min(stats.cpuPct, 100)}
        />
        <span className="shrink-0 tabular-nums text-muted-foreground">
          {stats.cpuPct.toFixed(1)}%
        </span>
      </div>
      {/* Memory */}
      <div className="flex items-center gap-2 text-xs">
        <MemoryStick className="h-3 w-3 shrink-0 text-muted-foreground" />
        <Progress
          className="h-1.5 flex-1"
          value={memPct}
          indicatorClassName={
            memPct > 90 ? 'bg-destructive' : memPct > 75 ? 'bg-warning' : 'bg-primary'
          }
        />
        <span className="shrink-0 tabular-nums text-muted-foreground">
          {formatBytes(stats.memUsageBytes)} / {formatBytes(stats.memLimitBytes)}
        </span>
      </div>
      {/* Network I/O */}
      <div className="flex items-center gap-2 text-[10px] text-muted-foreground">
        <Network className="h-3 w-3 shrink-0" />
        <span className="tabular-nums">
          ↓ {formatBytes(stats.netRxBytes)} · ↑ {formatBytes(stats.netTxBytes)}
        </span>
        <span className="ml-auto tabular-nums">
          {stats.pids} PIDs
        </span>
      </div>
    </div>
  );
}

/* ------------------------------- Log Dialog ------------------------------- */

function LogDialog({
  container,
  onClose,
}: {
  container: DockerContainer | null;
  onClose: () => void;
}) {
  return (
    <Dialog open={!!container} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>日志 · {container?.name}</DialogTitle>
        </DialogHeader>
        <LogStream containerId={container?.id} />
      </DialogContent>
    </Dialog>
  );
}

function LogStream({ containerId }: { containerId?: string }) {
  const [buffer, setBuffer] = useState('');
  const [connected, setConnected] = useState(false);
  const [ended, setEnded] = useState(false);
  const preRef = useRef<HTMLPreElement>(null);

  useEffect(() => {
    if (!containerId) return;

    // Reset for each new container.
    setBuffer('');
    setEnded(false);
    setConnected(false);

    const url = wsUrl(
      `/ws/docker-logs?container=${encodeURIComponent(containerId)}&tail=200&follow=true`,
    );
    const ws = new WebSocket(url);

    ws.onopen = () => setConnected(true);
    ws.onclose = () => {
      setConnected(false);
      setEnded(true);
    };
    ws.onerror = () => {
      // onclose will fire next; nothing extra to do here.
    };
    ws.onmessage = (e) => {
      const chunk = typeof e.data === 'string' ? e.data : '';
      // The backend sends a final JSON `{"type":"exit"}` frame to mark the
      // end of the stream (e.g. container stopped or client requested
      // non-follow). Detect it without echoing the literal JSON to the user.
      if (chunk === '{"type":"exit"}') {
        setEnded(true);
        return;
      }
      setBuffer((prev) => {
        // Cap the buffer to ~256KB to prevent unbounded memory growth on
        // long-lived noisy logs. We keep the tail by slicing off the head.
        const next = prev + chunk;
        if (next.length > 262144) return next.slice(-262144);
        return next;
      });
    };

    return () => {
      ws.close();
    };
  }, [containerId]);

  // Auto-scroll to bottom on new logs, but only if the user is already
  // parked near the bottom (so scrolling up to read history isn't yanked
  // away by incoming frames).
  useEffect(() => {
    const el = preRef.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    if (nearBottom) el.scrollTop = el.scrollHeight;
  }, [buffer]);

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span
          className={cn(
            'h-2 w-2 rounded-full',
            connected ? 'bg-success' : ended ? 'bg-muted-foreground' : 'bg-warning',
          )}
        />
        {connected ? '实时日志流已连接' : ended ? '已结束（容器可能已停止）' : '连接中…'}
      </div>
      <pre
        ref={preRef}
        className="h-80 overflow-auto rounded-md bg-black/80 p-3 font-mono text-xs leading-relaxed text-green-400"
      >
        {buffer || (ended ? '（无日志输出）' : '等待日志…')}
      </pre>
    </div>
  );
}

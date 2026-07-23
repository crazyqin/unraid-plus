import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Boxes,
  Cpu,
  Loader2,
  MemoryStick,
  Pause,
  Play,
  RotateCw,
  ScrollText,
  Square,
  Search,
} from 'lucide-react';
import { api, ApiError, wsUrl } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Progress } from '@/components/ui/progress';
import { ConfirmDialog } from '@/components/ui/alert-dialog';
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

/** Status → left border color class for container cards */
const STATUS_BORDER: Record<string, string> = {
  running: 'border-l-emerald-500/60',
  exited: 'border-l-border',
  paused: 'border-l-amber-500/60',
  restarting: 'border-l-amber-500/60',
  created: 'border-l-border',
  dead: 'border-l-red-500/70',
};

/** Status → subtle background tint for container cards */
const STATUS_BG: Record<string, string> = {
  running: '',
  exited: '',
  paused: 'bg-amber-500/5',
  restarting: 'bg-amber-500/5',
  created: '',
  dead: 'bg-red-500/5',
};

/** Highlight matching text in a string with a yellow background */
function Highlight({ text, query }: { text: string; query: string }) {
  if (!query) return <>{text}</>;
  const idx = text.toLowerCase().indexOf(query.toLowerCase());
  if (idx === -1) return <>{text}</>;
  return (
    <>
      {text.slice(0, idx)}
      <mark className="rounded-sm bg-yellow-300/80 px-0.5 text-inherit">
        {text.slice(idx, idx + query.length)}
      </mark>
      {text.slice(idx + query.length)}
    </>
  );
}

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

  const [actionError, setActionError] = useState<string | null>(null);
  const [pendingAction, setPendingAction] = useState<string | null>(null);
  const [confirmAction, setConfirmAction] = useState<{ id: string; action: 'stop' | 'restart'; name: string } | null>(null);

  const act = async (id: string, action: 'start' | 'stop' | 'restart' | 'pause') => {
    // Destructive actions require confirmation
    if ((action === 'stop' || action === 'restart') && !confirmAction) {
      const c = (data ?? []).find((c) => c.id === id);
      setConfirmAction({ id, action, name: c?.name ?? id });
      return;
    }
    const key = `${id}:${action}`;
    setPendingAction(key);
    setActionError(null);
    setConfirmAction(null);
    try {
      await api.post(`/docker/containers/${id}/${action}`);
      qc.invalidateQueries({ queryKey: ['docker'] });
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '操作失败');
    } finally {
      setPendingAction(null);
    }
  };

  const containers = (data ?? []).filter((c) =>
    c.name.toLowerCase().includes(filter.toLowerCase()),
  );
  const running = (data ?? []).filter((c) => c.status === 'running').length;

  return (
    <div className="space-y-4 p-4 md:p-6">
      {actionError && (
        <div className="flex items-center justify-between rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
          <span>{actionError}</span>
          <button className="text-xs underline" onClick={() => setActionError(null)}>
            关闭
          </button>
        </div>
      )}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className={cn(
            'flex h-10 w-10 items-center justify-center rounded-lg',
            running > 0 ? 'bg-success/15 text-success' : 'bg-muted text-muted-foreground',
          )}>
            <Boxes className="h-5 w-5" />
          </div>
          <div>
            <h1 className="text-xl font-semibold">Docker 容器</h1>
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Badge
                variant={running > 0 ? 'success' : 'secondary'}
                className="text-[10px]"
              >
                {running > 0 ? '运行中' : '空闲'}
              </Badge>
              <span>{running} / {data?.length ?? 0} 个容器</span>
            </div>
          </div>
        </div>
        <div className="relative w-48 shrink-0">
          <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 pl-8 text-sm"
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
            <Card key={c.id} className={cn(
              'flex flex-col border-l-2 transition-colors hover:bg-muted/30',
              STATUS_BORDER[c.status] ?? 'border-l-border',
              STATUS_BG[c.status],
            )}>
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <CardTitle className="truncate text-sm">
                      <Highlight text={c.name} query={filter} />
                    </CardTitle>
                    <div className="truncate text-xs text-muted-foreground">
                      <Highlight text={truncate(c.image, 38)} query={filter} />
                    </div>
                  </div>
                  <Badge variant={STATUS_VARIANT[c.status]} className="text-[9px] px-1.5 py-0 leading-none">{STATUS_LABEL[c.status] ?? c.status}</Badge>
                </div>
              </CardHeader>
              <CardContent className="flex flex-1 flex-col gap-3">
                <div className="flex flex-wrap gap-1">
                  {c.ports.slice(0, 3).map((p) => (
                    <Badge key={p} variant="outline" className="font-mono text-[9px] px-1.5 py-0 leading-none">
                      {p}
                    </Badge>
                  ))}
                  {c.ports.length > 3 && (
                    <Badge variant="outline" className="text-[9px] px-1.5 py-0 leading-none">
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
                <div className="mt-auto flex flex-wrap gap-2 pt-1">
                  {c.status !== 'running' && (
                    <Button size="sm" variant="success" onClick={() => act(c.id, 'start')} disabled={pendingAction !== null}>
                      {pendingAction === `${c.id}:start` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />} 启动
                    </Button>
                  )}
                  {c.status === 'running' && (
                    <Button size="sm" variant="destructive" onClick={() => act(c.id, 'stop')} disabled={pendingAction !== null}>
                      {pendingAction === `${c.id}:stop` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Square className="h-3.5 w-3.5" />} 停止
                    </Button>
                  )}
                  <Button size="sm" variant="outline" onClick={() => act(c.id, 'restart')} disabled={pendingAction !== null}>
                    {pendingAction === `${c.id}:restart` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RotateCw className="h-3.5 w-3.5" />} 重启
                  </Button>
                  {c.status === 'running' && (
                    <Button size="sm" variant="ghost" onClick={() => act(c.id, 'pause')} disabled={pendingAction !== null}>
                      <Pause className="h-3.5 w-3.5" /> 暂停
                    </Button>
                  )}
                  <Button size="sm" variant="ghost" onClick={() => setLogsFor(c)} disabled={pendingAction !== null}>
                    <ScrollText className="h-3.5 w-3.5" /> 日志
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <ConfirmDialog
        open={!!confirmAction}
        title={confirmAction?.action === 'stop' ? '确认停止容器' : '确认重启容器'}
        description={`确定要${confirmAction?.action === 'stop' ? '停止' : '重启'}容器 "${confirmAction?.name}" 吗？${confirmAction?.action === 'stop' ? '运行中的服务将中断。' : ''}`}
        confirmText={confirmAction?.action === 'stop' ? '停止' : '重启'}
        variant="destructive"
        onConfirm={() => confirmAction && act(confirmAction.id, confirmAction.action)}
        onCancel={() => setConfirmAction(null)}
      />

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
    <div className="space-y-1.5">
      {/* CPU + Memory pills */}
      <div className="flex items-center gap-1.5">
        <span className="inline-flex items-center gap-1 rounded bg-orange-500/10 px-1.5 py-0.5 text-[10px] font-mono tabular-nums text-ind-orange">
          <Cpu className="h-2.5 w-2.5" />
          {stats.cpuPct.toFixed(1)}%
        </span>
        <span className={cn(
          'inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-mono tabular-nums',
          memPct > 90
            ? 'bg-red-500/10 text-ind-red'
            : memPct > 75
              ? 'bg-amber-500/10 text-ind-amber'
              : 'bg-sky-500/10 text-ind-sky',
        )}>
          <MemoryStick className="h-2.5 w-2.5" />
          {formatBytes(stats.memUsageBytes)}/{formatBytes(stats.memLimitBytes)}
        </span>
      </div>
      {/* Memory progress bar */}
      <Progress
        className="h-1.5"
        value={memPct}
        indicatorClassName={
          memPct > 90 ? 'bg-destructive' : memPct > 75 ? 'bg-warning' : 'bg-primary'
        }
      />
      {/* Network I/O + PIDs */}
      <div className="flex items-center gap-1.5">
        <span className="inline-flex items-center gap-0.5 rounded bg-emerald-500/10 px-1.5 py-0.5 text-[10px] font-mono tabular-nums text-ind-emerald">
          ↓ {formatBytes(stats.netRxBytes)}
        </span>
        <span className="inline-flex items-center gap-0.5 rounded bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-mono tabular-nums text-ind-amber">
          ↑ {formatBytes(stats.netTxBytes)}
        </span>
        <span className="ml-auto text-[10px] tabular-nums text-muted-foreground">
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
        className="h-80 overflow-auto overflow-x-auto whitespace-pre-wrap break-all rounded-md bg-black/80 p-3 font-mono text-xs leading-relaxed text-green-400"
      >
        {buffer || (ended ? '（无日志输出）' : '等待日志…')}
      </pre>
    </div>
  );
}

import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { motion } from 'framer-motion';
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
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { cn, formatBytes, timeAgo, truncate } from '@/lib/utils';
import { useSettingsStore } from '@/stores/settings';
import type { ContainerStats, DockerContainer } from '@/types';
import {
  staggerContainer,
  fadeUpVariants,
  springGentle,
} from '@/lib/motion';

const STATUS_VARIANT: Record<DockerContainer['status'], 'success' | 'secondary' | 'warning' | 'destructive'> = {
  running: 'success',
  exited: 'secondary',
  paused: 'warning',
  restarting: 'warning',
  created: 'secondary',
  dead: 'destructive',
};

const STATUS_BORDER: Record<string, string> = {
  running: 'border-l-emerald-500/60',
  exited: 'border-l-border',
  paused: 'border-l-amber-500/60',
  restarting: 'border-l-amber-500/60',
  created: 'border-l-border',
  dead: 'border-l-red-500/70',
};

const STATUS_BG: Record<string, string> = {
  running: '',
  exited: '',
  paused: 'bg-amber-500/5',
  restarting: 'bg-amber-500/5',
  created: '',
  dead: 'bg-red-500/5',
};

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
  const { t } = useTranslation();
  const STATUS_LABEL: Record<string, string> = {
    running: t('docker.running'),
    exited: t('docker.exited'),
    paused: t('docker.paused'),
    restarting: t('docker.restarting'),
    created: t('docker.created'),
    dead: t('docker.stopped'),
  };
  const refresh = useSettingsStore((s) => s.refreshInterval);
  const qc = useQueryClient();
  const [filter, setFilter] = useState('');
  const [logsFor, setLogsFor] = useState<DockerContainer | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ['docker'],
    queryFn: () => api.get<DockerContainer[]>('/docker/containers'),
    refetchInterval: refresh || false,
  });

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
      setActionError(err instanceof ApiError ? err.message : t('common.failed'));
    } finally {
      setPendingAction(null);
    }
  };

  const containers = (data ?? []).filter((c) =>
    c.name.toLowerCase().includes(filter.toLowerCase()),
  );
  const running = (data ?? []).filter((c) => c.status === 'running').length;

  return (
    <div className="space-y-5 p-5 md:p-6">
      {actionError && (
        <motion.div
          className="flex items-center justify-between rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive"
          initial={{ opacity: 0, y: -8 }}
          animate={{ opacity: 1, y: 0 }}
        >
          <span>{actionError}</span>
          <button className="text-xs underline" onClick={() => setActionError(null)}>
            {t('common.close')}
          </button>
        </motion.div>
      )}

      {/* Header */}
      <motion.div
        className="flex flex-wrap items-end justify-between gap-4"
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={springGentle}
      >
        <div>
          <h1 className="text-display-md text-foreground">{t('docker.title')}</h1>
          <div className="mt-2 flex items-center gap-2 text-sm text-muted-foreground">
            <Badge
              variant={running > 0 ? 'success' : 'secondary'}
              className="text-[10px] font-semibold tracking-wide px-2.5"
            >
              {running > 0 ? t('docker.running') : t('docker.idle')}
            </Badge>
            <span className="text-xs">{running} / {data?.length ?? 0} {t('docker.containerCount')}</span>
          </div>
        </div>
        <div className="relative w-48 shrink-0">
          <Search className="absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground/60" />
          <Input
            className="h-9 pl-9 text-sm rounded-xl border-border/50 bg-card/50 backdrop-blur-sm"
            placeholder={t('docker.searchPlaceholder')}
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        </div>
      </motion.div>

      {isLoading ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" /> {t('docker.loading')}
        </div>
      ) : containers.length === 0 ? (
        <motion.div
          className="card-bento flex flex-col items-center gap-3 py-16 text-center text-sm text-muted-foreground"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
        >
          <Boxes className="h-10 w-10 text-muted-foreground/30" />
          {t('docker.noMatch')}
        </motion.div>
      ) : (
        <motion.div
          className="grid gap-4 md:grid-cols-2 xl:grid-cols-3"
          variants={staggerContainer}
          initial="hidden"
          animate="visible"
        >
          {containers.map((c) => (
            <motion.div
              key={c.id}
              className={cn(
                'card-bento flex flex-col border-l-2 overflow-hidden',
                STATUS_BORDER[c.status] ?? 'border-l-border',
                STATUS_BG[c.status],
              )}
              variants={fadeUpVariants}
              whileHover={{ y: -2 }}
              transition={springGentle}
            >
              <div className="px-5 pt-5 pb-3">
                <div className="flex items-start justify-between gap-2">
                  <div className="flex items-start gap-3 min-w-0">
                    {c.icon || c.iconUrl ? (
                      <img
                        src={c.icon || c.iconUrl}
                        alt={c.name}
                        className="h-9 w-9 shrink-0 rounded-lg bg-white/90 object-contain p-0.5 dark:bg-white/80"
                        onError={(e) => {
                          const img = e.currentTarget;
                          if (img.src === c.iconUrl) {
                            img.style.display = 'none';
                          }
                        }}
                      />
                    ) : (
                      <div className="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-primary/10 text-primary">
                        <Boxes className="h-4 w-4" />
                      </div>
                    )}
                    <div className="min-w-0">
                      <div className="truncate text-sm font-semibold">
                        <Highlight text={c.name} query={filter} />
                      </div>
                      <div className="truncate text-[11px] text-muted-foreground/70">
                        <Highlight text={truncate(c.image, 38)} query={filter} />
                      </div>
                    </div>
                  </div>
                  <Badge variant={STATUS_VARIANT[c.status]} className="text-[9px] px-1.5 py-0 leading-none shrink-0 font-semibold tracking-wide">{STATUS_LABEL[c.status] ?? c.status}</Badge>
                </div>
              </div>
              <div className="flex-1 flex flex-col gap-3 px-5 pb-4">
                <div className="flex flex-wrap gap-1">
                  {(c.ports ?? []).slice(0, 3).map((p) => (
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

                {c.status === 'running' && statsMap.get(c.id) && (
                  <ContainerStatsBar stats={statsMap.get(c.id)!} />
                )}

                <div className="text-[11px] text-muted-foreground/50">
                  {t('docker.startedAt')} {c.startedAt ? timeAgo(c.startedAt) : '--'}
                </div>
                <div className="mt-auto flex flex-wrap gap-2 pt-1">
                  {c.status !== 'running' && (
                    <Button size="sm" variant="success" onClick={() => act(c.id, 'start')} disabled={pendingAction !== null} className="rounded-lg h-8">
                      {pendingAction === `${c.id}:start` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />} {t('docker.start')}
                    </Button>
                  )}
                  {c.status === 'running' && (
                    <Button size="sm" variant="destructive" onClick={() => act(c.id, 'stop')} disabled={pendingAction !== null} className="rounded-lg h-8">
                      {pendingAction === `${c.id}:stop` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Square className="h-3.5 w-3.5" />} {t('docker.stop')}
                    </Button>
                  )}
                  <Button size="sm" variant="outline" onClick={() => act(c.id, 'restart')} disabled={pendingAction !== null} className="rounded-lg h-8">
                    {pendingAction === `${c.id}:restart` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RotateCw className="h-3.5 w-3.5" />} {t('docker.restart')}
                  </Button>
                  {c.status === 'running' && (
                    <Button size="sm" variant="ghost" onClick={() => act(c.id, 'pause')} disabled={pendingAction !== null} className="rounded-lg h-8">
                      <Pause className="h-3.5 w-3.5" /> {t('docker.pause')}
                    </Button>
                  )}
                  <Button size="sm" variant="ghost" onClick={() => setLogsFor(c)} disabled={pendingAction !== null} className="rounded-lg h-8">
                    <ScrollText className="h-3.5 w-3.5" /> {t('docker.logs')}
                  </Button>
                </div>
              </div>
            </motion.div>
          ))}
        </motion.div>
      )}

      <ConfirmDialog
        open={!!confirmAction}
        title={confirmAction?.action === 'stop' ? t('docker.confirmStop') : t('docker.confirmRestart')}
        description={t('docker.confirmStopDesc', { action: confirmAction?.action === 'stop' ? t('docker.stop') : t('docker.restart'), name: confirmAction?.name ?? '' })}
        confirmText={confirmAction?.action === 'stop' ? t('docker.stop') : t('docker.restart')}
        variant="destructive"
        onConfirm={() => confirmAction && act(confirmAction.id, confirmAction.action)}
        onCancel={() => setConfirmAction(null)}
      />

      <LogDialog container={logsFor} onClose={() => setLogsFor(null)} />
    </div>
  );
}

/* ── Container Stats Bar ── */
function ContainerStatsBar({ stats }: { stats: ContainerStats }) {
  const memPct = stats.memLimitBytes > 0
    ? (stats.memUsageBytes / stats.memLimitBytes) * 100
    : 0;

  return (
    <div className="space-y-1.5">
      <div className="flex items-center gap-1.5">
        <span className="inline-flex items-center gap-1 rounded-md bg-orange-500/10 px-1.5 py-0.5 text-[10px] font-mono-data tabular-nums text-ind-orange">
          <Cpu className="h-2.5 w-2.5" />
          {stats.cpuPct.toFixed(1)}%
        </span>
        <span className={cn(
          'inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-mono-data tabular-nums',
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
      <Progress
        className="h-1"
        value={memPct}
        indicatorClassName={
          memPct > 90 ? 'bg-destructive' : memPct > 75 ? 'bg-warning' : 'bg-primary'
        }
      />
      <div className="flex items-center gap-1.5">
        <span className="inline-flex items-center gap-0.5 rounded-md bg-emerald-500/10 px-1.5 py-0.5 text-[10px] font-mono-data tabular-nums text-ind-emerald">
          ↓ {formatBytes(stats.netRxBytes)}
        </span>
        <span className="inline-flex items-center gap-0.5 rounded-md bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-mono-data tabular-nums text-ind-amber">
          ↑ {formatBytes(stats.netTxBytes)}
        </span>
        <span className="ml-auto text-[10px] tabular-nums text-muted-foreground/50">
          {stats.pids} PIDs
        </span>
      </div>
    </div>
  );
}

/* ── Log Dialog ── */
function LogDialog({
  container,
  onClose,
}: {
  container: DockerContainer | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Dialog open={!!container} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-3xl rounded-2xl">
        <DialogHeader>
          <DialogTitle>{t('docker.logTitle')} {container?.name}</DialogTitle>
        </DialogHeader>
        <LogStream containerId={container?.id} />
      </DialogContent>
    </Dialog>
  );
}

function LogStream({ containerId }: { containerId?: string }) {
  const { t } = useTranslation();
  const [buffer, setBuffer] = useState('');
  const [connected, setConnected] = useState(false);
  const [ended, setEnded] = useState(false);
  const preRef = useRef<HTMLPreElement>(null);

  useEffect(() => {
    if (!containerId) return;
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
    ws.onerror = () => {};
    ws.onmessage = (e) => {
      const chunk = typeof e.data === 'string' ? e.data : '';
      if (chunk === '{"type":"exit"}') {
        setEnded(true);
        return;
      }
      setBuffer((prev) => {
        const next = prev + chunk;
        if (next.length > 262144) return next.slice(-262144);
        return next;
      });
    };

    return () => { ws.close(); };
  }, [containerId]);

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
        {connected ? t('docker.logConnected') : ended ? t('docker.logEnded') : t('docker.logConnecting')}
      </div>
      <pre
        ref={preRef}
        className="h-80 overflow-auto whitespace-pre-wrap break-all rounded-xl bg-black/80 p-4 font-mono text-xs leading-relaxed text-green-400"
      >
        {buffer || (ended ? t('docker.logEmpty') : t('docker.logWaiting'))}
      </pre>
    </div>
  );
}

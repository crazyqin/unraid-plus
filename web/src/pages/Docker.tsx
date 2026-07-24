import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { motion } from 'framer-motion';
import {
  Boxes,
  Cable,
  Cpu,
  FolderOpen,
  Loader2,
  MemoryStick,
  Network,
  Pause,
  Play,
  Power,
  RotateCw,
  ScrollText,
  Search,
  Square,
  Zap,
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
  springSnappy,
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
  running: 'border-l-emerald-500/70',
  exited: 'border-l-border/60',
  paused: 'border-l-amber-500/70',
  restarting: 'border-l-amber-500/70',
  created: 'border-l-border/60',
  dead: 'border-l-red-500/70',
};

function Highlight({ text, query }: { text: string; query: string }) {
  if (!query) return <>{text}</>;
  const idx = text.toLowerCase().indexOf(query.toLowerCase());
  if (idx === -1) return <>{text}</>;
  return (
    <>
      {text.slice(0, idx)}
      <mark className="rounded-sm bg-primary/25 px-0.5 text-inherit">
        {text.slice(idx, idx + query.length)}
      </mark>
      {text.slice(idx + query.length)}
    </>
  );
}

/** Match docker stats (short docker id / name) against GraphQL PrefixedID entries. */
function findStats(stats: ContainerStats[] | undefined, c: DockerContainer): ContainerStats | undefined {
  if (!stats?.length) return undefined;
  const short = (c.shortId || c.id.split(':').pop() || c.id).toLowerCase();
  const name = c.name.toLowerCase();
  return stats.find((s) => {
    const sid = (s.id || '').toLowerCase();
    const sname = (s.name || '').replace(/^\//, '').toLowerCase();
    return (
      sid === short ||
      short.startsWith(sid) ||
      sid.startsWith(short.slice(0, 12)) ||
      sname === name
    );
  });
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

  const containers = useMemo(
    () =>
      (data ?? []).filter((c) => {
        const q = filter.toLowerCase();
        if (!q) return true;
        return (
          c.name.toLowerCase().includes(q) ||
          c.image.toLowerCase().includes(q) ||
          (c.networkMode ?? '').toLowerCase().includes(q) ||
          (c.ports ?? []).some((p) => p.toLowerCase().includes(q))
        );
      }),
    [data, filter],
  );

  const running = (data ?? []).filter((c) => c.status === 'running').length;
  const autoStartCount = (data ?? []).filter((c) => c.autoStart).length;

  return (
    <div className="relative space-y-6 p-5 md:p-8">
      {/* Ambient orb */}
      <div className="pointer-events-none absolute -right-20 -top-20 h-72 w-72 rounded-full bg-primary/10 blur-3xl" />

      {actionError && (
        <motion.div
          className="flex items-center justify-between rounded-2xl border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive glass"
          initial={{ opacity: 0, y: -8 }}
          animate={{ opacity: 1, y: 0 }}
        >
          <span>{actionError}</span>
          <button className="text-xs underline" onClick={() => setActionError(null)}>
            {t('common.close')}
          </button>
        </motion.div>
      )}

      {/* Editorial header */}
      <motion.div
        className="flex flex-wrap items-end justify-between gap-6"
        initial={{ opacity: 0, y: 18 }}
        animate={{ opacity: 1, y: 0 }}
        transition={springGentle}
      >
        <div className="min-w-0">
          <div className="mb-2 flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.28em] text-muted-foreground/70">
            <Boxes className="h-3 w-3 text-primary" />
            Containers
          </div>
          <h1 className="text-display-md tracking-tight text-foreground">
            {t('docker.title')}
          </h1>
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <Badge
              variant={running > 0 ? 'success' : 'secondary'}
              className="rounded-full px-2.5 text-[10px] font-semibold tracking-wide"
            >
              <Power className="mr-1 h-3 w-3" />
              {running} {t('docker.running')}
            </Badge>
            <Badge variant="secondary" className="rounded-full px-2.5 text-[10px] font-mono-data">
              {(data?.length ?? 0)} {t('docker.containerCount')}
            </Badge>
            {autoStartCount > 0 && (
              <Badge variant="outline" className="rounded-full px-2.5 text-[10px]">
                <Zap className="mr-1 h-3 w-3" />
                {autoStartCount} auto
              </Badge>
            )}
          </div>
        </div>
        <div className="relative w-full max-w-xs shrink-0">
          <Search className="absolute left-3.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground/60" />
          <Input
            className="h-11 rounded-2xl border-border/40 bg-card/40 pl-10 text-sm backdrop-blur-xl"
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
          className="card-bento flex flex-col items-center gap-3 py-20 text-center text-sm text-muted-foreground"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
        >
          <Boxes className="h-12 w-12 text-muted-foreground/25" />
          {t('docker.noMatch')}
        </motion.div>
      ) : (
        <motion.div
          className="grid gap-4 md:grid-cols-2 xl:grid-cols-3"
          variants={staggerContainer}
          initial="hidden"
          animate="visible"
        >
          {containers.map((c) => {
            const stats = findStats(statsData, c);
            return (
              <motion.div
                key={c.id}
                className={cn(
                  'card-bento group relative flex flex-col overflow-hidden border-l-[3px]',
                  STATUS_BORDER[c.status] ?? 'border-l-border',
                )}
                variants={fadeUpVariants}
                whileHover={{ y: -4 }}
                transition={springSnappy}
              >
                <div className="pointer-events-none absolute inset-0 bg-gradient-to-br from-primary/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100" />

                <div className="relative px-5 pt-5 pb-3">
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex min-w-0 items-start gap-3">
                      {c.icon || c.iconUrl ? (
                        <img
                          src={c.icon || c.iconUrl}
                          alt=""
                          className="h-11 w-11 shrink-0 rounded-xl bg-white/90 object-contain p-1 shadow-sm dark:bg-white/85"
                          onError={(e) => {
                            e.currentTarget.style.display = 'none';
                          }}
                        />
                      ) : (
                        <div className="grid h-11 w-11 shrink-0 place-items-center rounded-xl bg-primary/10 text-primary">
                          <Boxes className="h-5 w-5" />
                        </div>
                      )}
                      <div className="min-w-0">
                        <div className="truncate text-[15px] font-semibold tracking-tight">
                          <Highlight text={c.name} query={filter} />
                        </div>
                        <div className="mt-0.5 truncate font-mono-data text-[11px] text-muted-foreground/70">
                          <Highlight text={truncate(c.image, 42)} query={filter} />
                        </div>
                        {c.statusText && (
                          <div className="mt-1 truncate text-[11px] text-muted-foreground/55">
                            {c.statusText}
                          </div>
                        )}
                      </div>
                    </div>
                    <Badge
                      variant={STATUS_VARIANT[c.status]}
                      className="shrink-0 rounded-full px-2 py-0.5 text-[9px] font-semibold uppercase tracking-wider"
                    >
                      {STATUS_LABEL[c.status] ?? c.status}
                    </Badge>
                  </div>
                </div>

                <div className="relative flex flex-1 flex-col gap-3 px-5 pb-4">
                  {/* Meta chips */}
                  <div className="flex flex-wrap gap-1.5">
                    {c.autoStart && (
                      <span className="inline-flex items-center gap-1 rounded-md bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-medium text-amber-600 dark:text-amber-400">
                        <Zap className="h-2.5 w-2.5" />
                        auto
                      </span>
                    )}
                    {c.networkMode && (
                      <span className="inline-flex items-center gap-1 rounded-md bg-sky-500/10 px-1.5 py-0.5 text-[10px] font-mono-data text-sky-600 dark:text-sky-400">
                        <Network className="h-2.5 w-2.5" />
                        {c.networkMode}
                      </span>
                    )}
                    {(c.mounts?.length ?? 0) > 0 && (
                      <span className="inline-flex items-center gap-1 rounded-md bg-violet-500/10 px-1.5 py-0.5 text-[10px] text-violet-600 dark:text-violet-400">
                        <FolderOpen className="h-2.5 w-2.5" />
                        {c.mounts.length} vol
                      </span>
                    )}
                    {(c.ports?.length ?? 0) > 0 && (
                      <span className="inline-flex items-center gap-1 rounded-md bg-emerald-500/10 px-1.5 py-0.5 text-[10px] text-emerald-600 dark:text-emerald-400">
                        <Cable className="h-2.5 w-2.5" />
                        {c.ports.length} port
                      </span>
                    )}
                  </div>

                  {/* Ports */}
                  {(c.ports?.length ?? 0) > 0 && (
                    <div className="flex flex-wrap gap-1">
                      {c.ports.slice(0, 4).map((p) => (
                        <Badge
                          key={p}
                          variant="outline"
                          className="rounded-md px-1.5 py-0 font-mono-data text-[9px] leading-none"
                        >
                          {p}
                        </Badge>
                      ))}
                      {c.ports.length > 4 && (
                        <Badge variant="outline" className="rounded-md px-1.5 py-0 text-[9px]">
                          +{c.ports.length - 4}
                        </Badge>
                      )}
                    </div>
                  )}

                  {/* Mounts preview */}
                  {(c.mounts?.length ?? 0) > 0 && (
                    <div className="space-y-1 rounded-xl border border-border/40 bg-background/40 p-2.5">
                      {c.mounts.slice(0, 2).map((m) => (
                        <div
                          key={`${m.source}->${m.destination}`}
                          className="truncate font-mono-data text-[10px] text-muted-foreground/80"
                          title={`${m.source} → ${m.destination}`}
                        >
                          <span className="text-foreground/70">{truncate(m.source, 28)}</span>
                          <span className="mx-1 text-muted-foreground/40">→</span>
                          <span>{m.destination}</span>
                        </div>
                      ))}
                      {c.mounts.length > 2 && (
                        <div className="text-[10px] text-muted-foreground/50">
                          +{c.mounts.length - 2} more
                        </div>
                      )}
                    </div>
                  )}

                  {c.status === 'running' && stats && <ContainerStatsBar stats={stats} />}

                  <div className="text-[11px] text-muted-foreground/50">
                    {t('docker.startedAt')}{' '}
                    <span className="font-mono-data text-muted-foreground/80">
                      {c.startedAt
                        ? timeAgo(c.startedAt)
                        : c.createdAt
                          ? timeAgo(c.createdAt)
                          : '—'}
                    </span>
                    {c.via && (
                      <span className="ml-2 rounded bg-muted/50 px-1 py-0.5 text-[9px] uppercase tracking-wider">
                        {c.via}
                      </span>
                    )}
                  </div>

                  <div className="mt-auto grid grid-cols-2 gap-2 pt-2">
                    {c.status !== 'running' && (
                      <Button
                        size="sm"
                        variant="success"
                        onClick={() => act(c.id, 'start')}
                        disabled={pendingAction !== null}
                        className="h-9 rounded-xl"
                      >
                        {pendingAction === `${c.id}:start` ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <Play className="h-3.5 w-3.5" />
                        )}{' '}
                        {t('docker.start')}
                      </Button>
                    )}
                    {c.status === 'running' && (
                      <Button
                        size="sm"
                        variant="destructive"
                        onClick={() => act(c.id, 'stop')}
                        disabled={pendingAction !== null}
                        className="h-9 rounded-xl"
                      >
                        {pendingAction === `${c.id}:stop` ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <Square className="h-3.5 w-3.5" />
                        )}{' '}
                        {t('docker.stop')}
                      </Button>
                    )}
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => act(c.id, 'restart')}
                      disabled={pendingAction !== null}
                      className="h-9 rounded-xl"
                    >
                      {pendingAction === `${c.id}:restart` ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <RotateCw className="h-3.5 w-3.5" />
                      )}{' '}
                      {t('docker.restart')}
                    </Button>
                    {c.status === 'running' && (
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => act(c.id, 'pause')}
                        disabled={pendingAction !== null}
                        className="h-9 rounded-xl"
                      >
                        <Pause className="h-3.5 w-3.5" /> {t('docker.pause')}
                      </Button>
                    )}
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => setLogsFor(c)}
                      disabled={pendingAction !== null}
                      className={cn(
                        'h-9 rounded-xl',
                        c.status !== 'running' ? 'col-span-2' : '',
                      )}
                    >
                      <ScrollText className="h-3.5 w-3.5" /> {t('docker.logs')}
                    </Button>
                  </div>
                </div>
              </motion.div>
            );
          })}
        </motion.div>
      )}

      <ConfirmDialog
        open={!!confirmAction}
        title={confirmAction?.action === 'stop' ? t('docker.confirmStop') : t('docker.confirmRestart')}
        description={t('docker.confirmStopDesc', {
          action: confirmAction?.action === 'stop' ? t('docker.stop') : t('docker.restart'),
          name: confirmAction?.name ?? '',
        })}
        confirmText={confirmAction?.action === 'stop' ? t('docker.stop') : t('docker.restart')}
        variant="destructive"
        onConfirm={() => confirmAction && act(confirmAction.id, confirmAction.action)}
        onCancel={() => setConfirmAction(null)}
      />

      <LogDialog container={logsFor} onClose={() => setLogsFor(null)} />
    </div>
  );
}

function ContainerStatsBar({ stats }: { stats: ContainerStats }) {
  const memPct =
    stats.memLimitBytes > 0 ? (stats.memUsageBytes / stats.memLimitBytes) * 100 : stats.memPct || 0;

  return (
    <div className="space-y-1.5 rounded-xl border border-border/30 bg-background/30 p-2.5">
      <div className="flex items-center gap-1.5">
        <span className="inline-flex items-center gap-1 rounded-md bg-orange-500/10 px-1.5 py-0.5 font-mono-data text-[10px] tabular-nums text-ind-orange">
          <Cpu className="h-2.5 w-2.5" />
          {stats.cpuPct.toFixed(1)}%
        </span>
        <span
          className={cn(
            'inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 font-mono-data text-[10px] tabular-nums',
            memPct > 90
              ? 'bg-red-500/10 text-ind-red'
              : memPct > 75
                ? 'bg-amber-500/10 text-ind-amber'
                : 'bg-sky-500/10 text-ind-sky',
          )}
        >
          <MemoryStick className="h-2.5 w-2.5" />
          {formatBytes(stats.memUsageBytes)}
          {stats.memLimitBytes > 0 ? `/${formatBytes(stats.memLimitBytes)}` : ''}
        </span>
      </div>
      <Progress
        className="h-1"
        value={Math.min(100, memPct)}
        indicatorClassName={
          memPct > 90 ? 'bg-destructive' : memPct > 75 ? 'bg-warning' : 'bg-primary'
        }
      />
      <div className="flex items-center gap-1.5">
        <span className="inline-flex items-center gap-0.5 rounded-md bg-emerald-500/10 px-1.5 py-0.5 font-mono-data text-[10px] tabular-nums text-ind-emerald">
          ↓ {formatBytes(stats.netRxBytes)}
        </span>
        <span className="inline-flex items-center gap-0.5 rounded-md bg-amber-500/10 px-1.5 py-0.5 font-mono-data text-[10px] tabular-nums text-ind-amber">
          ↑ {formatBytes(stats.netTxBytes)}
        </span>
        <span className="ml-auto text-[10px] tabular-nums text-muted-foreground/50">
          {stats.pids} PIDs
        </span>
      </div>
    </div>
  );
}

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
          <DialogTitle className="flex items-center gap-2">
            <ScrollText className="h-4 w-4 text-primary" />
            {t('docker.logTitle')} {container?.name}
          </DialogTitle>
        </DialogHeader>
        <LogStream
          containerId={container?.id}
          shortId={container?.shortId}
          name={container?.name}
        />
      </DialogContent>
    </Dialog>
  );
}

function LogStream({
  containerId,
  shortId,
  name,
}: {
  containerId?: string;
  shortId?: string;
  name?: string;
}) {
  const { t } = useTranslation();
  const [buffer, setBuffer] = useState('');
  const [connected, setConnected] = useState(false);
  const [ended, setEnded] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const preRef = useRef<HTMLPreElement>(null);
  const openedRef = useRef(false);

  useEffect(() => {
    if (!containerId && !shortId && !name) return;
    setBuffer('');
    setEnded(false);
    setConnected(false);
    setError(null);
    openedRef.current = false;
    let closedByUs = false;

    // Prefer short local docker id / name — PrefixedIDs break older validators.
    const local =
      (shortId && shortId.slice(0, 64)) ||
      (containerId?.includes(':') ? containerId.split(':').pop() : containerId) ||
      name ||
      '';
    const url = wsUrl(
      `/ws/docker-logs?container=${encodeURIComponent(local)}&tail=200&follow=true`,
    );
    const ws = new WebSocket(url);

    ws.onopen = () => {
      openedRef.current = true;
      setConnected(true);
    };
    ws.onclose = () => {
      setConnected(false);
      setEnded(true);
      // Only error if the socket never opened (auth/SSH/origin/id rejection).
      if (!openedRef.current && !closedByUs) {
        setError(t('docker.logFailed'));
      }
    };
    ws.onerror = () => {
      if (!openedRef.current) {
        setError(t('docker.logFailed'));
      }
    };
    ws.onmessage = (e) => {
      const chunk = typeof e.data === 'string' ? e.data : '';
      if (chunk === '{"type":"exit"}') {
        setEnded(true);
        return;
      }
      setError(null);
      setBuffer((prev) => {
        const next = prev + chunk;
        if (next.length > 262144) return next.slice(-262144);
        return next;
      });
    };

    return () => {
      closedByUs = true;
      ws.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [containerId, shortId, name]);

  useEffect(() => {
    const el = preRef.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    if (nearBottom) el.scrollTop = el.scrollHeight;
  }, [buffer]);

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
        <span
          className={cn(
            'h-1.5 w-1.5 rounded-full',
            error
              ? 'bg-destructive'
              : connected
                ? 'bg-emerald-500'
                : ended
                  ? 'bg-muted-foreground'
                  : 'bg-amber-500 animate-pulse',
          )}
        />
        {error
          ? t('common.error')
          : connected
            ? t('docker.logConnected')
            : ended
              ? t('docker.logEnded')
              : t('docker.logConnecting')}
      </div>
      {error && (
        <div className="rounded-xl border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
          {error}
        </div>
      )}
      <pre
        ref={preRef}
        className="max-h-[50vh] overflow-auto rounded-xl border border-border/40 bg-black/40 p-4 font-mono-data text-[11px] leading-relaxed text-emerald-100/90"
      >
        {buffer || (error ? '' : t('docker.logWaiting'))}
      </pre>
    </div>
  );
}

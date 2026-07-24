import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { motion } from 'framer-motion';
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip as RTooltip,
  XAxis,
  YAxis,
} from 'recharts';
import {
  Activity,
  AlertTriangle,
  ChevronDown,
  Cpu,
  Gauge,
  HardDrive,
  MemoryStick,
  Network,
  ShieldCheck,
  Thermometer,
  Wifi,
  Zap,
} from 'lucide-react';
import { api } from '@/lib/api';
import { Progress } from '@/components/ui/progress';
import { Badge } from '@/components/ui/badge';
import {
  formatBytes,
  formatPct,
  formatRate,
  cn,
} from '@/lib/utils';
import type { DashboardSummary, ArrayStatus, ParityStatus } from '@/types';
import { useSettingsStore, type ChartRange } from '@/stores/settings';
import { useAuthStore } from '@/stores/auth';
import {
  staggerContainer,
  fadeUpVariants,
  springGentle,
} from '@/lib/motion';

interface Sample {
  t: number;
  cpu: number;
  rx: number;
  tx: number;
  read: number;
  write: number;
}

const RANGE_SECONDS: Record<ChartRange, number> = {
  '60s': 60,
  '5m': 300,
  '30m': 1800,
  '2h': 7200,
};

export default function DashboardPage() {
  const { t } = useTranslation();
  const RANGE_LABELS: Record<ChartRange, string> = {
    '60s': t('time.1min'),
    '5m': t('time.5min'),
    '30m': t('time.30min'),
    '2h': t('time.2hr'),
  };
  const refreshInterval = useSettingsStore((s) => s.refreshInterval);
  const chartRange = useSettingsStore((s) => s.chartRange);
  const setChartRange = useSettingsStore((s) => s.setChartRange);
  const sshAvailable = useAuthStore((s) => s.sshAvailable);
  const apiAvailable = useAuthStore((s) => s.apiAvailable);

  const modeLabel =
    sshAvailable && apiAvailable ? t('connection.dual') :
    apiAvailable && !sshAvailable ? t('connection.api') :
    sshAvailable && !apiAvailable ? t('connection.ssh') : '';
  const { data, isLoading, isError } = useQuery({
    queryKey: ['dashboard'],
    queryFn: () => api.get<DashboardSummary>('/dashboard'),
    refetchInterval: refreshInterval || false,
  });

  const maxSamples = refreshInterval > 0
    ? Math.ceil(RANGE_SECONDS[chartRange] / (refreshInterval / 1000))
    : 60;
  const [history, setHistory] = useState<Sample[]>([]);
  useEffect(() => {
    if (!data) return;
    const net0 = (data.network && data.network[0]) ?? { rxBytesPerSec: 0, txBytesPerSec: 0 };
    const rw = data.arrayRwBytesPerSec ?? { read: 0, write: 0 };
    setHistory((prev) => {
      const next = [
        ...prev,
        {
          t: Date.now(),
          cpu: data.cpu?.usagePct ?? 0,
          rx: net0.rxBytesPerSec,
          tx: net0.txBytesPerSec,
          read: rw.read,
          write: rw.write,
        },
      ];
      return next.slice(-maxSamples);
    });
  }, [data, maxSamples]);

  return (
    <div className="p-5 md:p-6 space-y-5">
      {/* Hero header — editorial canvas */}
      <motion.div
        className="flex flex-wrap items-end justify-between gap-4"
        initial={{ opacity: 0, y: 18 }}
        animate={{ opacity: 1, y: 0 }}
        transition={springGentle}
      >
        <div>
          <div className="mb-2 text-[10px] font-semibold uppercase tracking-[0.3em] text-muted-foreground/65">
            System telemetry
          </div>
          <h1 className="text-display-md text-gradient-kinetic">{t('dashboard.title')}</h1>
          <div className="mt-2 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            {data && (
              <Badge variant="success" className="text-[10px] font-semibold tracking-wide px-2.5">
                {t('common.online')}
              </Badge>
            )}
            {data && modeLabel && (
              <Badge
                variant="secondary"
                className={cn(
                  'text-[10px] font-semibold tracking-wide px-2.5',
                  sshAvailable && apiAvailable
                    ? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20'
                    : apiAvailable
                      ? 'bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20'
                      : 'bg-sky-500/10 text-sky-600 dark:text-sky-400 border-sky-500/20',
                )}
              >
                <Zap className="mr-1 h-3 w-3" />
                {modeLabel}
              </Badge>
            )}
            <span className="text-xs">
              {t('dashboard.serverStatus')}{refreshInterval > 0 ? ` · ${refreshInterval / 1000}s` : ` · ${t('dashboard.refreshPaused')}`}
            </span>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {data && (
            <Badge variant="secondary" className="text-[10px] font-mono-data px-2.5 py-1">
              <Gauge className="mr-1 h-3 w-3" />
              {t('dashboard.load')} {(data.loadAvg?.[0] ?? 0).toFixed(2)} / {(data.loadAvg?.[1] ?? 0).toFixed(2)} / {(data.loadAvg?.[2] ?? 0).toFixed(2)}
            </Badge>
          )}
          {data && (
            <Badge variant="secondary" className="text-[10px] font-mono-data px-2.5 py-1">
              {t('dashboard.started')} {Math.floor(data.uptime / 3600)}h {Math.floor((data.uptime % 3600) / 60)}m
            </Badge>
          )}
          <select
            className="rounded-lg border border-border/50 bg-background/50 px-2.5 py-1.5 text-xs backdrop-blur-sm transition-colors hover:border-border focus:outline-none focus:ring-1 focus:ring-ring"
            value={chartRange}
            onChange={(e) => setChartRange(e.target.value as ChartRange)}
          >
            {(Object.keys(RANGE_LABELS) as ChartRange[]).map((r) => (
              <option key={r} value={r}>{RANGE_LABELS[r]}</option>
            ))}
          </select>
        </div>
      </motion.div>

      {isError && (
        <motion.div
          className="rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive"
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
        >
          {t('dashboard.cannotFetch')}
        </motion.div>
      )}

      {/* Degraded mode banner (API-only, no SSH) */}
      {data?.degraded && (
        <motion.div
          className="flex items-center gap-2 rounded-xl border border-amber-500/30 bg-amber-500/5 p-3 text-sm text-amber-600 dark:text-amber-400"
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
        >
          <Wifi className="h-4 w-4 shrink-0" />
          <span>{t('dashboard.degradedNotice')}</span>
        </motion.div>
      )}

      {/* Bento stat cards */}
      <motion.div
        className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4"
        variants={staggerContainer}
        initial="hidden"
        animate="visible"
      >
        <StatCard
          title="CPU"
          icon={Cpu}
          isLoading={isLoading}
          accent="text-orange-500"
          accentGlow="from-orange-500/20"
          value={data ? formatPct(data.cpu.usagePct) : '—'}
          subtitle={data?.cpu.modelName ?? ''}
        />
        <StatCard
          title={t('dashboard.memory')}
          icon={MemoryStick}
          isLoading={isLoading}
          accent="text-sky-500"
          accentGlow="from-sky-500/20"
          value={
            data?.memory
              ? `${formatBytes(data.memory.usedBytes)} / ${formatBytes(data.memory.totalBytes)}`
              : '—'
          }
          subtitle={data?.memory ? formatPct(data.memory.usagePct) : ''}
          progress={data?.memory?.usagePct}
        />
        <StatCard
          title={t('dashboard.network')}
          icon={Network}
          isLoading={isLoading}
          accent="text-emerald-500"
          accentGlow="from-emerald-500/20"
          value={
            data?.network?.[0]
              ? `${formatRate(data.network[0].rxBytesPerSec)} ↓ ${formatRate(
                  data.network[0].txBytesPerSec,
                )} ↑`
              : '—'
          }
          subtitle={data?.network?.[0]?.iface ?? ''}
        />
        <StatCard
          title={t('dashboard.arrayRW')}
          icon={HardDrive}
          isLoading={isLoading}
          accent="text-violet-500"
          accentGlow="from-violet-500/20"
          value={
            data?.arrayRwBytesPerSec
              ? `${formatRate(data.arrayRwBytesPerSec.read)} / ${formatRate(
                  data.arrayRwBytesPerSec.write,
                )}`
              : '—'
          }
          subtitle={t('dashboard.rw')}
        />
      </motion.div>

      {/* Array / Parity summary */}
      <ArrayStatusSummary />

      {/* Charts — Bento grid */}
      <motion.div
        className="grid gap-4 lg:grid-cols-2"
        variants={staggerContainer}
        initial="hidden"
        animate="visible"
      >
        <ChartCard
          icon={<Cpu className="h-4 w-4 text-orange-500" />}
          title={t('dashboard.cpuUsage')}
          description={`${t('dashboard.recent')} ${RANGE_LABELS[chartRange]}`}
        >
          <LineChart data={history} dataKey="cpu" color="hsl(var(--chart-cpu))" unit="%" />
        </ChartCard>

        <ChartCard
          icon={<Network className="h-4 w-4 text-emerald-500" />}
          title={t('dashboard.networkTraffic')}
          description={t('dashboard.rxTx')}
        >
          <DualLineChart data={history} />
        </ChartCard>

        <ChartCard
          icon={<Activity className="h-4 w-4 text-violet-500" />}
          title={t('dashboard.arrayRWSpeed')}
          description={t('dashboard.rw')}
        >
          <RwChart data={history} />
        </ChartCard>

        <ChartCard
          icon={<Thermometer className="h-4 w-4 text-rose-500" />}
          title={t('dashboard.perCoreStatus')}
          description={t('dashboard.perCoreDetail')}
        >
          <CoreStatus data={data} isLoading={isLoading} />
        </ChartCard>
      </motion.div>
    </div>
  );
}

/* ── Chart card wrapper ── */
function ChartCard({
  icon,
  title,
  description,
  children,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <motion.div
      className="card-bento overflow-hidden"
      variants={fadeUpVariants}
      whileHover={{ y: -2 }}
      transition={springGentle}
    >
      <div className="px-5 pt-5 pb-2">
        <div className="flex items-center gap-2">
          {icon}
          <h3 className="text-sm font-semibold">{title}</h3>
        </div>
        <p className="text-[11px] text-muted-foreground/70 mt-0.5">{description}</p>
      </div>
      <div className="px-2 pb-2 h-56">
        {children}
      </div>
    </motion.div>
  );
}

/* ── Array / Parity status ── */
function ArrayStatusSummary() {
  const { t } = useTranslation();
  const { data: storage } = useQuery({
    queryKey: ['storage-summary'],
    queryFn: () => api.get<ArrayStatus>('/storage'),
    refetchInterval: 10000,
  });
  const { data: parity } = useQuery({
    queryKey: ['parity-summary'],
    queryFn: () => api.get<ParityStatus>('/storage/parity-status'),
    refetchInterval: 5000,
  });

  if (!storage) return null;

  const disks = storage.disks ?? [];
  const cacheDisks = storage.cacheDisks ?? [];
  const totalDisks = disks.length + cacheDisks.length;
  const problemDisks = [...disks, ...cacheDisks].filter(
    (d) => d.status === 'warning' || d.status === 'critical',
  ).length;

  return (
    <motion.div
      className="flex flex-wrap items-center gap-3 rounded-xl border border-border/30 bg-card/30 backdrop-blur-sm px-4 py-2.5"
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={springGentle}
    >
      <div className="flex items-center gap-1.5 text-xs">
        <HardDrive className="h-3.5 w-3.5 text-muted-foreground/60" />
        <span className="text-muted-foreground/70">{t('dashboard.array')}</span>
        <Badge
          variant={storage.state === 'started' ? 'success' : 'secondary'}
          className="text-[9px] px-1.5 py-0 leading-none font-semibold tracking-wide"
        >
          {storage.state === 'started' ? t('dashboard.started') : storage.state === 'stopped' ? t('dashboard.stopped') : storage.state}
        </Badge>
        <span className="text-muted-foreground/60">
          {totalDisks} {t('dashboard.diskCount')}
        </span>
        {problemDisks > 0 && (
          <span className="flex items-center gap-0.5 text-amber-500">
            <AlertTriangle className="h-3 w-3" />
            {problemDisks} {t('dashboard.issue')}
          </span>
        )}
      </div>

      <div className="flex items-center gap-1.5 text-xs">
        <ShieldCheck className="h-3.5 w-3.5 text-muted-foreground/60" />
        <span className="text-muted-foreground/70">{t('dashboard.parity')}</span>
        {parity?.state === 'checking' ? (
          <>
            <Badge variant="success" className="text-[9px] px-1.5 py-0 leading-none animate-pulse font-semibold">
              {t('dashboard.checking')}
            </Badge>
            <span className="tabular-nums font-mono-data font-medium">{formatPct(parity.progress)}</span>
            <span className="text-muted-foreground/60">· {parity.speed} · {t('dashboard.remaining')} {parity.remaining}</span>
          </>
        ) : (
          <Badge variant="secondary" className="text-[9px] px-1.5 py-0 leading-none">
            {parity?.state === 'idle' ? t('dashboard.idle') : t('dashboard.unknown')}
          </Badge>
        )}
      </div>
    </motion.div>
  );
}

/* ── Stat card — bento style with dramatic metrics ── */
function StatCard({
  title,
  icon: Icon,
  isLoading,
  value,
  subtitle,
  accent,
  accentGlow,
  progress,
}: {
  title: string;
  icon: React.ComponentType<{ className?: string }>;
  isLoading: boolean;
  value: string;
  subtitle?: string;
  accent?: string;
  accentGlow?: string;
  progress?: number;
}) {
  return (
    <motion.div
      className={cn(
        'card-bento overflow-hidden p-5',
        accentGlow && `bg-gradient-to-br ${accentGlow} to-transparent`,
      )}
      variants={fadeUpVariants}
      whileHover={{ y: -3 }}
      transition={springGentle}
    >
      <div className="flex items-center justify-between">
        <span className="text-[11px] font-semibold uppercase tracking-widest text-muted-foreground/60">{title}</span>
        <div className={cn('grid h-8 w-8 place-items-center rounded-lg bg-background/50', accent)}>
          <Icon className="h-4 w-4" />
        </div>
      </div>
      {isLoading ? (
        <div className="mt-3 h-8 w-32 skeleton-shimmer rounded" />
      ) : (
        <motion.div
          className="mt-2 text-2xl font-bold font-mono-data tabular-nums tracking-tight"
          initial={{ opacity: 0, y: 4 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ delay: 0.1, ...springGentle }}
        >
          {value}
        </motion.div>
      )}
      {subtitle && (
        <div className="mt-1 truncate text-[11px] text-muted-foreground/60">
          {subtitle}
        </div>
      )}
      {progress !== undefined && (
        <Progress
          className="mt-3 h-1"
          value={progress}
          indicatorClassName={
            progress > 90 ? 'bg-destructive' : progress > 75 ? 'bg-warning' : 'bg-primary'
          }
        />
      )}
    </motion.div>
  );
}

/* ── Charts (same logic, refined visuals) ── */

function LineChart({
  data,
  dataKey,
  color,
  unit,
}: {
  data: Sample[];
  dataKey: 'cpu';
  color: string;
  unit: string;
}) {
  return (
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart data={data} margin={{ top: 5, right: 12, left: 0, bottom: 0 }}>
        <defs>
          <linearGradient id={`g-${dataKey}`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor={color} stopOpacity={0.3} />
            <stop offset="95%" stopColor={color} stopOpacity={0} />
          </linearGradient>
        </defs>
        <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" strokeOpacity={0.3} vertical={false} />
        <XAxis
          dataKey="t"
          tickFormatter={(t) => new Date(t).toLocaleTimeString().slice(0, 5)}
          tick={{ fontSize: 10, fill: 'hsl(var(--muted-foreground))' }}
          stroke="hsl(var(--border))"
          strokeOpacity={0.3}
        />
        <YAxis
          domain={[0, 100]}
          tick={{ fontSize: 10, fill: 'hsl(var(--muted-foreground))' }}
          stroke="hsl(var(--border))"
          strokeOpacity={0.3}
          width={30}
        />
        <RTooltip
          contentStyle={{
            background: 'hsl(var(--popover))',
            border: '1px solid hsl(var(--border))',
            borderRadius: 12,
            fontSize: 12,
            boxShadow: '0 8px 32px rgba(0,0,0,0.3)',
          }}
          labelFormatter={(t) => new Date(Number(t)).toLocaleTimeString()}
          formatter={(v: number) => [`${v.toFixed(1)}${unit}`, '']}
        />
        <Area
          type="monotone"
          dataKey={dataKey}
          stroke={color}
          strokeWidth={2}
          fill={`url(#g-${dataKey})`}
          isAnimationActive={false}
        />
      </AreaChart>
    </ResponsiveContainer>
  );
}

function DualLineChart({ data }: { data: Sample[] }) {
  const { t } = useTranslation();
  const rxColor = 'hsl(var(--chart-rx))';
  const txColor = 'hsl(var(--chart-tx))';
  return (
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart data={data} margin={{ top: 5, right: 12, left: 0, bottom: 0 }}>
        <defs>
          <linearGradient id="g-rx" x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor={rxColor} stopOpacity={0.3} />
            <stop offset="95%" stopColor={rxColor} stopOpacity={0} />
          </linearGradient>
          <linearGradient id="g-tx" x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor={txColor} stopOpacity={0.3} />
            <stop offset="95%" stopColor={txColor} stopOpacity={0} />
          </linearGradient>
        </defs>
        <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" strokeOpacity={0.3} vertical={false} />
        <XAxis
          dataKey="t"
          tickFormatter={(t) => new Date(t).toLocaleTimeString().slice(0, 5)}
          tick={{ fontSize: 10, fill: 'hsl(var(--muted-foreground))' }}
          stroke="hsl(var(--border))"
          strokeOpacity={0.3}
        />
        <YAxis
          tickFormatter={(v) => formatBytes(v, 0)}
          tick={{ fontSize: 10, fill: 'hsl(var(--muted-foreground))' }}
          stroke="hsl(var(--border))"
          strokeOpacity={0.3}
          width={48}
        />
        <RTooltip
          contentStyle={{
            background: 'hsl(var(--popover))',
            border: '1px solid hsl(var(--border))',
            borderRadius: 12,
            fontSize: 12,
            boxShadow: '0 8px 32px rgba(0,0,0,0.3)',
          }}
          labelFormatter={(t) => new Date(Number(t)).toLocaleTimeString()}
          formatter={(v: number, n: string) => [formatRate(v), n === 'rx' ? t('dashboard.rx') : t('dashboard.tx')]}
        />
        <Area type="monotone" dataKey="rx" stroke={rxColor} strokeWidth={2} fill="url(#g-rx)" isAnimationActive={false} />
        <Area type="monotone" dataKey="tx" stroke={txColor} strokeWidth={2} fill="url(#g-tx)" isAnimationActive={false} />
      </AreaChart>
    </ResponsiveContainer>
  );
}

function RwChart({ data }: { data: Sample[] }) {
  const { t } = useTranslation();
  const rdColor = 'hsl(var(--chart-rd))';
  const wrColor = 'hsl(var(--chart-wr))';
  return (
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart data={data} margin={{ top: 5, right: 12, left: 0, bottom: 0 }}>
        <defs>
          <linearGradient id="g-rd" x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor={rdColor} stopOpacity={0.3} />
            <stop offset="95%" stopColor={rdColor} stopOpacity={0} />
          </linearGradient>
          <linearGradient id="g-wr" x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor={wrColor} stopOpacity={0.3} />
            <stop offset="95%" stopColor={wrColor} stopOpacity={0} />
          </linearGradient>
        </defs>
        <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" strokeOpacity={0.3} vertical={false} />
        <XAxis
          dataKey="t"
          tickFormatter={(t) => new Date(t).toLocaleTimeString().slice(0, 5)}
          tick={{ fontSize: 10, fill: 'hsl(var(--muted-foreground))' }}
          stroke="hsl(var(--border))"
          strokeOpacity={0.3}
        />
        <YAxis
          tickFormatter={(v) => formatBytes(v, 0)}
          tick={{ fontSize: 10, fill: 'hsl(var(--muted-foreground))' }}
          stroke="hsl(var(--border))"
          strokeOpacity={0.3}
          width={48}
        />
        <RTooltip
          contentStyle={{
            background: 'hsl(var(--popover))',
            border: '1px solid hsl(var(--border))',
            borderRadius: 12,
            fontSize: 12,
            boxShadow: '0 8px 32px rgba(0,0,0,0.3)',
          }}
          labelFormatter={(t) => new Date(Number(t)).toLocaleTimeString()}
          formatter={(v: number, n: string) => [formatRate(v), n === 'read' ? t('dashboard.read') : t('dashboard.write')]}
        />
        <Area type="monotone" dataKey="read" stroke={rdColor} strokeWidth={2} fill="url(#g-rd)" isAnimationActive={false} />
        <Area type="monotone" dataKey="write" stroke={wrColor} strokeWidth={2} fill="url(#g-wr)" isAnimationActive={false} />
      </AreaChart>
    </ResponsiveContainer>
  );
}

/* ── Per-core status ── */
function CoreStatus({
  data,
  isLoading,
}: {
  data?: DashboardSummary;
  isLoading: boolean;
}) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);

  if (isLoading) return <div className="h-32 skeleton-shimmer rounded-lg" />;
  if (!data) return <div className="text-sm text-muted-foreground">{t('dashboard.noData')}</div>;

  const usage = data.cpu.perCoreUsagePct ?? [];
  const temps = data.cpu.perCoreTempC ?? [];
  if (usage.length === 0 && temps.length === 0) {
    return <div className="text-sm text-muted-foreground">{t('dashboard.cpuNoData')}</div>;
  }
  const cores = Math.max(usage.length, temps.length);

  const COLLAPSE_THRESHOLD = 8;
  const needsCollapse = cores > COLLAPSE_THRESHOLD;
  const visibleCount = (!needsCollapse || expanded) ? cores : COLLAPSE_THRESHOLD;

  const avgUsage = usage.length > 0
    ? usage.reduce((a: number, b: number | undefined) => a + (typeof b === 'number' ? b : 0), 0) / usage.length
    : null;
  const maxTemp = temps.length > 0
    ? Math.max(...temps.filter((t: number | undefined): t is number => typeof t === 'number' && t > 0))
    : null;

  return (
    <div>
      {needsCollapse && !expanded && avgUsage !== null && (
        <div className="mb-3 flex items-center gap-4 text-xs text-muted-foreground/60">
          <span className="flex items-center gap-1">
            <Cpu className="h-3.5 w-3.5" />
            {cores} {t('dashboard.core')}
          </span>
          <span className="tabular-nums font-mono-data">
            {t('dashboard.avg')} <span className={cn('font-semibold', avgUsage >= 70 ? 'text-warning' : avgUsage >= 90 ? 'text-destructive' : 'text-foreground')}>{avgUsage.toFixed(0)}%</span>
          </span>
          {maxTemp !== null && maxTemp > 0 && (
            <span className="tabular-nums font-mono-data">
              {t('dashboard.max')} <span className={cn('font-semibold', maxTemp >= 80 ? 'text-destructive' : maxTemp >= 65 ? 'text-warning' : 'text-foreground')}>{maxTemp}°C</span>
            </span>
          )}
        </div>
      )}

      <div className={cn(
        'grid gap-2',
        expanded
          ? 'grid-cols-6 sm:grid-cols-8 md:grid-cols-10 lg:grid-cols-12'
          : 'grid-cols-4 sm:grid-cols-6 md:grid-cols-8',
      )}>
        {Array.from({ length: visibleCount }).map((_, i) => {
          const u = usage[i];
          const fillPct = typeof u === 'number' ? Math.max(0, Math.min(100, u)) : 0;
          const fillColor =
            fillPct >= 90 ? 'bg-destructive' : fillPct >= 70 ? 'bg-warning' : 'bg-success';
          const temp = temps[i];
          const hasTemp = typeof temp === 'number' && temp > 0;
          const tempDisp = hasTemp ? Math.round(temp) : null;
          const tempColor =
            hasTemp && tempDisp !== null
              ? tempDisp >= 80
                ? 'text-destructive'
                : tempDisp >= 65
                  ? 'text-warning'
                  : 'text-muted-foreground'
              : 'text-muted-foreground';

          if (expanded && cores > 16) {
            return (
              <div key={i} className="flex items-center gap-1.5 rounded-lg bg-muted/30 px-1.5 py-1">
                <div className="text-[9px] text-muted-foreground/60 font-mono-data w-5">C{i}</div>
                <div className="flex-1 h-1.5 rounded-full bg-muted overflow-hidden">
                  <motion.div
                    className={cn('h-full rounded-full', fillColor)}
                    initial={{ width: 0 }}
                    animate={{ width: `${fillPct}%` }}
                    transition={springGentle}
                  />
                </div>
                <div className={cn('text-[9px] font-mono-data w-8 text-right', tempColor)}>
                  {tempDisp !== null ? `${tempDisp}°` : '—'}
                </div>
              </div>
            );
          }

          return (
            <div key={i} className="space-y-1 text-center">
              <div className="relative h-20 overflow-hidden rounded-lg bg-muted/30">
                <motion.div
                  className={cn('absolute bottom-0 w-full', fillColor)}
                  initial={{ height: 0 }}
                  animate={{ height: `${fillPct}%` }}
                  transition={springGentle}
                />
                <div className="absolute inset-0 grid place-items-center text-[10px] font-bold font-mono-data tabular-nums">
                  {typeof u === 'number' ? `${u.toFixed(0)}%` : '—'}
                </div>
              </div>
              <div className="text-[10px] text-muted-foreground/50 font-mono-data">C{i}</div>
              <div className={cn('text-[10px] font-mono-data tabular-nums', tempColor)}>
                {tempDisp !== null ? `${tempDisp}°C` : '—'}
              </div>
            </div>
          );
        })}
      </div>

      {needsCollapse && (
        <motion.button
          onClick={() => setExpanded(!expanded)}
          className="mt-3 flex items-center gap-1 text-xs text-muted-foreground/60 hover:text-foreground transition-colors"
          whileHover={{ x: 2 }}
        >
          <ChevronDown className={cn('h-3.5 w-3.5 transition-transform', expanded && 'rotate-180')} />
          {expanded ? t('dashboard.collapse') : `${t('dashboard.viewAll')} ${cores} ${t('dashboard.core')}`}
        </motion.button>
      )}
    </div>
  );
}

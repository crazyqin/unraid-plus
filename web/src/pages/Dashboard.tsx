import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
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
  Cpu,
  Gauge,
  HardDrive,
  MemoryStick,
  Network,
  ShieldCheck,
  Thermometer,
} from 'lucide-react';
import { api } from '@/lib/api';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { Skeleton } from '@/components/ui/skeleton';
import { Badge } from '@/components/ui/badge';
import {
  formatBytes,
  formatPct,
  formatRate,
  cn,
} from '@/lib/utils';
import type { DashboardSummary, ArrayStatus, ParityStatus } from '@/types';
import { useSettingsStore, type ChartRange } from '@/stores/settings';

interface Sample {
  t: number;
  cpu: number;
  rx: number;
  tx: number;
  read: number;
  write: number;
}

/** Map chart range label to seconds, then derive max buffer size from refresh interval. */
const RANGE_SECONDS: Record<ChartRange, number> = {
  '60s': 60,
  '5m': 300,
  '30m': 1800,
  '2h': 7200,
};

const RANGE_LABELS: Record<ChartRange, string> = {
  '60s': '1 分钟',
  '5m': '5 分钟',
  '30m': '30 分钟',
  '2h': '2 小时',
};

export default function DashboardPage() {
  const refreshInterval = useSettingsStore((s) => s.refreshInterval);
  const chartRange = useSettingsStore((s) => s.chartRange);
  const setChartRange = useSettingsStore((s) => s.setChartRange);
  const { data, isLoading, isError } = useQuery({
    queryKey: ['dashboard'],
    queryFn: () => api.get<DashboardSummary>('/dashboard'),
    refetchInterval: refreshInterval || false,
  });

  // Rolling history buffer. Size depends on chartRange × refreshInterval.
  const maxSamples = refreshInterval > 0
    ? Math.ceil(RANGE_SECONDS[chartRange] / (refreshInterval / 1000))
    : 60;
  const [history, setHistory] = useState<Sample[]>([]);
  useEffect(() => {
    if (!data) return;
    setHistory((prev) => {
      const next = [
        ...prev,
        {
          t: Date.now(),
          cpu: data.cpu.usagePct,
          rx: data.network[0]?.rxBytesPerSec ?? 0,
          tx: data.network[0]?.txBytesPerSec ?? 0,
          read: data.arrayRwBytesPerSec.read,
          write: data.arrayRwBytesPerSec.write,
        },
      ];
      return next.slice(-maxSamples);
    });
  }, [data, maxSamples]);

  return (
    <div className="space-y-4 p-4 md:p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className={cn(
            'flex h-10 w-10 items-center justify-center rounded-lg',
            data ? 'bg-success/15 text-success' : 'bg-muted text-muted-foreground',
          )}>
            <Activity className="h-5 w-5" />
          </div>
          <div>
            <h1 className="text-xl font-semibold">仪表盘</h1>
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              {data && (
                <Badge variant="success" className="text-[10px]">
                  在线
                </Badge>
              )}
              <span>
                服务器实时状态{refreshInterval > 0 ? ` · 每 ${refreshInterval / 1000}s 刷新` : ' · 已暂停刷新'}
              </span>
            </div>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {data && (
            <Badge variant="secondary" className="text-[10px] tabular-nums">
              <Gauge className="mr-1 h-3 w-3" />
              负载 {data.loadAvg[0].toFixed(2)} / {data.loadAvg[1].toFixed(2)} / {data.loadAvg[2].toFixed(2)}
            </Badge>
          )}
          {data && (
            <Badge variant="secondary" className="text-[10px]">
              启动 {Math.floor(data.uptime / 3600)}h {Math.floor((data.uptime % 3600) / 60)}m
            </Badge>
          )}
          <select
            className="rounded border bg-background px-2 py-1 text-xs"
            value={chartRange}
            onChange={(e) => setChartRange(e.target.value as ChartRange)}
          >
            {(Object.keys(RANGE_LABELS) as ChartRange[]).map((r) => (
              <option key={r} value={r}>{RANGE_LABELS[r]}</option>
            ))}
          </select>
        </div>
      </div>

      {isError && (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
          无法获取数据。请确认后端已连接到 Unraid。
        </div>
      )}

      {/* Top stats */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          title="CPU"
          icon={Cpu}
          isLoading={isLoading}
          accent="text-orange-500"
          value={data ? formatPct(data.cpu.usagePct) : '—'}
          subtitle={data?.cpu.modelName ?? ''}
        />
        <StatCard
          title="内存"
          icon={MemoryStick}
          isLoading={isLoading}
          accent="text-sky-500"
          value={
            data
              ? `${formatBytes(data.memory.usedBytes)} / ${formatBytes(data.memory.totalBytes)}`
              : '—'
          }
          subtitle={data ? formatPct(data.memory.usagePct) : ''}
          progress={data?.memory.usagePct}
        />
        <StatCard
          title="网络"
          icon={Network}
          isLoading={isLoading}
          accent="text-emerald-500"
          value={
            data
              ? `${formatRate(data.network[0]?.rxBytesPerSec ?? 0)} ↓ ${formatRate(
                  data.network[0]?.txBytesPerSec ?? 0,
                )} ↑`
              : '—'
          }
          subtitle={data?.network[0]?.iface ?? ''}
        />
        <StatCard
          title="阵列读写"
          icon={HardDrive}
          isLoading={isLoading}
          accent="text-violet-500"
          value={
            data
              ? `${formatRate(data.arrayRwBytesPerSec.read)} / ${formatRate(
                  data.arrayRwBytesPerSec.write,
                )}`
              : '—'
          }
          subtitle="读 / 写"
        />
      </div>

      {/* Array / Parity summary (polls storage + parity) */}
      <ArrayStatusSummary />

      {/* Charts */}
      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Cpu className="h-4 w-4 text-orange-500" /> CPU 使用率
            </CardTitle>
            <CardDescription>最近 {RANGE_LABELS[chartRange]}</CardDescription>
          </CardHeader>
          <CardContent className="h-56">
            <LineChart data={history} dataKey="cpu" color="hsl(var(--chart-cpu))" unit="%" />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Network className="h-4 w-4 text-emerald-500" /> 网络流量
            </CardTitle>
            <CardDescription>接收 / 发送</CardDescription>
          </CardHeader>
          <CardContent className="h-56">
            <DualLineChart data={history} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Activity className="h-4 w-4 text-violet-500" /> 阵列读写速率
            </CardTitle>
            <CardDescription>读 / 写</CardDescription>
          </CardHeader>
          <CardContent className="h-56">
            <RwChart data={history} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Thermometer className="h-4 w-4 text-rose-500" /> 各核状态
            </CardTitle>
            <CardDescription>每核使用率 / 温度</CardDescription>
          </CardHeader>
          <CardContent>
            <CoreStatus data={data} isLoading={isLoading} />
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

/* --------------------------------- bits ---------------------------------- */

/* Array / Parity status summary strip */
function ArrayStatusSummary() {
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

  const totalDisks = storage.disks.length + storage.cacheDisks.length;
  const problemDisks = [...storage.disks, ...storage.cacheDisks].filter(
    (d) => d.status === 'warning' || d.status === 'critical',
  ).length;

  return (
    <div className="flex flex-wrap items-center gap-3">
      {/* Array state */}
      <div className="flex items-center gap-1.5 text-xs">
        <HardDrive className="h-3.5 w-3.5 text-muted-foreground" />
        <span className="text-muted-foreground">阵列:</span>
        <Badge
          variant={storage.state === 'started' ? 'success' : 'secondary'}
          className="text-[9px] px-1.5 py-0 leading-none"
        >
          {storage.state === 'started' ? '已启动' : storage.state === 'stopped' ? '已停止' : storage.state}
        </Badge>
        <span className="text-muted-foreground">
          {totalDisks} 块磁盘
        </span>
        {problemDisks > 0 && (
          <span className="flex items-center gap-0.5 text-amber-500">
            <AlertTriangle className="h-3 w-3" />
            {problemDisks} 问题
          </span>
        )}
      </div>

      {/* Parity status */}
      <div className="flex items-center gap-1.5 text-xs">
        <ShieldCheck className="h-3.5 w-3.5 text-muted-foreground" />
        <span className="text-muted-foreground">校验:</span>
        {parity?.state === 'checking' ? (
          <>
            <Badge variant="success" className="text-[9px] px-1.5 py-0 leading-none animate-pulse">
              检查中
            </Badge>
            <span className="tabular-nums font-medium">{formatPct(parity.progress)}</span>
            <span className="text-muted-foreground">· {parity.speed} · 剩余 {parity.remaining}</span>
          </>
        ) : (
          <Badge variant="secondary" className="text-[9px] px-1.5 py-0 leading-none">
            {parity?.state === 'idle' ? '空闲' : '未知'}
          </Badge>
        )}
      </div>
    </div>
  );
}

function StatCard({
  title,
  icon: Icon,
  isLoading,
  value,
  subtitle,
  accent,
  progress,
}: {
  title: string;
  icon: React.ComponentType<{ className?: string }>;
  isLoading: boolean;
  value: string;
  subtitle?: string;
  accent?: string;
  progress?: number;
}) {
  return (
    <Card>
      <CardContent className="p-4">
        <div className="flex items-center justify-between">
          <span className="text-xs text-muted-foreground">{title}</span>
          <Icon className={cn('h-4 w-4', accent)} />
        </div>
        {isLoading ? (
          <Skeleton className="mt-2 h-6 w-32" />
        ) : (
          <div className="mt-1 text-lg font-semibold tabular-nums">{value}</div>
        )}
        {subtitle && (
          <div className="mt-1 truncate text-xs text-muted-foreground">
            {subtitle}
          </div>
        )}
        {progress !== undefined && (
          <Progress
            className="mt-2"
            value={progress}
            indicatorClassName={
              progress > 90 ? 'bg-destructive' : progress > 75 ? 'bg-warning' : 'bg-primary'
            }
          />
        )}
      </CardContent>
    </Card>
  );
}

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
      <AreaChart data={data} margin={{ top: 5, right: 8, left: 0, bottom: 0 }}>
        <defs>
          <linearGradient id={`g-${dataKey}`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor={color} stopOpacity={0.4} />
            <stop offset="95%" stopColor={color} stopOpacity={0} />
          </linearGradient>
        </defs>
        <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
        <XAxis
          dataKey="t"
          tickFormatter={(t) => new Date(t).toLocaleTimeString().slice(0, 5)}
          tick={{ fontSize: 10, fill: 'hsl(var(--muted-foreground))' }}
          stroke="hsl(var(--border))"
        />
        <YAxis
          domain={[0, 100]}
          tick={{ fontSize: 10, fill: 'hsl(var(--muted-foreground))' }}
          stroke="hsl(var(--border))"
          width={30}
        />
        <RTooltip
          contentStyle={{
            background: 'hsl(var(--popover))',
            border: '1px solid hsl(var(--border))',
            borderRadius: 8,
            fontSize: 12,
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
  const rxColor = 'hsl(var(--chart-rx))';
  const txColor = 'hsl(var(--chart-tx))';
  return (
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart data={data} margin={{ top: 5, right: 8, left: 0, bottom: 0 }}>
        <defs>
          <linearGradient id="g-rx" x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor={rxColor} stopOpacity={0.4} />
            <stop offset="95%" stopColor={rxColor} stopOpacity={0} />
          </linearGradient>
          <linearGradient id="g-tx" x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor={txColor} stopOpacity={0.4} />
            <stop offset="95%" stopColor={txColor} stopOpacity={0} />
          </linearGradient>
        </defs>
        <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
        <XAxis
          dataKey="t"
          tickFormatter={(t) => new Date(t).toLocaleTimeString().slice(0, 5)}
          tick={{ fontSize: 10, fill: 'hsl(var(--muted-foreground))' }}
          stroke="hsl(var(--border))"
        />
        <YAxis
          tickFormatter={(v) => formatBytes(v, 0)}
          tick={{ fontSize: 10, fill: 'hsl(var(--muted-foreground))' }}
          stroke="hsl(var(--border))"
          width={48}
        />
        <RTooltip
          contentStyle={{
            background: 'hsl(var(--popover))',
            border: '1px solid hsl(var(--border))',
            borderRadius: 8,
            fontSize: 12,
          }}
          labelFormatter={(t) => new Date(Number(t)).toLocaleTimeString()}
          formatter={(v: number, n: string) => [formatRate(v), n === 'rx' ? '↓ 接收' : '↑ 发送']}
        />
        <Area type="monotone" dataKey="rx" stroke={rxColor} strokeWidth={2} fill="url(#g-rx)" isAnimationActive={false} />
        <Area type="monotone" dataKey="tx" stroke={txColor} strokeWidth={2} fill="url(#g-tx)" isAnimationActive={false} />
      </AreaChart>
    </ResponsiveContainer>
  );
}

function RwChart({ data }: { data: Sample[] }) {
  const rdColor = 'hsl(var(--chart-rd))';
  const wrColor = 'hsl(var(--chart-wr))';
  return (
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart data={data} margin={{ top: 5, right: 8, left: 0, bottom: 0 }}>
        <defs>
          <linearGradient id="g-rd" x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor={rdColor} stopOpacity={0.4} />
            <stop offset="95%" stopColor={rdColor} stopOpacity={0} />
          </linearGradient>
          <linearGradient id="g-wr" x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor={wrColor} stopOpacity={0.4} />
            <stop offset="95%" stopColor={wrColor} stopOpacity={0} />
          </linearGradient>
        </defs>
        <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
        <XAxis
          dataKey="t"
          tickFormatter={(t) => new Date(t).toLocaleTimeString().slice(0, 5)}
          tick={{ fontSize: 10, fill: 'hsl(var(--muted-foreground))' }}
          stroke="hsl(var(--border))"
        />
        <YAxis
          tickFormatter={(v) => formatBytes(v, 0)}
          tick={{ fontSize: 10, fill: 'hsl(var(--muted-foreground))' }}
          stroke="hsl(var(--border))"
          width={48}
        />
        <RTooltip
          contentStyle={{
            background: 'hsl(var(--popover))',
            border: '1px solid hsl(var(--border))',
            borderRadius: 8,
            fontSize: 12,
          }}
          labelFormatter={(t) => new Date(Number(t)).toLocaleTimeString()}
          formatter={(v: number, n: string) => [formatRate(v), n === 'read' ? '读' : '写']}
        />
        <Area type="monotone" dataKey="read" stroke={rdColor} strokeWidth={2} fill="url(#g-rd)" isAnimationActive={false} />
        <Area type="monotone" dataKey="write" stroke={wrColor} strokeWidth={2} fill="url(#g-wr)" isAnimationActive={false} />
      </AreaChart>
    </ResponsiveContainer>
  );
}

/**
 * Per-core combined display: usage bar (fill = busy %, color band = busy %)
 * with the core label and temperature in °C underneath.
 *
 * v0.3 fix: previously this was a temperature-only widget because the
 * backend's `cat /proc/stat | head -n 1` stripped the per-core rows and
 * `computeCPUUsage` returned all-zeros for the perCoreUsagePct slice. The
 * handler now reads full /proc/stat and parses cpuN rows via parseProcStat,
 * so data.cpu.perCoreUsagePct[i] is the真实 busy percentage of logical
 * core i between the two snapshots ~900ms apart.
 *
 * The usage and temp arrays are both indexed by logical core number. They
 * may have different lengths on some hosts (thermal_zone count != nproc),
 * in which case we render up to max(usage.length, temp.length) columns and
 * only show the fields available per core.
 */
function CoreStatus({
  data,
  isLoading,
}: {
  data?: DashboardSummary;
  isLoading: boolean;
}) {
  if (isLoading) return <Skeleton className="h-32 w-full" />;
  if (!data) return <div className="text-sm text-muted-foreground">暂无数据</div>;

  const usage = data.cpu.perCoreUsagePct ?? [];
  const temps = data.cpu.perCoreTempC ?? [];
  if (usage.length === 0 && temps.length === 0) {
    return <div className="text-sm text-muted-foreground">该 CPU 未提供使用率/温度读数</div>;
  }
  const cores = Math.max(usage.length, temps.length);

  return (
    <div className="grid grid-cols-4 gap-2 sm:grid-cols-6 md:grid-cols-8">
      {Array.from({ length: cores }).map((_, i) => {
        const u = usage[i];
        // Usage-based fill + color band: 0-70% green, 70-90% amber, 90%+ red.
        // (Temperature is shown as a number underneath, not color-coded, to
        // avoid conflating two channels in the same bar.)
        const fillPct = typeof u === 'number' ? Math.max(0, Math.min(100, u)) : 0;
        const fillColor =
          fillPct >= 90 ? 'bg-destructive' : fillPct >= 70 ? 'bg-warning' : 'bg-success';
        const t = temps[i];
        // -1 means sensor unavailable; 0°C is nonsensical for a running CPU
        const hasTemp = typeof t === 'number' && t > 0;
        const tempColor =
          hasTemp
            ? t >= 80
              ? 'text-destructive'
              : t >= 65
                ? 'text-warning'
                : 'text-muted-foreground'
            : 'text-muted-foreground';
        return (
          <div key={i} className="space-y-1 text-center">
            <div className="relative h-20 overflow-hidden rounded bg-muted">
              <div
                className={cn('absolute bottom-0 w-full transition-[height]', fillColor)}
                style={{ height: `${fillPct}%` }}
              />
              <div className="absolute inset-0 grid place-items-center text-[10px] font-medium tabular-nums">
                {typeof u === 'number' ? `${u.toFixed(0)}%` : '—'}
              </div>
            </div>
            <div className="text-[10px] text-muted-foreground">C{i}</div>
            <div className={cn('text-[10px] tabular-nums', tempColor)}>
              {hasTemp ? `${t}°C` : '—'}
            </div>
          </div>
        );
      })}
    </div>
  );
}

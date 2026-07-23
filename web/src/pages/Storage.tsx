import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  HardDrive,
  Loader2,
  Play,
  RefreshCw,
  ShieldCheck,
  Square,
  Thermometer,
  XCircle,
  Usb,
  Zap,
  Cable,
} from 'lucide-react';
import { api, ApiError } from '@/lib/api';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { formatBytes, formatPct, formatRate, cn, timeAgo } from '@/lib/utils';
import type { ArrayStatus, DiskInfo, ParityStatus, SmartInfo } from '@/types';
import { useSettingsStore } from '@/stores/settings';
import { ConfirmDialog } from '@/components/ui/alert-dialog';

const DISK_STATUS_VARIANT: Record<DiskInfo['status'], 'success' | 'warning' | 'destructive' | 'secondary'> = {
  ok: 'success',
  warning: 'warning',
  critical: 'destructive',
  unknown: 'secondary',
};

const DISK_STATUS_LABEL: Record<DiskInfo['status'], string> = {
  ok: '正常',
  warning: '警告',
  critical: '危险',
  unknown: '未知',
};

const ARRAY_STATE_LABEL: Record<string, string> = {
  started: '已启动',
  stopped: '已停止',
  starting: '启动中',
  stopping: '停止中',
};

// SMART self-test status → (badge variant, label, icon, text class).
const SMART_STATUS: Record<
  SmartInfo['status'],
  { variant: 'success' | 'warning' | 'destructive'; label: string; icon: typeof CheckCircle2 }
> = {
  ok: { variant: 'success', label: 'SMART 正常', icon: CheckCircle2 },
  warning: { variant: 'warning', label: 'SMART 警告', icon: AlertTriangle },
  failing: { variant: 'destructive', label: 'SMART 失败', icon: XCircle },
  unknown: { variant: 'success', label: '', icon: CheckCircle2 },
};

/** Status → left border color class for disk cards */
const STATUS_BORDER: Record<string, string> = {
  ok: 'border-l-emerald-500/60',
  warning: 'border-l-amber-500/60',
  critical: 'border-l-red-500/70',
  unknown: 'border-l-border',
};

/** Status → subtle background tint for disk cards */
const STATUS_BG: Record<string, string> = {
  ok: '',
  warning: 'bg-amber-500/5',
  critical: 'bg-red-500/5',
  unknown: '',
};

interface SmartRefreshResp {
  ok: boolean;
  cleared: string[];
  count: number;
  message?: string;
}

export default function StoragePage() {
  const refresh = useSettingsStore((s) => s.refreshInterval);
  const qc = useQueryClient();

  const [refreshMsg, setRefreshMsg] = useState<{ kind: 'ok' | 'err'; text: string } | null>(null);
  const [confirmStopArray, setConfirmStopArray] = useState(false);
  const [confirmParity, setConfirmParity] = useState(false);

  const { data, isLoading, isError } = useQuery({
    queryKey: ['storage'],
    queryFn: () => api.get<ArrayStatus>('/storage'),
    refetchInterval: refresh || false,
  });

  const refreshMut = useMutation({
    mutationFn: () => api.post<SmartRefreshResp>('/smart/refresh'),
    onSuccess: (r) => {
      setRefreshMsg({ kind: 'ok', text: r.message ?? `已刷新 ${r.count} 块` });
      qc.invalidateQueries({ queryKey: ['storage'] });
      window.setTimeout(() => setRefreshMsg(null), 3000);
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError ? e.message : '刷新失败';
      setRefreshMsg({ kind: 'err', text: msg });
      window.setTimeout(() => setRefreshMsg(null), 3000);
    },
  });

  const arrayMut = useMutation({
    mutationFn: (action: 'start' | 'stop') =>
      api.post<{ ok: boolean; message?: string; detail?: string }>(
        `/storage/array/${action}`,
      ),
    onSuccess: (r) => {
      setRefreshMsg({
        kind: r.ok ? 'ok' : 'err',
        text: r.ok ? (r.message ?? '操作成功') : (r.detail ?? r.message ?? '操作失败'),
      });
      qc.invalidateQueries({ queryKey: ['storage'] });
      window.setTimeout(() => setRefreshMsg(null), 4000);
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError ? e.message : '阵列操作失败';
      setRefreshMsg({ kind: 'err', text: msg });
      window.setTimeout(() => setRefreshMsg(null), 4000);
    },
  });

  const parityMut = useMutation({
    mutationFn: (action: 'start' | 'stop') =>
      api.post<{ ok: boolean; message?: string; detail?: string }>(
        `/storage/parity/${action}`,
      ),
    onSuccess: (r) => {
      setRefreshMsg({
        kind: r.ok ? 'ok' : 'err',
        text: r.ok ? (r.message ?? '操作成功') : (r.detail ?? r.message ?? '操作失败'),
      });
      qc.invalidateQueries({ queryKey: ['parity'] });
      window.setTimeout(() => setRefreshMsg(null), 4000);
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError ? e.message : 'Parity 操作失败';
      setRefreshMsg({ kind: 'err', text: msg });
      window.setTimeout(() => setRefreshMsg(null), 4000);
    },
  });

  const { data: parity } = useQuery({
    queryKey: ['parity'],
    queryFn: () => api.get<ParityStatus>('/storage/parity-status'),
    refetchInterval: 3000,
  });

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 p-6 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" /> 加载阵列信息…
      </div>
    );
  }
  if (isError || !data) {
    return (
      <Card className="m-6">
        <CardContent className="py-12 text-center text-sm text-muted-foreground">
          无法获取磁盘信息。请确认后端已连接到 Unraid。
        </CardContent>
      </Card>
    );
  }

  const totalDisks = data.disks.length + data.cacheDisks.length;
  const healthyDisks = [...data.disks, ...data.cacheDisks].filter(d => d.status === 'ok').length;

  return (
    <div className="space-y-4 p-4 md:p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className={cn(
            'flex h-10 w-10 items-center justify-center rounded-lg',
            data.state === 'started' ? 'bg-success/15 text-success' : 'bg-muted text-muted-foreground',
          )}>
            <HardDrive className="h-5 w-5" />
          </div>
          <div>
            <h1 className="text-xl font-semibold">存储阵列</h1>
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Badge
                variant={data.state === 'started' ? 'success' : 'secondary'}
                className="text-[10px]"
              >
                {ARRAY_STATE_LABEL[data.state] ?? data.state}
              </Badge>
              <span>{healthyDisks}/{totalDisks} 块硬盘健康</span>
            </div>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {data.state === 'started' ? (
            <Button
              size="sm"
              variant="destructive"
              disabled={arrayMut.isPending}
              onClick={() => setConfirmStopArray(true)}
            >
              <Square className="h-3.5 w-3.5" /> 停止阵列
            </Button>
          ) : (
            <Button
              size="sm"
              variant="success"
              disabled={arrayMut.isPending}
              onClick={() => arrayMut.mutate('start')}
            >
              {arrayMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
              启动阵列
            </Button>
          )}
          {refreshMsg && (
            <span
              className={cn(
                'text-xs',
                refreshMsg.kind === 'ok' ? 'text-muted-foreground' : 'text-destructive',
              )}
            >
              {refreshMsg.text}
            </span>
          )}
          <Button
            variant="outline"
            size="sm"
            disabled={refreshMut.isPending}
            onClick={() => refreshMut.mutate()}
          >
            <RefreshCw className={cn('h-3.5 w-3.5', refreshMut.isPending && 'animate-spin')} />
            刷新 SMART
          </Button>
        </div>
      </div>

      {/* Parity check progress + controls */}
      {parity && parity.state === 'checking' && (
        <Card className="border-primary/30">
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center justify-between text-base">
              <span className="flex items-center gap-2">
                <Activity className="h-4 w-4 animate-pulse text-primary" />
                Parity 检查进行中
              </span>
              <div className="flex items-center gap-2">
                {parity.errors > 0 && (
                  <Badge variant="destructive" className="text-[10px]">
                    <AlertTriangle className="mr-1 h-3 w-3" />
                    {parity.errors} 错误
                  </Badge>
                )}
                <Button
                  size="sm"
                  variant="outline"
                  disabled={parityMut.isPending}
                  onClick={() => parityMut.mutate('stop')}
                >
                  <Square className="h-3.5 w-3.5" /> 停止检查
                </Button>
              </div>
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <div className="flex items-center gap-3">
              <Progress className="h-2.5 flex-1" value={parity.progress} indicatorClassName="bg-primary" />
              <span className="shrink-0 text-sm font-semibold tabular-nums">
                {formatPct(parity.progress)}
              </span>
            </div>
            <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
              <span>速度：{parity.speed}</span>
              <span>剩余：{parity.remaining}</span>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Parity check idle */}
      {parity && parity.state === 'idle' && (
        <div className="flex items-center justify-between rounded-lg border border-dashed p-3">
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <ShieldCheck className="h-4 w-4 text-muted-foreground" />
            Parity 检查未运行
          </div>
          <Button
            size="sm"
            variant="outline"
            disabled={parityMut.isPending}
            onClick={() => setConfirmParity(true)}
          >
            <Play className="h-3.5 w-3.5" /> 开始 Parity 检查
          </Button>
        </div>
      )}

      <DiskGroup title="阵列磁盘" disks={data.disks} />
      <DiskGroup title="缓存池" disks={data.cacheDisks} />

      <ConfirmDialog
        open={confirmStopArray}
        title="确认停止阵列"
        description="所有磁盘将被卸载。确认要停止阵列吗？"
        confirmText="停止阵列"
        variant="destructive"
        loading={arrayMut.isPending}
        onConfirm={() => {
          arrayMut.mutate('stop');
          setConfirmStopArray(false);
        }}
        onCancel={() => setConfirmStopArray(false)}
      />
      <ConfirmDialog
        open={confirmParity}
        title="启动 Parity 检查"
        description="这将读取所有阵列磁盘，可能需要数小时。确认启动？"
        confirmText="开始检查"
        loading={parityMut.isPending}
        onConfirm={() => {
          parityMut.mutate('start');
          setConfirmParity(false);
        }}
        onCancel={() => setConfirmParity(false)}
      />
    </div>
  );
}

/* ------------------------------ Disk Group ------------------------------- */

function DiskGroup({ title, disks }: { title: string; disks: DiskInfo[] }) {
  if (disks.length === 0) return null;
  const total = disks.reduce((s, d) => s + d.sizeBytes, 0);
  const used = disks.reduce((s, d) => s + d.usedBytes, 0);
  const usedPct = total > 0 ? (used / total) * 100 : 0;

  const groupIndicatorColor =
    usedPct > 90 ? 'bg-destructive' : usedPct > 75 ? 'bg-warning' : 'bg-success';

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center justify-between text-base">
          <span className="flex items-center gap-2">
            <HardDrive className="h-4 w-4 text-muted-foreground" />
            {title}
            <span className="text-xs font-normal text-muted-foreground">
              {disks.length} 块
            </span>
          </span>
          <span className="text-xs font-normal tabular-nums text-muted-foreground">
            {formatBytes(used)} / {formatBytes(total)}
          </span>
        </CardTitle>
        {/* Group-level mini progress bar */}
        <div className="mt-2 flex items-center gap-2">
          <Progress
            className="h-1.5"
            value={usedPct}
            indicatorClassName={groupIndicatorColor}
          />
          <span className="shrink-0 text-xs font-medium tabular-nums text-muted-foreground">
            {formatPct(usedPct)}
          </span>
        </div>
      </CardHeader>
      <CardContent className="space-y-2.5">
        {disks.map((d) => (
          <DiskRow key={d.device} disk={d} />
        ))}
      </CardContent>
    </Card>
  );
}

/* ------------------------------ Disk Row --------------------------------- */

function DiskRow({ disk: d }: { disk: DiskInfo }) {
  const pct = d.sizeBytes > 0 ? (d.usedBytes / d.sizeBytes) * 100 : 0;

  // Unraid LED color mapping
  const unraidColorMap: Record<string, string> = {
    'green-on': 'bg-emerald-500',
    'green-off': 'bg-emerald-300 dark:bg-emerald-800',
    'yellow-on': 'bg-amber-500',
    'yellow-off': 'bg-amber-300 dark:bg-amber-800',
    'red-on': 'bg-red-500',
    'red-off': 'bg-red-300 dark:bg-red-800',
    'grey-off': 'bg-gray-400 dark:bg-gray-600',
  };
  const ledDot = d.color ? unraidColorMap[d.color] : '';
  const isSsd = d.rotational === '0';
  const TransportIcon = d.transport === 'nvme' ? Zap : d.transport === 'usb' ? Usb : Cable;

  // Temperature color logic
  const tempHigh = d.tempC !== undefined && d.tempC >= 50;
  const tempCritical = d.tempC !== undefined && d.tempC >= 65;
  const tempColor = tempCritical
    ? 'text-destructive'
    : tempHigh
      ? 'text-warning'
      : 'text-muted-foreground';
  const tempBg = tempCritical
    ? 'bg-destructive/10 border-destructive/30'
    : tempHigh
      ? 'bg-warning/10 border-warning/30'
      : 'bg-transparent border-transparent';

  // Progress bar color
  const progressColor =
    pct > 90 ? 'bg-destructive' : pct > 75 ? 'bg-warning' : 'bg-primary';

  const isProblemDisk = d.status === 'warning' || d.status === 'critical';

  return (
    <div
      className={cn(
        'rounded-lg border border-l-2 p-3 transition-colors hover:bg-muted/30',
        STATUS_BORDER[d.status] ?? 'border-l-border',
        STATUS_BG[d.status],
      )}
    >
      {/* Row 1: Identity (left) + Badges (right) */}
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          {ledDot && (
            <span
              className={cn(
                'inline-block h-2.5 w-2.5 shrink-0 rounded-full ring-2 ring-background',
                ledDot,
                d.color?.endsWith('-on') && 'shadow-sm',
              )}
              title={`Unraid: ${d.color}`}
            />
          )}
          <div className="min-w-0">
            <div className="flex items-center gap-1.5">
              <span className="truncate text-sm font-medium">
                {d.diskName || d.name || d.device}
              </span>
              {d.diskName && d.name && d.name !== d.diskName && (
                <span className="truncate text-xs text-muted-foreground">
                  {d.name}
                </span>
              )}
              {isSsd && (
                <Badge variant="secondary" className="px-1 py-0 text-[9px] leading-none">
                  SSD
                </Badge>
              )}
            </div>
            <div className="mt-0.5 flex items-center gap-1 text-[10px] font-mono text-muted-foreground/70">
              {d.transport && <TransportIcon className="h-2.5 w-2.5 shrink-0" />}
              <span>{d.device}</span>
              <span className="text-muted-foreground/40">·</span>
              <span>{d.fsType}</span>
            </div>
          </div>
        </div>

        <div className="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
          {/* Temperature pill */}
          {d.tempC !== undefined && (
            <span
              className={cn(
                'inline-flex items-center gap-0.5 rounded-md border px-1.5 py-0.5 text-[10px] font-medium tabular-nums',
                tempBg,
                tempColor,
              )}
            >
              <Thermometer className="h-2.5 w-2.5" />
              {d.tempC}°
            </span>
          )}
          {/* Error badge */}
          {d.errors > 0 && (
            <Badge variant="destructive" className="px-1.5 py-0 text-[9px] leading-none">
              <AlertTriangle className="mr-0.5 h-2.5 w-2.5" />
              {d.errors}
            </Badge>
          )}
          {/* Status badge */}
          <Badge
            variant={DISK_STATUS_VARIANT[d.status]}
            className="px-1.5 py-0 text-[9px] leading-none"
          >
            {DISK_STATUS_LABEL[d.status] ?? d.status}
          </Badge>
          {/* SMART badge */}
          {d.smart?.available && d.smart.status !== 'unknown' && (
            <Badge
              variant={SMART_STATUS[d.smart.status].variant}
              className="px-1.5 py-0 text-[9px] leading-none"
            >
              <SmartStatusIcon status={d.smart.status} />
              {SMART_STATUS[d.smart.status].label}
            </Badge>
          )}
        </div>
      </div>

      {/* Row 2: Usage progress bar */}
      <div className="mt-2.5 flex items-center gap-2.5">
        <Progress
          className="h-2 flex-1"
          value={pct}
          indicatorClassName={progressColor}
        />
        <div className="flex shrink-0 items-baseline gap-1 text-xs tabular-nums">
          <span className="font-medium">{formatPct(pct)}</span>
          <span className="text-muted-foreground">
            {formatBytes(d.usedBytes)}/{formatBytes(d.sizeBytes)}
          </span>
        </div>
      </div>

      {/* Row 3: Read/Write rate pills */}
      <div className="mt-1.5 flex items-center gap-1.5">
        <span className="inline-flex items-center gap-1 rounded bg-emerald-500/10 px-1.5 py-0.5 text-[10px] font-mono tabular-nums text-emerald-600 dark:text-emerald-400">
          ↓ {formatRate(d.readBytesPerSec)}
        </span>
        <span className="inline-flex items-center gap-1 rounded bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-mono tabular-nums text-amber-600 dark:text-amber-400">
          ↑ {formatRate(d.writeBytesPerSec)}
        </span>
      </div>

      {/* SMART detail */}
      <SmartDetail smart={d.smart} expanded={isProblemDisk} />
    </div>
  );
}

/* ------------------------------- SMART bits ------------------------------ */

function SmartStatusIcon({ status }: { status: SmartInfo['status'] }) {
  const Icon = SMART_STATUS[status].icon;
  return <Icon className="mr-0.5 h-2.5 w-2.5" />;
}

/**
 * SMART detail row. Collapsed by default for healthy disks (click to expand),
 * auto-expanded for warning/failing disks.
 */
function SmartDetail({ smart, expanded = false }: { smart?: SmartInfo; expanded?: boolean }) {
  const [open, setOpen] = useState(expanded);

  if (!smart || !smart.available || smart.status === 'unknown') return null;

  const counters: { label: string; value: number }[] = [
    { label: '重映射', value: smart.reallocated },
    { label: '待处理', value: smart.pending },
    { label: '离线不可纠正', value: smart.uncorrectable },
    { label: '介质错误', value: smart.mediaErrors },
  ].filter((c) => c.value > 0);

  const isFailing = smart.status === 'failing';
  const isWarning = smart.status === 'warning';
  const hasProblem = isFailing || isWarning;

  // For healthy disks, show a compact one-liner with expand toggle.
  // For problem disks, show full detail (auto-expanded).
  return (
    <div
      className={cn(
        'mt-2 rounded-md border-t pt-1.5 text-[10px]',
        isFailing && 'border-destructive/30 bg-destructive/5 px-2 py-1.5',
        isWarning && 'border-warning/30 bg-warning/5 px-2 py-1.5',
        !hasProblem && 'border-border/50',
      )}
    >
      {/* Compact header row — always visible */}
      <div
        className={cn(
          'flex items-center justify-between gap-2 text-muted-foreground',
          !hasProblem && 'cursor-pointer hover:text-foreground',
        )}
        onClick={!hasProblem ? () => setOpen(o => !o) : undefined}
      >
        <span className="flex items-center gap-1.5 truncate font-mono">
          {!hasProblem && (
            <CheckCircle2 className="h-3 w-3 shrink-0 text-emerald-500" />
          )}
          <span className="truncate">
            {smart.modelName || 'SMART'}
            {smart.serialNumber && (
              <span className="text-muted-foreground/50"> · {smart.serialNumber}</span>
            )}
          </span>
        </span>
        <span className="flex shrink-0 items-center gap-1.5">
          <span className="text-muted-foreground/60">刷新于 {timeAgo(smart.fetchedAt)}</span>
          {!hasProblem && (
            <ChevronDown className={cn('h-3 w-3 transition-transform', open && 'rotate-180')} />
          )}
        </span>
      </div>

      {/* Expanded detail for healthy disks */}
      {!hasProblem && open && (
        <div className="mt-1 text-emerald-600 dark:text-emerald-500">
          自检通过 · 无可靠性告警
        </div>
      )}

      {/* Problem disk detail (always shown, no collapse) */}
      {hasProblem && (
        <>
          {isFailing && (
            <div className="mt-1 font-medium text-destructive">
              自检未通过 — 该硬盘可能即将故障，请尽快备份数据！
            </div>
          )}
          {counters.length > 0 && (
            <div
              className={cn(
                'mt-1 flex flex-wrap gap-x-3 gap-y-0.5 font-medium',
                isFailing ? 'text-destructive' : 'text-warning',
              )}
            >
              {counters.map((c) => (
                <span key={c.label}>
                  {c.label} {c.value}
                </span>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}

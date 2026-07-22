import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
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
// We surface SMART whenever smart.available is true, even on 'ok', so the
// user knows SMART monitoring is active on that disk. 'unknown' is hidden
// entirely (we have no data to show).
const SMART_STATUS: Record<
  SmartInfo['status'],
  { variant: 'success' | 'warning' | 'destructive'; label: string; icon: typeof CheckCircle2 }
> = {
  ok: { variant: 'success', label: 'SMART 正常', icon: CheckCircle2 },
  warning: { variant: 'warning', label: 'SMART 警告', icon: AlertTriangle },
  failing: { variant: 'destructive', label: 'SMART 失败', icon: XCircle },
  unknown: { variant: 'success', label: '', icon: CheckCircle2 }, // unused — see guard below
};

/** Shape of the POST /api/smart/refresh response (see smart_refresh.go). */
interface SmartRefreshResp {
  ok: boolean;
  cleared: string[];
  count: number;
  message?: string;
}

export default function StoragePage() {
  const refresh = useSettingsStore((s) => s.refreshInterval);
  const qc = useQueryClient();

  // Transient status line shown next to the refresh button after a refresh
  // attempt ("已刷新全部 N 块" / "刷新失败"). Cleared after 3s. Null = idle.
  const [refreshMsg, setRefreshMsg] = useState<{ kind: 'ok' | 'err'; text: string } | null>(null);
  const [confirmStopArray, setConfirmStopArray] = useState(false);
  const [confirmParity, setConfirmParity] = useState(false);

  const { data, isLoading, isError } = useQuery({
    queryKey: ['storage'],
    queryFn: () => api.get<ArrayStatus>('/storage'),
    refetchInterval: refresh || false,
  });

  // Refresh SMART: POST /api/smart/refresh (empty body = invalidate all),
  // then invalidate the ['storage'] query to trigger a refetch. The refetch
  // is what causes fresh smartctl probes (cache was just cleared), so the
  // user sees updated data within ~1-2s per disk.
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

  // Array start/stop (mdcmd start / mdcmd stop).
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

  // Parity check start/stop (mdcmd check / mdcmd nocheck).
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

  // Poll parity status. This is cheap (just reads /proc/mdstat) so we
  // poll every 3s regardless of whether a check is running — that way
  // the UI updates immediately when a check starts or finishes.
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

  return (
    <div className="space-y-4 p-4 md:p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">存储阵列</h1>
          <p className="text-sm text-muted-foreground">
            状态：
            <Badge
              variant={data.state === 'started' ? 'success' : 'secondary'}
              className="ml-1"
            >
              {ARRAY_STATE_LABEL[data.state] ?? data.state}
            </Badge>
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {/* Array start/stop */}
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
              <Play className="h-3.5 w-3.5" /> 启动阵列
            </Button>
          )}
          {/* Show transient result inline */}
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
            <RefreshCw className={cn(refreshMut.isPending && 'animate-spin')} />
            刷新 SMART
          </Button>
        </div>
      </div>

      {/* Parity check progress + controls */}
      {parity && parity.state === 'checking' && (
        <Card>
          <CardHeader>
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
              <Progress className="flex-1" value={parity.progress} />
              <span className="shrink-0 text-sm font-medium tabular-nums">
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

      {/* Parity check idle — show start button */}
      {parity && parity.state === 'idle' && (
        <div className="flex items-center justify-between rounded-lg border p-3">
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

function DiskGroup({ title, disks }: { title: string; disks: DiskInfo[] }) {
  if (disks.length === 0) return null;
  const total = disks.reduce((s, d) => s + d.sizeBytes, 0);
  const used = disks.reduce((s, d) => s + d.usedBytes, 0);
  const usedPct = total > 0 ? (used / total) * 100 : 0;
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center justify-between text-base">
          <span className="flex items-center gap-2">
            <HardDrive className="h-4 w-4" />
            {title}
          </span>
          <span className="text-xs font-normal text-muted-foreground tabular-nums">
            {formatBytes(used)} / {formatBytes(total)} ({formatPct(usedPct)})
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {disks.map((d) => {
          const pct = d.sizeBytes > 0 ? (d.usedBytes / d.sizeBytes) * 100 : 0;
          // Unraid LED color dot
          const unraidColorMap: Record<string, string> = {
            'green-on': 'bg-emerald-500',
            'green-off': 'bg-emerald-300 dark:bg-emerald-800',
            'yellow-on': 'bg-yellow-500',
            'yellow-off': 'bg-yellow-300 dark:bg-yellow-800',
            'red-on': 'bg-red-500',
            'red-off': 'bg-red-300 dark:bg-red-800',
            'grey-off': 'bg-gray-400 dark:bg-gray-600',
          };
          const ledDot = d.color ? unraidColorMap[d.color] : '';
          const isSsd = d.rotational === '0';
          const TransportIcon = d.transport === 'nvme' ? Zap : d.transport === 'usb' ? Usb : Cable;
          return (
            <div key={d.device} className="rounded-md border p-3">
              <div className="flex items-center justify-between gap-2">
                <div className="min-w-0">
                  <div className="flex items-center gap-1.5 text-sm font-medium">
                    {ledDot && (
                      <span
                        className={cn('inline-block h-2 w-2 shrink-0 rounded-full', ledDot)}
                        title={`Unraid: ${d.color}`}
                      />
                    )}
                    <span className="truncate">{d.diskName || d.name || d.device}</span>
                    {d.diskName && d.name && d.name !== d.diskName && (
                      <span className="truncate font-normal text-muted-foreground">
                        {d.name}
                      </span>
                    )}
                    {isSsd && (
                      <Badge variant="secondary" className="text-[9px] px-1 py-0 leading-none">
                        SSD
                      </Badge>
                    )}
                  </div>
                  <div className="flex items-center gap-1 truncate font-mono text-[10px] text-muted-foreground">
                    {d.transport && <TransportIcon className="h-3 w-3 shrink-0" />}
                    {d.device} · {d.fsType}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  {d.errors > 0 && (
                    <Badge variant="destructive" className="text-[10px]">
                      <AlertTriangle className="mr-1 h-3 w-3" />
                      {d.errors} errors
                    </Badge>
                  )}
                  <Badge variant={DISK_STATUS_VARIANT[d.status]} className="text-[10px]">
                    {DISK_STATUS_LABEL[d.status] ?? d.status}
                  </Badge>
                  {d.tempC !== undefined && (
                    <Badge
                      variant="outline"
                      className={cn(
                        'text-[10px]',
                        d.tempC >= 50 && 'border-warning text-warning',
                        d.tempC >= 65 && 'border-destructive text-destructive',
                      )}
                    >
                      <Thermometer className="mr-1 h-3 w-3" />
                      {d.tempC}°C
                    </Badge>
                  )}
                  {d.smart?.available && d.smart.status !== 'unknown' && (
                    <Badge
                      variant={SMART_STATUS[d.smart.status].variant}
                      className="text-[10px]"
                    >
                      <SmartStatusIcon status={d.smart.status} />
                      {SMART_STATUS[d.smart.status].label}
                    </Badge>
                  )}
                </div>
              </div>
              <div className="mt-2 flex items-center gap-3">
                <Progress
                  className="flex-1"
                  value={pct}
                  indicatorClassName={
                    pct > 90
                      ? 'bg-destructive'
                      : pct > 75
                        ? 'bg-warning'
                        : 'bg-primary'
                  }
                />
                <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                  {formatPct(pct)} · {formatBytes(d.usedBytes)}/
                  {formatBytes(d.sizeBytes)}
                </span>
              </div>
              <div className="mt-1 text-[10px] tabular-nums text-muted-foreground">
                读 {formatRate(d.readBytesPerSec)} · 写 {formatRate(d.writeBytesPerSec)}
              </div>
              <SmartDetail smart={d.smart} />
            </div>
          );
        })}
      </CardContent>
    </Card>
  );
}

/* ------------------------------- SMART bits ------------------------------ */

function SmartStatusIcon({ status }: { status: SmartInfo['status'] }) {
  const Icon = SMART_STATUS[status].icon;
  return <Icon className="mr-1 h-3 w-3" />;
}

/**
 * Renders the structured SMART detail row beneath each disk's RW rate line.
 *
 * Hidden entirely when:
 *   - smart is undefined (device doesn't support SMART, smartctl missing, or
 *     software raid / loop / zfs vdev — see baseDevName on the server)
 *   - smart.available is false (JSON parse error, device unsupported)
 *   - smart.status is 'unknown' (only happens transiently; once available
 *     becomes true status will be ok/warning/failing)
 *
 * Layout:
 *   ┌───────────────────────────────────────────────────────────┐
 *   │ SMART · Samsung 870 EVO · S5XXXXXXXX  · 刷新于 12s ago     │
 *   │ ⚠ 重映射 5 · 待处理 2 · 离线不可纠正 0                      │  (warning/failing only)
 *   └───────────────────────────────────────────────────────────┘
 *
 * Counters are listed individually (not summed) so the user can tell whether
 * the problem is reallocated sectors (attr 5 — physical damage) vs pending
 * (attr 197 — possibly recoverable) vs uncorrectable (attr 198 — confirmed
 * unreadable) vs NVMe media errors (different failure mode entirely).
 */
function SmartDetail({ smart }: { smart?: SmartInfo }) {
  if (!smart || !smart.available || smart.status === 'unknown') return null;

  const counters: { label: string; value: number }[] = [
    { label: '重映射', value: smart.reallocated },
    { label: '待处理', value: smart.pending },
    { label: '离线不可纠正', value: smart.uncorrectable },
    { label: '介质错误', value: smart.mediaErrors },
  ].filter((c) => c.value > 0);

  const isFailing = smart.status === 'failing';
  const isWarning = smart.status === 'warning';

  return (
    <div
      className={cn(
        'mt-2 rounded border-t pt-2 text-[10px]',
        isFailing && 'border-destructive/40 bg-destructive/5',
        isWarning && 'border-warning/40 bg-warning/5',
        !isFailing && !isWarning && 'border-border',
      )}
    >
      <div className="flex items-center justify-between gap-2 text-muted-foreground">
        <span className="truncate font-mono">
          SMART
          {smart.modelName && ` · ${smart.modelName}`}
          {smart.serialNumber && ` · ${smart.serialNumber}`}
        </span>
        <span className="shrink-0">刷新于 {timeAgo(smart.fetchedAt)}</span>
      </div>
      {isFailing && (
        <div className="mt-1 font-medium text-destructive">
          自检未通过 — 该硬盘可能即将故障，请尽快备份数据！
        </div>
      )}
      {(isFailing || isWarning) && counters.length > 0 && (
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
      {!isFailing && !isWarning && (
        <div className="mt-0.5 text-emerald-600 dark:text-emerald-500">
          自检通过 · 无可靠性告警
        </div>
      )}
    </div>
  );
}

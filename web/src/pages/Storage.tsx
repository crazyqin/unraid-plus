import { useQuery } from '@tanstack/react-query';
import {
  AlertTriangle,
  CheckCircle2,
  HardDrive,
  Loader2,
  Thermometer,
  XCircle,
} from 'lucide-react';
import { api } from '@/lib/api';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { Badge } from '@/components/ui/badge';
import { formatBytes, formatPct, formatRate, cn, timeAgo } from '@/lib/utils';
import type { ArrayStatus, DiskInfo, SmartInfo } from '@/types';
import { useSettingsStore } from '@/stores/settings';

const DISK_STATUS_VARIANT: Record<DiskInfo['status'], 'success' | 'warning' | 'destructive' | 'secondary'> = {
  ok: 'success',
  warning: 'warning',
  critical: 'destructive',
  unknown: 'secondary',
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

export default function StoragePage() {
  const refresh = useSettingsStore((s) => s.refreshInterval);
  const { data, isLoading, isError } = useQuery({
    queryKey: ['storage'],
    queryFn: () => api.get<ArrayStatus>('/storage'),
    refetchInterval: refresh || false,
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
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">存储阵列</h1>
          <p className="text-sm text-muted-foreground">
            状态：
            <Badge
              variant={data.state === 'started' ? 'success' : 'secondary'}
              className="ml-1"
            >
              {data.state}
            </Badge>
          </p>
        </div>
      </div>

      <DiskGroup title="阵列磁盘" disks={data.disks} />
      <DiskGroup title="缓存池" disks={data.cacheDisks} />
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
          return (
            <div key={d.device} className="rounded-md border p-3">
              <div className="flex items-center justify-between gap-2">
                <div className="min-w-0">
                  <div className="truncate text-sm font-medium">
                    {d.name || d.device}
                  </div>
                  <div className="truncate font-mono text-[10px] text-muted-foreground">
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
                    {d.status}
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

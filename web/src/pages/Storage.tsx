import { useQuery } from '@tanstack/react-query';
import { AlertTriangle, HardDrive, Loader2, Thermometer } from 'lucide-react';
import { api } from '@/lib/api';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { Badge } from '@/components/ui/badge';
import { formatBytes, formatPct, formatRate, cn } from '@/lib/utils';
import type { ArrayStatus, DiskInfo } from '@/types';
import { useSettingsStore } from '@/stores/settings';

const DISK_STATUS_VARIANT: Record<DiskInfo['status'], 'success' | 'warning' | 'destructive' | 'secondary'> = {
  ok: 'success',
  warning: 'warning',
  critical: 'destructive',
  unknown: 'secondary',
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
            </div>
          );
        })}
      </CardContent>
    </Card>
  );
}

import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Cpu,
  Loader2,
  MemoryStick,
  Pause,
  Play,
  Power,
  Square,
} from 'lucide-react';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { formatBytes } from '@/lib/utils';
import type { VmInfo } from '@/types';

const STATUS_VARIANT: Record<VmInfo['status'], 'success' | 'secondary' | 'warning'> = {
  running: 'success',
  shutoff: 'secondary',
  paused: 'warning',
  unknown: 'secondary',
};

const STATUS_LABEL: Record<string, string> = {
  running: '运行中',
  shutoff: '已关机',
  paused: '已暂停',
  unknown: '未知',
};

export default function VmsPage() {
  const qc = useQueryClient();
  const { data, isLoading, isError } = useQuery({
    queryKey: ['vms'],
    queryFn: () => api.get<VmInfo[]>('/vms'),
    refetchInterval: 10_000,
  });

  const act = async (id: string, action: 'start' | 'stop' | 'shutdown' | 'resume' | 'suspend') => {
    await api.post(`/vms/${id}/${action}`);
    qc.invalidateQueries({ queryKey: ['vms'] });
  };

  const running = (data ?? []).filter((v) => v.status === 'running').length;

  return (
    <div className="space-y-4 p-4 md:p-6">
      <div>
        <h1 className="text-xl font-semibold">虚拟机</h1>
        <p className="text-sm text-muted-foreground">
          {running} 个运行中 / {data?.length ?? 0} 个总计
        </p>
      </div>

      {isLoading ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" /> 加载虚拟机列表…
        </div>
      ) : isError ? (
        <Card>
          <CardContent className="py-12 text-center text-sm text-muted-foreground">
            无法获取虚拟机信息。请确认 Unraid 已启用 libvirt / libvirtd。
          </CardContent>
        </Card>
      ) : data!.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center gap-2 py-12 text-center text-sm text-muted-foreground">
            <Cpu className="h-8 w-8" />
            没有虚拟机。
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {data!.map((vm) => (
            <Card key={vm.id}>
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between gap-2">
                  <CardTitle className="truncate text-sm">{vm.name}</CardTitle>
                  <Badge variant={STATUS_VARIANT[vm.status]}>{STATUS_LABEL[vm.status] ?? vm.status}</Badge>
                </div>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="grid grid-cols-2 gap-2 text-xs">
                  <div className="rounded-md bg-muted/40 p-2">
                    <div className="flex items-center gap-1 text-muted-foreground">
                      <Cpu className="h-3 w-3" /> vCPU
                    </div>
                    <div className="mt-0.5 font-medium">{vm.vcpus}</div>
                  </div>
                  <div className="rounded-md bg-muted/40 p-2">
                    <div className="flex items-center gap-1 text-muted-foreground">
                      <MemoryStick className="h-3 w-3" /> 内存
                    </div>
                    <div className="mt-0.5 font-medium">
                      {formatBytes(vm.memoryBytes)}
                    </div>
                  </div>
                </div>
                <div className="flex flex-wrap gap-2">
                  {vm.status !== 'running' && vm.status !== 'paused' && (
                    <Button size="sm" variant="success" onClick={() => act(vm.id, 'start')}>
                      <Play className="h-3.5 w-3.5" /> 启动
                    </Button>
                  )}
                  {vm.status === 'running' && (
                    <>
                      <Button size="sm" variant="outline" onClick={() => act(vm.id, 'shutdown')}>
                        <Power className="h-3.5 w-3.5" /> 安全关机
                      </Button>
                      <Button size="sm" variant="destructive" onClick={() => act(vm.id, 'stop')}>
                        <Square className="h-3.5 w-3.5" /> 强制停止
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => act(vm.id, 'suspend')}>
                        <Pause className="h-3.5 w-3.5" /> 暂停
                      </Button>
                    </>
                  )}
                  {vm.status === 'paused' && (
                    <Button size="sm" variant="success" onClick={() => act(vm.id, 'resume')}>
                      <Play className="h-3.5 w-3.5" /> 恢复
                    </Button>
                  )}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}

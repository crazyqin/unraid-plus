import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Cpu,
  Loader2,
  MemoryStick,
  Monitor,
  Pause,
  Play,
  Power,
  Square,
} from 'lucide-react';
import { api, ApiError, wsUrl } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
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
  const [vncVm, setVncVm] = useState<VmInfo | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const { data, isLoading, isError } = useQuery({
    queryKey: ['vms'],
    queryFn: () => api.get<VmInfo[]>('/vms'),
    refetchInterval: 10_000,
  });

  const act = async (id: string, action: 'start' | 'stop' | 'shutdown' | 'resume' | 'suspend') => {
    setActionError(null);
    try {
      await api.post(`/vms/${id}/${action}`);
      qc.invalidateQueries({ queryKey: ['vms'] });
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '操作失败');
    }
  };

  const running = (data ?? []).filter((v) => v.status === 'running').length;

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
                  {vm.status === 'running' && (
                    <Button size="sm" variant="outline" onClick={() => setVncVm(vm)}>
                      <Monitor className="h-3.5 w-3.5" /> 控制台
                    </Button>
                  )}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <VNCDialog vm={vncVm} onClose={() => setVncVm(null)} />
    </div>
  );
}

/* ------------------------------- VNC Dialog ------------------------------- */

function VNCDialog({ vm, onClose }: { vm: VmInfo | null; onClose: () => void }) {
  if (!vm) return null;

  // Build the WebSocket URL for the VNC proxy endpoint.
  // The noVNC viewer (iframe) will connect to this URL.
  const vncWsUrl = wsUrl(`/ws/vnc?vm=${encodeURIComponent(vm.id)}`);

  // The noVNC lite viewer is served from /vnc/vnc_lite.html (public dir).
  // We pass the WebSocket URL via the "url" query parameter.
  const iframeSrc = `/vnc/vnc_lite.html?url=${encodeURIComponent(vncWsUrl)}&scale=true`;

  return (
    <Dialog open={!!vm} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-4xl p-0 gap-0 overflow-hidden">
        <DialogHeader className="px-4 pt-4 pb-2">
          <DialogTitle className="flex items-center gap-2">
            <Monitor className="h-4 w-4" />
            VNC 控制台 · {vm.name}
          </DialogTitle>
        </DialogHeader>
        <div className="px-4 pb-1 text-xs text-muted-foreground">
          通过 SSH 隧道连接到虚拟机的 VNC 服务。如无画面，请确认虚拟机已配置 VNC 显卡。
        </div>
        <div className="relative" style={{ height: '70vh' }}>
          <iframe
            src={iframeSrc}
            className="absolute inset-0 h-full w-full border-0"
            title={`VNC - ${vm.name}`}
            sandbox="allow-scripts allow-same-origin"
          />
        </div>
      </DialogContent>
    </Dialog>
  );
}

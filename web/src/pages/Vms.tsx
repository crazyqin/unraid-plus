import { useEffect, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Cpu,
  Loader2,
  MemoryStick,
  Monitor,
  Pause,
  Play,
  Power,
  Search,
  Square,
} from 'lucide-react';
import { api, ApiError, wsUrl } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { ConfirmDialog } from '@/components/ui/alert-dialog';
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
import { Input } from '@/components/ui/input';
import { cn, formatBytes } from '@/lib/utils';
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

/** Status → left border color class for VM cards */
const STATUS_BORDER: Record<string, string> = {
  running: 'border-l-emerald-500/60',
  shutoff: 'border-l-border',
  paused: 'border-l-amber-500/60',
  unknown: 'border-l-border',
};

/** Status → subtle background tint for VM cards */
const STATUS_BG: Record<string, string> = {
  running: '',
  shutoff: '',
  paused: 'bg-amber-500/5',
  unknown: '',
};

export default function VmsPage() {
  const qc = useQueryClient();
  const [vncVm, setVncVm] = useState<VmInfo | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [pendingAction, setPendingAction] = useState<string | null>(null);
  const [confirmAction, setConfirmAction] = useState<{ id: string; action: 'stop'; name: string } | null>(null);
  const [search, setSearch] = useState('');
  const { data, isLoading, isError } = useQuery({
    queryKey: ['vms'],
    queryFn: () => api.get<VmInfo[]>('/vms'),
    refetchInterval: 10_000,
  });

  const act = async (id: string, action: 'start' | 'stop' | 'shutdown' | 'resume' | 'suspend') => {
    // Force stop requires confirmation
    if (action === 'stop' && !confirmAction) {
      const vm = (data ?? []).find((v) => v.id === id);
      setConfirmAction({ id, action, name: vm?.name ?? id });
      return;
    }
    const key = `${id}:${action}`;
    setPendingAction(key);
    setActionError(null);
    setConfirmAction(null);
    try {
      await api.post(`/vms/${id}/${action}`);
      qc.invalidateQueries({ queryKey: ['vms'] });
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '操作失败');
    } finally {
      setPendingAction(null);
    }
  };

  const running = (data ?? []).filter((v) => v.status === 'running').length;
  const filtered = (data ?? []).filter((v) =>
    v.name.toLowerCase().includes(search.toLowerCase()),
  );

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
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className={cn(
            'flex h-10 w-10 items-center justify-center rounded-lg',
            running > 0 ? 'bg-success/15 text-success' : 'bg-muted text-muted-foreground',
          )}>
            <Monitor className="h-5 w-5" />
          </div>
          <div>
            <h1 className="text-xl font-semibold">虚拟机</h1>
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Badge
                variant={running > 0 ? 'success' : 'secondary'}
                className="text-[10px]"
              >
                {running > 0 ? '运行中' : '空闲'}
              </Badge>
              <span>{running} / {data?.length ?? 0} 台虚拟机</span>
            </div>
          </div>
        </div>
        {(data ?? []).length > 0 && (
          <div className="relative w-48 shrink-0">
            <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              placeholder="搜索虚拟机…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="h-8 pl-8 text-sm"
            />
          </div>
        )}
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
      ) : (data ?? []).length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center gap-2 py-12 text-center text-sm text-muted-foreground">
            <Cpu className="h-8 w-8" />
            没有虚拟机。
          </CardContent>
        </Card>
      ) : filtered.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center gap-2 py-12 text-center text-sm text-muted-foreground">
            <Cpu className="h-8 w-8" />
            没有匹配的虚拟机。
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {filtered.map((vm) => (
            <Card key={vm.id} className={cn(
              'border-l-2 transition-colors hover:bg-muted/30',
              STATUS_BORDER[vm.status] ?? 'border-l-border',
              STATUS_BG[vm.status],
            )}>
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between gap-2">
                  <CardTitle className="truncate text-sm">{vm.name}</CardTitle>
                  <Badge variant={STATUS_VARIANT[vm.status]} className="text-[9px] px-1.5 py-0 leading-none">{STATUS_LABEL[vm.status] ?? vm.status}</Badge>
                </div>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="flex items-center gap-1.5">
                  <span className="inline-flex items-center gap-1 rounded bg-blue-500/10 px-1.5 py-0.5 text-[10px] font-mono tabular-nums text-blue-600 dark:text-blue-400">
                    <Cpu className="h-2.5 w-2.5" />
                    {vm.vcpus} vCPU
                  </span>
                  <span className="inline-flex items-center gap-1 rounded bg-violet-500/10 px-1.5 py-0.5 text-[10px] font-mono tabular-nums text-violet-600 dark:text-violet-400">
                    <MemoryStick className="h-2.5 w-2.5" />
                    {formatBytes(vm.memoryBytes)}
                  </span>
                </div>
                <div className="flex flex-wrap gap-2">
                  {vm.status !== 'running' && vm.status !== 'paused' && (
                    <Button size="sm" variant="success" onClick={() => act(vm.id, 'start')} disabled={pendingAction !== null}>
                      {pendingAction === `${vm.id}:start` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />} 启动
                    </Button>
                  )}
                  {vm.status === 'running' && (
                    <>
                      <Button size="sm" variant="outline" onClick={() => act(vm.id, 'shutdown')} disabled={pendingAction !== null}>
                        {pendingAction === `${vm.id}:shutdown` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Power className="h-3.5 w-3.5" />} 安全关机
                      </Button>
                      <Button size="sm" variant="destructive" onClick={() => act(vm.id, 'stop')} disabled={pendingAction !== null}>
                        {pendingAction === `${vm.id}:stop` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Square className="h-3.5 w-3.5" />} 强制停止
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => act(vm.id, 'suspend')} disabled={pendingAction !== null}>
                        <Pause className="h-3.5 w-3.5" /> 暂停
                      </Button>
                    </>
                  )}
                  {vm.status === 'paused' && (
                    <Button size="sm" variant="success" onClick={() => act(vm.id, 'resume')} disabled={pendingAction !== null}>
                      <Play className="h-3.5 w-3.5" /> 恢复
                    </Button>
                  )}
                  {vm.status === 'running' && (
                    <Button size="sm" variant="outline" onClick={() => setVncVm(vm)} disabled={pendingAction !== null}>
                      <Monitor className="h-3.5 w-3.5" /> 控制台
                    </Button>
                  )}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <ConfirmDialog
        open={!!confirmAction}
        title="确认强制停止虚拟机"
        description={`强制停止虚拟机 "${confirmAction?.name}" 可能导致数据丢失，建议优先使用"安全关机"。确定要强制停止吗？`}
        confirmText="强制停止"
        variant="destructive"
        onConfirm={() => confirmAction && act(confirmAction.id, confirmAction.action)}
        onCancel={() => setConfirmAction(null)}
      />

      <VNCDialog vm={vncVm} onClose={() => setVncVm(null)} />
    </div>
  );
}

/* ------------------------------- VNC Dialog ------------------------------- */

function VNCDialog({ vm, onClose }: { vm: VmInfo | null; onClose: () => void }) {
  const [vncLoading, setVncLoading] = useState(true);

  // Reset loading state when vm changes
  useEffect(() => {
    if (vm) setVncLoading(true);
  }, [vm]);

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
          {vncLoading && (
            <div className="absolute inset-0 z-10 flex items-center justify-center bg-[#0b0b0d]">
              <div className="flex flex-col items-center gap-3 text-sm text-muted-foreground">
                <Loader2 className="h-6 w-6 animate-spin" />
                正在连接 VNC…
              </div>
            </div>
          )}
          <iframe
            src={iframeSrc}
            className="absolute inset-0 h-full w-full border-0"
            title={`VNC - ${vm.name}`}
            sandbox="allow-scripts allow-same-origin"
            onLoad={() => setVncLoading(false)}
          />
        </div>
      </DialogContent>
    </Dialog>
  );
}

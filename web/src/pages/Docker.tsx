import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Boxes,
  Loader2,
  Pause,
  Play,
  RotateCw,
  ScrollText,
  Square,
  Search,
} from 'lucide-react';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
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
import { cn, timeAgo, truncate } from '@/lib/utils';
import { useSettingsStore } from '@/stores/settings';
import type { DockerContainer } from '@/types';

const STATUS_VARIANT: Record<DockerContainer['status'], 'success' | 'secondary' | 'warning' | 'destructive'> = {
  running: 'success',
  exited: 'secondary',
  paused: 'warning',
  restarting: 'warning',
  created: 'secondary',
  dead: 'destructive',
};

export default function DockerPage() {
  const refresh = useSettingsStore((s) => s.refreshInterval);
  const qc = useQueryClient();
  const [filter, setFilter] = useState('');
  const [logsFor, setLogsFor] = useState<DockerContainer | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ['docker'],
    queryFn: () => api.get<DockerContainer[]>('/docker/containers'),
    refetchInterval: refresh || false,
  });

  const act = async (id: string, action: 'start' | 'stop' | 'restart' | 'pause') => {
    await api.post(`/docker/containers/${id}/${action}`);
    qc.invalidateQueries({ queryKey: ['docker'] });
  };

  const containers = (data ?? []).filter((c) =>
    c.name.toLowerCase().includes(filter.toLowerCase()),
  );
  const running = (data ?? []).filter((c) => c.status === 'running').length;

  return (
    <div className="space-y-4 p-4 md:p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Docker 容器</h1>
          <p className="text-sm text-muted-foreground">
            {running} 个运行中 / {data?.length ?? 0} 个总计
          </p>
        </div>
        <div className="relative">
          <Search className="absolute left-2 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="pl-8"
            placeholder="搜索容器名…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        </div>
      </div>

      {isLoading ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" /> 加载容器列表…
        </div>
      ) : containers.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center gap-2 py-12 text-center text-sm text-muted-foreground">
            <Boxes className="h-8 w-8" />
            没有匹配的容器。
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {containers.map((c) => (
            <Card key={c.id}>
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <CardTitle className="truncate text-sm">
                      {c.name}
                    </CardTitle>
                    <div className="truncate text-xs text-muted-foreground">
                      {truncate(c.image, 38)}
                    </div>
                  </div>
                  <Badge variant={STATUS_VARIANT[c.status]}>{c.status}</Badge>
                </div>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="flex flex-wrap gap-1">
                  {c.ports.slice(0, 3).map((p) => (
                    <Badge key={p} variant="outline" className="font-mono text-[10px]">
                      {p}
                    </Badge>
                  ))}
                  {c.ports.length > 3 && (
                    <Badge variant="outline" className="text-[10px]">
                      +{c.ports.length - 3}
                    </Badge>
                  )}
                </div>
                <div className="text-xs text-muted-foreground">
                  启动于 {c.startedAt ? timeAgo(c.startedAt) : '—'}
                </div>
                <div className="flex flex-wrap gap-2">
                  {c.status !== 'running' && (
                    <Button size="sm" variant="success" onClick={() => act(c.id, 'start')}>
                      <Play className="h-3.5 w-3.5" /> 启动
                    </Button>
                  )}
                  {c.status === 'running' && (
                    <Button size="sm" variant="destructive" onClick={() => act(c.id, 'stop')}>
                      <Square className="h-3.5 w-3.5" /> 停止
                    </Button>
                  )}
                  <Button size="sm" variant="outline" onClick={() => act(c.id, 'restart')}>
                    <RotateCw className="h-3.5 w-3.5" /> 重启
                  </Button>
                  {c.status === 'running' && (
                    <Button size="sm" variant="ghost" onClick={() => act(c.id, 'pause')}>
                      <Pause className="h-3.5 w-3.5" /> 暂停
                    </Button>
                  )}
                  <Button size="sm" variant="ghost" onClick={() => setLogsFor(c)}>
                    <ScrollText className="h-3.5 w-3.5" /> 日志
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <LogDialog container={logsFor} onClose={() => setLogsFor(null)} />
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
  return (
    <Dialog open={!!container} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>日志 · {container?.name}</DialogTitle>
        </DialogHeader>
        <LogStream containerId={container?.id} />
      </DialogContent>
    </Dialog>
  );
}

function LogStream({ containerId }: { containerId?: string }) {
  const [lines, setLines] = useState<string[]>([]);
  const [connected, setConnected] = useState(false);

  useState(() => {
    if (!containerId) return;
    // TODO: connect to /ws/docker/<id>/logs once backend is ready.
    setConnected(true);
    setLines([
      '[stub] 后端 WebSocket 实现已规划，待 server/internal/ws 完成。',
      `[stub] container id = ${containerId}`,
    ]);
  });

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span
          className={cn(
            'h-2 w-2 rounded-full',
            connected ? 'bg-success' : 'bg-muted-foreground',
          )}
        />
        {connected ? '已连接' : '未连接'}
      </div>
      <pre className="h-80 overflow-auto rounded-md bg-black/80 p-3 font-mono text-xs leading-relaxed text-green-400">
        {lines.length > 0 ? lines.join('\n') : '等待日志…'}
      </pre>
    </div>
  );
}

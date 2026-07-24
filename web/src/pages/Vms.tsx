import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { motion } from 'framer-motion';
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
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { cn, formatBytes } from '@/lib/utils';
import type { VmInfo } from '@/types';
import { useSettingsStore } from '@/stores/settings';
import {
  staggerContainer,
  fadeUpVariants,
  springGentle,
} from '@/lib/motion';

const STATUS_VARIANT: Record<VmInfo['status'], 'success' | 'secondary' | 'warning'> = {
  running: 'success',
  shutoff: 'secondary',
  paused: 'warning',
  unknown: 'secondary',
};

const STATUS_BORDER: Record<string, string> = {
  running: 'border-l-emerald-500/60',
  shutoff: 'border-l-border',
  paused: 'border-l-amber-500/60',
  unknown: 'border-l-border',
};

const STATUS_BG: Record<string, string> = {
  running: '',
  shutoff: '',
  paused: 'bg-amber-500/5',
  unknown: '',
};

export default function VmsPage() {
  const { t } = useTranslation();
  const STATUS_LABEL: Record<string, string> = {
    running: t('vms.running'),
    shutoff: t('vms.shutoff'),
    paused: t('vms.paused'),
    unknown: t('vms.unknown'),
  };
  const qc = useQueryClient();
  const refresh = useSettingsStore((s) => s.refreshInterval);
  const [vncVm, setVncVm] = useState<VmInfo | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [pendingAction, setPendingAction] = useState<string | null>(null);
  const [confirmAction, setConfirmAction] = useState<{ id: string; action: 'stop'; name: string } | null>(null);
  const [search, setSearch] = useState('');
  const { data, isLoading, isError } = useQuery({
    queryKey: ['vms'],
    queryFn: () => api.get<VmInfo[]>('/vms'),
    refetchInterval: refresh || false,
  });

  const act = async (id: string, action: 'start' | 'stop' | 'shutdown' | 'resume' | 'suspend') => {
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
      setActionError(err instanceof ApiError ? err.message : t('common.failed'));
    } finally {
      setPendingAction(null);
    }
  };

  const running = (data ?? []).filter((v) => v.status === 'running').length;
  const filtered = (data ?? []).filter((v) =>
    v.name.toLowerCase().includes(search.toLowerCase()),
  );

  return (
    <div className="space-y-5 p-5 md:p-6">
      {actionError && (
        <motion.div
          className="flex items-center justify-between rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive"
          initial={{ opacity: 0, y: -8 }}
          animate={{ opacity: 1, y: 0 }}
        >
          <span>{actionError}</span>
          <button className="text-xs underline" onClick={() => setActionError(null)}>
            {t('common.close')}
          </button>
        </motion.div>
      )}

      {/* Header */}
      <motion.div
        className="flex flex-wrap items-end justify-between gap-4"
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={springGentle}
      >
        <div>
          <h1 className="text-display-md text-foreground">{t('vms.title')}</h1>
          <div className="mt-2 flex items-center gap-2 text-sm text-muted-foreground">
            <Badge
              variant={running > 0 ? 'success' : 'secondary'}
              className="text-[10px] font-semibold tracking-wide px-2.5"
            >
              {running > 0 ? t('vms.running') : t('vms.idle')}
            </Badge>
            <span className="text-xs">{running} / {data?.length ?? 0} {t('vms.vmCount')}</span>
          </div>
        </div>
        {(data ?? []).length > 0 && (
          <div className="relative w-48 shrink-0">
            <Search className="absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground/60" />
            <Input
              placeholder={t('vms.searchPlaceholder')}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="h-9 pl-9 text-sm rounded-xl border-border/50 bg-card/50 backdrop-blur-sm"
            />
          </div>
        )}
      </motion.div>

      {isLoading ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" /> {t('vms.loading')}
        </div>
      ) : isError ? (
        <motion.div className="card-bento p-12 text-center text-sm text-muted-foreground" initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
          {t('vms.cannotFetch')}
        </motion.div>
      ) : (data ?? []).length === 0 ? (
        <motion.div className="card-bento flex flex-col items-center gap-3 py-16 text-center text-sm text-muted-foreground" initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
          <Cpu className="h-10 w-10 text-muted-foreground/30" />
          {t('vms.noVM')}
        </motion.div>
      ) : filtered.length === 0 ? (
        <motion.div className="card-bento flex flex-col items-center gap-3 py-16 text-center text-sm text-muted-foreground" initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
          <Cpu className="h-10 w-10 text-muted-foreground/30" />
          {t('vms.noMatch')}
        </motion.div>
      ) : (
        <motion.div
          className="grid gap-4 md:grid-cols-2 xl:grid-cols-3"
          variants={staggerContainer}
          initial="hidden"
          animate="visible"
        >
          {filtered.map((vm) => (
            <motion.div
              key={vm.id}
              className={cn(
                'card-bento flex flex-col border-l-2 overflow-hidden',
                STATUS_BORDER[vm.status] ?? 'border-l-border',
                STATUS_BG[vm.status],
              )}
              variants={fadeUpVariants}
              whileHover={{ y: -2 }}
              transition={springGentle}
            >
              <div className="px-5 pt-5 pb-3">
                <div className="flex items-start justify-between gap-2">
                  <div className="text-sm font-semibold truncate">{vm.name}</div>
                  <Badge variant={STATUS_VARIANT[vm.status]} className="text-[9px] px-1.5 py-0 leading-none shrink-0 font-semibold tracking-wide">{STATUS_LABEL[vm.status] ?? vm.status}</Badge>
                </div>
              </div>
              <div className="flex-1 flex flex-col gap-3 px-5 pb-5">
                <div className="flex items-center gap-1.5">
                  <span className="inline-flex items-center gap-1 rounded-md bg-blue-500/10 px-1.5 py-0.5 text-[10px] font-mono-data tabular-nums text-ind-blue">
                    <Cpu className="h-2.5 w-2.5" />
                    {vm.vcpus} vCPU
                  </span>
                  <span className="inline-flex items-center gap-1 rounded-md bg-violet-500/10 px-1.5 py-0.5 text-[10px] font-mono-data tabular-nums text-ind-violet">
                    <MemoryStick className="h-2.5 w-2.5" />
                    {formatBytes(vm.memoryBytes)}
                  </span>
                </div>
                <div className="mt-auto flex flex-wrap gap-2 pt-1">
                  {vm.status !== 'running' && vm.status !== 'paused' && (
                    <Button size="sm" variant="success" onClick={() => act(vm.id, 'start')} disabled={pendingAction !== null} className="rounded-lg h-8">
                      {pendingAction === `${vm.id}:start` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />} {t('vms.start')}
                    </Button>
                  )}
                  {vm.status === 'running' && (
                    <>
                      <Button size="sm" variant="outline" onClick={() => act(vm.id, 'shutdown')} disabled={pendingAction !== null} className="rounded-lg h-8">
                        {pendingAction === `${vm.id}:shutdown` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Power className="h-3.5 w-3.5" />} {t('vms.shutdown')}
                      </Button>
                      <Button size="sm" variant="destructive" onClick={() => act(vm.id, 'stop')} disabled={pendingAction !== null} className="rounded-lg h-8">
                        {pendingAction === `${vm.id}:stop` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Square className="h-3.5 w-3.5" />} {t('vms.forceStop')}
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => act(vm.id, 'suspend')} disabled={pendingAction !== null} className="rounded-lg h-8">
                        <Pause className="h-3.5 w-3.5" /> {t('vms.pause')}
                      </Button>
                    </>
                  )}
                  {vm.status === 'paused' && (
                    <Button size="sm" variant="success" onClick={() => act(vm.id, 'resume')} disabled={pendingAction !== null} className="rounded-lg h-8">
                      <Play className="h-3.5 w-3.5" /> {t('vms.resume')}
                    </Button>
                  )}
                  {vm.status === 'running' && (
                    <Button size="sm" variant="outline" onClick={() => setVncVm(vm)} disabled={pendingAction !== null} className="rounded-lg h-8">
                      <Monitor className="h-3.5 w-3.5" /> {t('vms.console')}
                    </Button>
                  )}
                </div>
              </div>
            </motion.div>
          ))}
        </motion.div>
      )}

      <ConfirmDialog
        open={!!confirmAction}
        title={t('vms.confirmForceStopTitle')}
        description={t('vms.confirmForceStopDesc', { name: confirmAction?.name ?? '' })}
        confirmText={t('vms.forceStop')}
        variant="destructive"
        onConfirm={() => confirmAction && act(confirmAction.id, confirmAction.action)}
        onCancel={() => setConfirmAction(null)}
      />

      <VNCDialog vm={vncVm} onClose={() => setVncVm(null)} />
    </div>
  );
}

/* ── VNC Dialog ── */
function VNCDialog({ vm, onClose }: { vm: VmInfo | null; onClose: () => void }) {
  const { t } = useTranslation();
  const [vncLoading, setVncLoading] = useState(true);

  useEffect(() => {
    if (vm) setVncLoading(true);
  }, [vm]);

  if (!vm) return null;

  const vncWsUrl = wsUrl(`/ws/vnc?vm=${encodeURIComponent(vm.id)}`);
  const iframeSrc = `/vnc/vnc_lite.html?url=${encodeURIComponent(vncWsUrl)}&scale=true`;

  return (
    <Dialog open={!!vm} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-4xl p-0 gap-0 overflow-hidden rounded-2xl">
        <DialogHeader className="px-4 pt-4 pb-2">
          <DialogTitle className="flex items-center gap-2">
            <Monitor className="h-4 w-4" />
            {t('vms.vncConsole')} {vm.name}
          </DialogTitle>
        </DialogHeader>
        <div className="px-4 pb-1 text-xs text-muted-foreground">
          {t('vms.vncDesc')}
        </div>
        <div className="relative" style={{ height: '70vh' }}>
          {vncLoading && (
            <div className="absolute inset-0 z-10 flex items-center justify-center bg-[#0b0b0d]">
              <div className="flex flex-col items-center gap-3 text-sm text-muted-foreground">
                <Loader2 className="h-6 w-6 animate-spin" />
                {t('vms.vncConnecting')}
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

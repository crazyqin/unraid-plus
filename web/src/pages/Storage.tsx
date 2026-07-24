import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { motion } from 'framer-motion';
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  HardDrive,
  Loader2,
  Moon,
  Play,
  RefreshCw,
  ShieldCheck,
  Square,
  Sun,
  Thermometer,
  XCircle,
  Usb,
  Wifi,
  Zap,
  Cable,
} from 'lucide-react';
import { api, ApiError } from '@/lib/api';
import { staggerContainer, fadeUpVariants, springGentle } from '@/lib/motion';
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

const DISK_STATUS_KEY: Record<DiskInfo['status'], string> = {
  ok: 'storage.statusOk',
  warning: 'storage.statusWarn',
  critical: 'storage.statusDanger',
  unknown: 'storage.statusUnknown',
};

const ARRAY_STATE_KEY: Record<string, string> = {
  started: 'storage.arrayStarted',
  stopped: 'storage.arrayStopped',
  starting: 'storage.arrayStarting',
  stopping: 'storage.arrayStopping',
};

const SMART_STATUS_META: Record<
  SmartInfo['status'],
  { variant: 'success' | 'warning' | 'destructive' | 'secondary'; icon: typeof CheckCircle2 }
> = {
  ok: { variant: 'success', icon: CheckCircle2 },
  warning: { variant: 'warning', icon: AlertTriangle },
  failing: { variant: 'destructive', icon: XCircle },
  standby: { variant: 'secondary', icon: Moon },
  unknown: { variant: 'success', icon: CheckCircle2 },
};

const SMART_LABEL_KEY: Record<SmartInfo['status'], string> = {
  ok: 'storage.smartOk',
  warning: 'storage.smartWarn',
  failing: 'storage.smartFail',
  standby: 'storage.standby',
  unknown: '',
};

const STATUS_BORDER: Record<string, string> = {
  ok: 'border-l-emerald-500/60',
  warning: 'border-l-amber-500/60',
  critical: 'border-l-red-500/70',
  unknown: 'border-l-border',
};

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
  const { t } = useTranslation();
  const refresh = useSettingsStore((s) => s.refreshInterval);
  const qc = useQueryClient();

  const [refreshMsg, setRefreshMsg] = useState<{ kind: 'ok' | 'err'; text: string } | null>(null);
  const [confirmStopArray, setConfirmStopArray] = useState(false);
  const [confirmParity, setConfirmParity] = useState(false);
  const [parityAction, setParityAction] = useState<'start' | 'correcting'>('start');

  const { data, isLoading, isError } = useQuery({
    queryKey: ['storage'],
    queryFn: () => api.get<ArrayStatus>('/storage'),
    refetchInterval: refresh || false,
  });

  const refreshMut = useMutation({
    mutationFn: () => api.post<SmartRefreshResp>('/smart/refresh'),
    onSuccess: (r) => {
      setRefreshMsg({ kind: 'ok', text: r.message ?? t('storage.smartRefreshed', { count: r.count }) });
      qc.invalidateQueries({ queryKey: ['storage'] });
      window.setTimeout(() => setRefreshMsg(null), 3000);
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError ? e.message : t('storage.smartRefreshFailed');
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
        text: r.ok ? (r.message ?? t('common.success')) : (r.detail ?? r.message ?? t('common.failed')),
      });
      qc.invalidateQueries({ queryKey: ['storage'] });
      window.setTimeout(() => setRefreshMsg(null), 4000);
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError ? e.message : t('common.failed');
      setRefreshMsg({ kind: 'err', text: msg });
      window.setTimeout(() => setRefreshMsg(null), 4000);
    },
  });

  const parityMut = useMutation({
    mutationFn: (action: 'start' | 'stop' | 'correcting' | 'resume') =>
      api.post<{ ok: boolean; message?: string; detail?: string }>(
        `/storage/parity/${action}`,
      ),
    onSuccess: (r) => {
      setRefreshMsg({
        kind: r.ok ? 'ok' : 'err',
        text: r.ok ? (r.message ?? t('common.success')) : (r.detail ?? r.message ?? t('common.failed')),
      });
      qc.invalidateQueries({ queryKey: ['parity'] });
      window.setTimeout(() => setRefreshMsg(null), 4000);
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError ? e.message : t('common.failed');
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
        <Loader2 className="h-4 w-4 animate-spin" /> {t('storage.loading')}
      </div>
    );
  }
  if (isError || !data) {
    return (
      <motion.div className="card-bento m-6 p-12 text-center text-sm text-muted-foreground" initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
        {t('storage.cannotFetch')}
      </motion.div>
    );
  }

  const totalDisks = data.disks.length + data.cacheDisks.length;
  const healthyDisks = [...data.disks, ...data.cacheDisks].filter(d => d.status === 'ok').length;

  return (
    <div className="space-y-5 p-5 md:p-6">
      {/* Degraded mode banner (API-only, no SSH) */}
      {data.degraded && (
        <motion.div
          className="flex items-center gap-2 rounded-xl border border-amber-500/30 bg-amber-500/5 p-3 text-sm text-amber-600 dark:text-amber-400"
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          transition={springGentle}
        >
          <Wifi className="h-4 w-4 shrink-0" />
          <span>{t('storage.degradedNotice')}</span>
        </motion.div>
      )}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <motion.div
          className="flex items-center gap-3"
          initial={{ opacity: 0, y: 12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={springGentle}
        >
          <div className={cn(
            'flex h-10 w-10 items-center justify-center rounded-xl',
            data.state === 'started' ? 'bg-success/15 text-success' : 'bg-muted text-muted-foreground',
          )}>
            <HardDrive className="h-5 w-5" />
          </div>
          <div>
            <h1 className="text-display-md text-foreground">{t('storage.title')}</h1>
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Badge
                variant={data.state === 'started' ? 'success' : 'secondary'}
                className="text-[10px] tracking-wide"
              >
                {ARRAY_STATE_KEY[data.state] ? t(ARRAY_STATE_KEY[data.state]) : data.state}
              </Badge>
              <span>{healthyDisks}/{totalDisks} {t('storage.diskHealthy')}</span>
            </div>
          </div>
        </motion.div>
        <div className="flex flex-wrap items-center gap-2">
          {data.state === 'started' ? (
            <Button
              size="sm"
              variant="destructive"
              className="rounded-lg h-8"
              disabled={arrayMut.isPending}
              onClick={() => setConfirmStopArray(true)}
            >
              <Square className="h-3.5 w-3.5" /> {t('storage.stopArray')}
            </Button>
          ) : (
            <Button
              size="sm"
              variant="success"
              className="rounded-lg h-8"
              disabled={arrayMut.isPending}
              onClick={() => arrayMut.mutate('start')}
            >
              {arrayMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
              {t('storage.startArray')}
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
            className="rounded-lg h-8"
            disabled={refreshMut.isPending}
            onClick={() => refreshMut.mutate()}
          >
            <RefreshCw className={cn('h-3.5 w-3.5', refreshMut.isPending && 'animate-spin')} />
            {t('storage.smartRefresh')}
          </Button>
        </div>
      </div>

      {/* Parity check progress + controls */}
      {parity && parity.state === 'checking' && (
        <motion.div className="card-bento border-primary/30" variants={fadeUpVariants} whileHover={{ y: -2 }} transition={springGentle}>
          <div className="pb-3">
            <div className="flex items-center justify-between text-base">
              <span className="flex items-center gap-2">
                <Activity className="h-4 w-4 skeleton-shimmer text-primary" />
                {t('storage.parityCheckRunning')}
              </span>
              <div className="flex items-center gap-2">
                {parity.errors > 0 && (
                  <Badge variant="destructive" className="text-[10px] tracking-wide">
                    <AlertTriangle className="mr-1 h-3 w-3" />
                    {parity.errors} {t('common.error')}
                  </Badge>
                )}
                <Button
                  size="sm"
                  variant="outline"
                  className="rounded-lg h-8"
                  disabled={parityMut.isPending}
                  onClick={() => parityMut.mutate('stop')}
                >
                  <Square className="h-3.5 w-3.5" /> {t('storage.stopCheck')}
                </Button>
              </div>
            </div>
          </div>
          <div className="space-y-2">
            <div className="flex items-center gap-3">
              <Progress className="h-2.5 flex-1" value={parity.progress} indicatorClassName="bg-primary" />
              <span className="shrink-0 text-sm font-semibold tabular-nums font-mono-data">
                {formatPct(parity.progress)}
              </span>
            </div>
            <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
              <span>{t('storage.speed')}{parity.speed}</span>
              <span>{t('storage.remaining')}{parity.remaining}</span>
            </div>
          </div>
        </motion.div>
      )}

      {/* Parity check idle */}
      {parity && parity.state === 'idle' && (
        <div className="flex items-center justify-between rounded-xl border border-dashed p-3">
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <ShieldCheck className="h-4 w-4 text-muted-foreground" />
            {t('storage.parityIdle')}
          </div>
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              className="rounded-lg h-8"
              disabled={parityMut.isPending}
              onClick={() => {
                setParityAction('start');
                setConfirmParity(true);
              }}
            >
              <Play className="h-3.5 w-3.5" /> {t('storage.startCheckBtn')}
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="rounded-lg h-8"
              disabled={parityMut.isPending}
              onClick={() => {
                setParityAction('correcting');
                setConfirmParity(true);
              }}
            >
              <ShieldCheck className="h-3.5 w-3.5" /> {t('storage.correctingCheck')}
            </Button>
          </div>
        </div>
      )}

      <DiskGroup title={t('storage.arrayDisks')} disks={data.disks} />
      <DiskGroup title={t('storage.cachePool')} disks={data.cacheDisks} />

      <ConfirmDialog
        open={confirmStopArray}
        title={t('storage.confirmStopArray')}
        description={t('storage.confirmStopArrayDesc')}
        confirmText={t('storage.stopArray')}
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
        title={parityAction === 'correcting' ? t('storage.confirmStartCorrecting') : t('storage.confirmStartCheck')}
        description={parityAction === 'correcting'
          ? t('storage.confirmCorrectingDesc')
          : t('storage.confirmCheckDesc')}
        confirmText={parityAction === 'correcting' ? t('storage.startCorrecting') : t('storage.startCheckBtn')}
        loading={parityMut.isPending}
        onConfirm={() => {
          parityMut.mutate(parityAction);
          setConfirmParity(false);
        }}
        onCancel={() => setConfirmParity(false)}
      />
    </div>
  );
}

/* ------------------------------ Disk Group ------------------------------- */

function DiskGroup({ title, disks }: { title: string; disks: DiskInfo[] }) {
  const { t } = useTranslation();
  if (disks.length === 0) return null;
  const total = disks.reduce((s, d) => s + d.sizeBytes, 0);
  const used = disks.reduce((s, d) => s + d.usedBytes, 0);
  const usedPct = total > 0 ? (used / total) * 100 : 0;

  const groupIndicatorColor =
    usedPct > 90 ? 'bg-destructive' : usedPct > 75 ? 'bg-warning' : 'bg-success';

  return (
    <motion.div className="card-bento" variants={fadeUpVariants} whileHover={{ y: -2 }} transition={springGentle}>
      <div className="pb-3">
        <div className="flex items-center justify-between text-base">
          <span className="flex items-center gap-2">
            <HardDrive className="h-4 w-4 text-muted-foreground" />
            {title}
            <span className="text-xs font-normal text-muted-foreground">
              {disks.length} {t('storage.diskCount')}
            </span>
          </span>
          <span className="text-xs font-normal tabular-nums text-muted-foreground font-mono-data">
            {formatBytes(used)} / {formatBytes(total)}
          </span>
        </div>
        {/* Group-level mini progress bar */}
        <div className="mt-2 flex items-center gap-2">
          <Progress
            className="h-1.5"
            value={usedPct}
            indicatorClassName={groupIndicatorColor}
          />
          <span className="shrink-0 text-xs font-medium tabular-nums text-muted-foreground font-mono-data">
            {formatPct(usedPct)}
          </span>
        </div>
      </div>
      <div className="space-y-2.5">
        <motion.div variants={staggerContainer} initial="hidden" animate="visible" className="space-y-2.5">
          {disks.map((d) => (
            <DiskRow key={d.device} disk={d} />
          ))}
        </motion.div>
      </div>
    </motion.div>
  );
}

/* ------------------------------ Disk Row --------------------------------- */

function DiskRow({ disk: d }: { disk: DiskInfo }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const pct = d.sizeBytes > 0 ? (d.usedBytes / d.sizeBytes) * 100 : 0;

  const spinMut = useMutation({
    mutationFn: ({ device, action }: { device: string; action: 'spinup' | 'spindown' }) =>
      api.post<{ ok: boolean; message?: string }>('/storage/disk/spin', { device, action }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['storage'] });
    },
  });

  const unraidColorMap: Record<string, string> = {
    'green-on': 'bg-emerald-500',
    'green-off': 'bg-emerald-500/30',
    'yellow-on': 'bg-amber-500',
    'yellow-off': 'bg-amber-500/30',
    'red-on': 'bg-red-500',
    'red-off': 'bg-red-500/30',
    'grey-off': 'bg-muted-foreground/30',
  };
  const ledDot = d.color ? unraidColorMap[d.color] : '';
  const isSsd = d.rotational === '0';
  const TransportIcon = d.transport === 'nvme' ? Zap : d.transport === 'usb' ? Usb : Cable;

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

  const progressColor =
    pct > 90 ? 'bg-destructive' : pct > 75 ? 'bg-warning' : 'bg-primary';

  const isProblemDisk = d.status === 'warning' || d.status === 'critical';

  return (
    <motion.div
      variants={fadeUpVariants}
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
                <Badge variant="secondary" className="px-1 py-0 text-[9px] leading-none tracking-wide">
                  SSD
                </Badge>
              )}
            </div>
            <div className="mt-0.5 flex items-center gap-1 text-[10px] font-mono-data text-muted-foreground/70">
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
                'inline-flex items-center gap-0.5 rounded-lg border px-1.5 py-0.5 text-[10px] font-medium tabular-nums',
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
            <Badge variant="destructive" className="px-1.5 py-0 text-[9px] leading-none tracking-wide">
              <AlertTriangle className="mr-0.5 h-2.5 w-2.5" />
              {d.errors}
            </Badge>
          )}
          {/* Status badge */}
          <Badge
            variant={DISK_STATUS_VARIANT[d.status]}
            className="px-1.5 py-0 text-[9px] leading-none tracking-wide"
          >
            {t(DISK_STATUS_KEY[d.status] ?? d.status)}
          </Badge>
          {/* SMART badge — show for ok/warning/failing, show standby for standby */}
          {(d.smart?.available && d.smart.status !== 'unknown') && (
            <Badge
              variant={SMART_STATUS_META[d.smart.status].variant}
              className="px-1.5 py-0 text-[9px] leading-none tracking-wide"
            >
              <SmartStatusIcon status={d.smart.status} />
              {t(SMART_LABEL_KEY[d.smart.status])}
            </Badge>
          )}
          {/* Standby badge when smart is unavailable and disk appears spun down */}
          {d.smart?.status === 'standby' && (
            <Badge
              variant="secondary"
              className="px-1.5 py-0 text-[9px] leading-none tracking-wide"
            >
              <Moon className="mr-0.5 h-3 w-3" />
              {t('storage.standby')}
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
          <span className="font-medium font-mono-data">{formatPct(pct)}</span>
          <span className="text-muted-foreground">
            {formatBytes(d.usedBytes)}/{formatBytes(d.sizeBytes)}
          </span>
        </div>
      </div>

      {/* Row 3: Read/Write rate pills + spin controls */}
      <div className="mt-1.5 flex items-center justify-between gap-1.5">
        <div className="flex items-center gap-1.5">
          <span className="inline-flex items-center gap-1 rounded-lg bg-emerald-500/10 px-1.5 py-0.5 text-[10px] font-mono-data tabular-nums text-ind-emerald">
            ↓ {formatRate(d.readBytesPerSec)}
          </span>
          <span className="inline-flex items-center gap-1 rounded-lg bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-mono-data tabular-nums text-ind-amber">
            ↑ {formatRate(d.writeBytesPerSec)}
          </span>
        </div>
        {/* Disk spin controls (only for HDDs with a physical device name) */}
        {d.rotational === '1' && d.diskName && d.diskName !== 'flash' && (
          <div className="flex items-center gap-1">
            <button
              className="inline-flex items-center gap-0.5 rounded-lg px-1.5 py-0.5 text-[10px] text-muted-foreground transition-colors hover:bg-emerald-500/10 hover:text-emerald-500 disabled:opacity-50"
              disabled={spinMut.isPending}
              onClick={() => {
                const dev = d.device.replace(/^\/dev\//, '');
                spinMut.mutate({ device: dev, action: 'spinup' });
              }}
              title={t('storage.spinUp')}
            >
              <Sun className="h-2.5 w-2.5" /> {t('storage.spinUpShort')}
            </button>
            <button
              className="inline-flex items-center gap-0.5 rounded-lg px-1.5 py-0.5 text-[10px] text-muted-foreground transition-colors hover:bg-amber-500/10 hover:text-amber-500 disabled:opacity-50"
              disabled={spinMut.isPending}
              onClick={() => {
                const dev = d.device.replace(/^dev\//, '');
                spinMut.mutate({ device: dev, action: 'spindown' });
              }}
              title={t('storage.spinDown')}
            >
              <Moon className="h-2.5 w-2.5" /> {t('storage.spinDownShort')}
            </button>
          </div>
        )}
      </div>

      {/* SMART detail */}
      <SmartDetail smart={d.smart} expanded={isProblemDisk} />
    </motion.div>
  );
}

/* ------------------------------- SMART bits ------------------------------ */

function SmartStatusIcon({ status }: { status: SmartInfo['status'] }) {
  const Icon = SMART_STATUS_META[status].icon;
  return <Icon className="mr-0.5 h-2.5 w-2.5" />;
}

/**
 * SMART detail row. Collapsed by default for healthy disks (click to expand),
 * auto-expanded for warning/failing disks.
 */
function SmartDetail({ smart, expanded = false }: { smart?: SmartInfo; expanded?: boolean }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(expanded);

  // Sync expanded prop: when a disk goes from ok→warning, auto-expand.
  useEffect(() => {
    setOpen(expanded);
  }, [expanded]);

  // Don't render SMART details for standby disks (no data available)
  if (smart?.status === 'standby') return null;

  if (!smart || !smart.available || smart.status === 'unknown') return null;

  const counters: { label: string; value: number }[] = [
    { label: t('storage.reallocated'), value: smart.reallocated },
    { label: t('storage.pending'), value: smart.pending },
    { label: t('storage.offlineUncorrectable'), value: smart.uncorrectable },
    { label: t('storage.mediaErrors'), value: smart.mediaErrors },
  ].filter((c) => c.value > 0);

  const isFailing = smart.status === 'failing';
  const isWarning = smart.status === 'warning';
  const hasProblem = isFailing || isWarning;

  // For healthy disks, show a compact one-liner with expand toggle.
  // For problem disks, show full detail (auto-expanded).
  return (
    <div
      className={cn(
        'mt-2 rounded-lg border-t pt-1.5 text-[10px]',
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
        <span className="flex items-center gap-1.5 truncate font-mono-data">
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
          <span className="text-muted-foreground/60">{t('storage.refreshedAt')} {timeAgo(smart.fetchedAt)}</span>
          {!hasProblem && (
            <ChevronDown className={cn('h-3 w-3 transition-transform', open && 'rotate-180')} />
          )}
        </span>
      </div>

      {/* Expanded detail for healthy disks */}
      {!hasProblem && open && (
        <div className="mt-1 text-ind-emerald">
          {t('storage.smartOkDetail')}
        </div>
      )}

      {/* Problem disk detail (always shown, no collapse) */}
      {hasProblem && (
        <>
          {isFailing && (
            <div className="mt-1 font-medium text-destructive">
              {t('storage.smartFailDetail')}
            </div>
          )}
          {counters.length > 0 && (
            <div
              className={cn(
                'mt-1 flex flex-wrap gap-x-3 gap-y-0.5 font-medium font-mono-data',
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

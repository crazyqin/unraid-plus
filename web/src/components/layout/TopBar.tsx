import { useQuery } from '@tanstack/react-query';
import { motion, AnimatePresence } from 'framer-motion';
import { Activity, Globe, Moon, RefreshCw, Sun, Zap, Loader2 } from 'lucide-react';
import { useState } from 'react';
import { Button } from '@/components/ui/button';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { useSettingsStore, THEMES } from '@/stores/settings';
import { useAuthStore } from '@/stores/auth';
import { cn } from '@/lib/utils';
import { useTranslation } from 'react-i18next';
import i18n from '@/i18n';
import { springSnappy } from '@/lib/motion';

const LANGUAGES = [
  { code: 'zh', label: '中文' },
  { code: 'en', label: 'English' },
  { code: 'ja', label: '日本語' },
  { code: 'ko', label: '한국어' },
  { code: 'fr', label: 'Français' },
  { code: 'de', label: 'Deutsch' },
  { code: 'es', label: 'Español' },
];

export default function TopBar() {
  const { t } = useTranslation();
  const server = useAuthStore((s) => s.server);
  const sshAvailable = useAuthStore((s) => s.sshAvailable);
  const apiAvailable = useAuthStore((s) => s.apiAvailable);
  const refreshInterval = useSettingsStore((s) => s.refreshInterval);
  const setRefreshInterval = useSettingsStore((s) => s.setRefreshInterval);
  const theme = useSettingsStore((s) => s.theme);
  const setTheme = useSettingsStore((s) => s.setTheme);

  // Derive connection mode — never show dual unless BOTH flags are true.
  const modeLabel =
    sshAvailable && apiAvailable ? t('connection.dual') :
    apiAvailable && !sshAvailable ? t('connection.api') :
    sshAvailable && !apiAvailable ? t('connection.ssh') :
    t('connection.disconnected');
  const modeStyle =
    sshAvailable && apiAvailable ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' :
    apiAvailable && !sshAvailable ? 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400' :
    sshAvailable && !apiAvailable ? 'border-sky-500/40 bg-sky-500/10 text-sky-600 dark:text-sky-400' :
    'border-muted-foreground/40 bg-muted text-muted-foreground';

  const [themeOpen, setThemeOpen] = useState(false);
  const [langOpen, setLangOpen] = useState(false);

  const health = useQuery({
    queryKey: ['health'],
    queryFn: async () => {
      const res = await fetch('/health', { credentials: 'include' });
      if (!res.ok) throw new Error('health check failed');
      return res.json() as Promise<{ ok: boolean; uptime: number }>;
    },
    refetchInterval: refreshInterval,
  });

  const online = health.data?.ok ?? false;
  const currentTheme = THEMES.find((th) => th.id === theme);

  const isReconnecting = !sshAvailable && !apiAvailable && !!server;

  return (
    <>
      {/* Reconnecting banner */}
      <AnimatePresence>
        {isReconnecting && (
          <motion.div
            className="flex items-center justify-center gap-2 bg-amber-500/10 border-b border-amber-500/20 py-1.5 text-[11px] font-medium text-amber-600 dark:text-amber-400"
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: 'auto' }}
            exit={{ opacity: 0, height: 0 }}
            transition={{ duration: 0.2 }}
          >
            <Loader2 className="h-3 w-3 animate-spin" />
            {t('connection.reconnecting', '正在自动重连…')}
          </motion.div>
        )}
      </AnimatePresence>

    {/* z-50: dropdowns (theme/lang) must paint above main content (e.g. dashboard charts) */}
    <header className="relative z-50 flex h-14 shrink-0 items-center justify-between border-b border-border/40 bg-card/80 backdrop-blur-2xl px-5">
      <div className="flex items-center gap-3">
        {/* Online status pill */}
        <motion.div
          className={cn(
            'flex items-center gap-2 rounded-full border px-3 py-1 text-[11px] font-medium tracking-wide',
            online
              ? 'border-success/30 bg-success/5 text-success'
              : 'border-destructive/30 bg-destructive/5 text-destructive',
          )}
          initial={{ opacity: 0, scale: 0.9 }}
          animate={{ opacity: 1, scale: 1 }}
          transition={springSnappy}
        >
          <span className={cn('relative flex h-2 w-2', online && 'status-pulse')}>
            <span className={cn(
              'inline-flex h-2 w-2 rounded-full',
              online ? 'bg-emerald-500' : 'bg-destructive',
            )} />
          </span>
          {online ? t('common.online') : t('common.offline')}
        </motion.div>

        {/* Refresh interval selector */}
        <div className="hidden items-center gap-2 text-xs text-muted-foreground/70 sm:flex">
          <Activity className="h-3 w-3" />
          <select
            className="rounded-lg border border-border/50 bg-background/50 px-2 py-0.5 text-xs backdrop-blur-sm transition-colors hover:border-border focus:outline-none focus:ring-1 focus:ring-ring"
            value={refreshInterval}
            onChange={(e) => setRefreshInterval(Number(e.target.value))}
          >
            <option value={1000}>1s</option>
            <option value={2000}>2s</option>
            <option value={5000}>5s</option>
            <option value={15000}>15s</option>
            <option value={0}>{t('common.pause')}</option>
          </select>
        </div>

        {/* Server name */}
        <span className="max-w-[200px] truncate text-xs font-medium text-muted-foreground/80" title={server?.label || server?.host}>
          {server?.label || server?.host}
        </span>

        {/* Connection mode badge */}
        <Tooltip>
          <TooltipTrigger asChild>
            <div className={cn(
              'flex items-center gap-1 rounded-full border px-2.5 py-0.5 text-[10px] font-semibold tracking-wide cursor-default',
              modeStyle,
            )}>
              <Zap className="h-3 w-3" />
              {modeLabel}
            </div>
          </TooltipTrigger>
          <TooltipContent>
            {sshAvailable && apiAvailable ? t('connection.dualTip') :
             apiAvailable && !sshAvailable ? t('connection.apiTip') :
             sshAvailable && !apiAvailable ? t('connection.sshTip') :
             t('connection.notConnected')}
          </TooltipContent>
        </Tooltip>
      </div>

      <div className="flex items-center gap-1">
        {/* Language quick switch */}
        <div className="relative">
          <Tooltip>
            <TooltipTrigger asChild>
              <motion.div whileHover={{ scale: 1.05 }} whileTap={{ scale: 0.95 }}>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8 rounded-xl"
                  onClick={() => setLangOpen((v) => !v)}
                  title={t('settings.language')}
                >
                  <Globe className="h-4 w-4" />
                </Button>
              </motion.div>
            </TooltipTrigger>
            <TooltipContent>{i18n.language?.split('-')[0]?.toUpperCase() ?? 'ZH'}</TooltipContent>
          </Tooltip>

          <AnimatePresence>
            {langOpen && (
              <>
                <div className="fixed inset-0 z-40" onClick={() => setLangOpen(false)} />
                <motion.div
                  className="absolute right-0 top-full z-50 mt-2 w-36 rounded-xl border border-border/50 bg-card/95 backdrop-blur-2xl p-1.5 shadow-2xl"
                  initial={{ opacity: 0, y: -8, scale: 0.95 }}
                  animate={{ opacity: 1, y: 0, scale: 1 }}
                  exit={{ opacity: 0, y: -4, scale: 0.97 }}
                  transition={springSnappy}
                >
                  {LANGUAGES.map((lang) => (
                    <motion.button
                      key={lang.code}
                      onClick={() => { i18n.changeLanguage(lang.code); setLangOpen(false); }}
                      className={cn(
                        'flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-xs transition-colors',
                        (i18n.language?.split('-')[0] ?? 'zh') === lang.code
                          ? 'bg-primary/10 text-primary font-semibold'
                          : 'text-foreground hover:bg-accent',
                      )}
                      whileHover={{ x: 2 }}
                      whileTap={{ scale: 0.97 }}
                    >
                      {lang.label}
                    </motion.button>
                  ))}
                </motion.div>
              </>
            )}
          </AnimatePresence>
        </div>

        {/* Theme quick switch */}
        <div className="relative">
          <Tooltip>
            <TooltipTrigger asChild>
              <motion.div whileHover={{ scale: 1.05 }} whileTap={{ scale: 0.95 }}>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8 rounded-xl"
                  onClick={() => setThemeOpen((v) => !v)}
                  title={t('topbar.switchTheme')}
                >
                  {theme === 'daylight' ? (
                    <Sun className="h-4 w-4" />
                  ) : (
                    <Moon className="h-4 w-4" />
                  )}
                </Button>
              </motion.div>
            </TooltipTrigger>
            <TooltipContent>{t('topbar.theme')}{currentTheme ? t('themes.' + currentTheme.id) : ''}</TooltipContent>
          </Tooltip>

          <AnimatePresence>
            {themeOpen && (
              <>
                <div className="fixed inset-0 z-40" onClick={() => setThemeOpen(false)} />
                <motion.div
                  className="absolute right-0 top-full z-50 mt-2 w-56 rounded-xl border border-border/50 bg-card/95 backdrop-blur-2xl p-2 shadow-2xl"
                  initial={{ opacity: 0, y: -8, scale: 0.95 }}
                  animate={{ opacity: 1, y: 0, scale: 1 }}
                  exit={{ opacity: 0, y: -4, scale: 0.97 }}
                  transition={springSnappy}
                >
                  <div className="mb-1.5 px-2 text-[10px] font-semibold uppercase tracking-widest text-muted-foreground/60">
                    {t('topbar.themeStyle')}
                  </div>
                  <div className="space-y-0.5">
                    {THEMES.map((themeMeta) => (
                      <motion.button
                        key={themeMeta.id}
                        onClick={() => { setTheme(themeMeta.id); setThemeOpen(false); }}
                        className={cn(
                          'flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-sm transition-colors',
                          theme === themeMeta.id
                            ? 'bg-primary/10 text-primary'
                            : 'text-foreground hover:bg-accent',
                        )}
                        whileHover={{ x: 2 }}
                        whileTap={{ scale: 0.97 }}
                      >
                        <div className={`h-4 w-4 rounded-full ${themeMeta.accent} shrink-0 shadow-sm ring-1 ring-white/10`} />
                        <div className="min-w-0 flex-1">
                          <div className="text-xs font-medium leading-tight">{t('themes.' + themeMeta.id)}</div>
                          <div className="text-[10px] text-muted-foreground leading-tight">{t('themes.' + themeMeta.id + 'Desc')}</div>
                        </div>
                        {theme === themeMeta.id && (
                          <motion.div
                            className="h-1.5 w-1.5 rounded-full bg-primary"
                            layoutId="theme-dot"
                            transition={springSnappy}
                          />
                        )}
                      </motion.button>
                    ))}
                  </div>
                </motion.div>
              </>
            )}
          </AnimatePresence>
        </div>

        {/* Refresh */}
        <Tooltip>
          <TooltipTrigger asChild>
            <motion.div whileHover={{ scale: 1.05, rotate: 45 }} whileTap={{ scale: 0.9 }}>
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8 rounded-xl"
                onClick={() => health.refetch()}
                title={t('common.refresh')}
              >
                <RefreshCw
                  className={cn('h-4 w-4', health.isFetching && 'animate-spin')}
                />
              </Button>
            </motion.div>
          </TooltipTrigger>
          <TooltipContent>{t('common.refreshNow')}</TooltipContent>
        </Tooltip>
      </div>
    </header>
    </>
  );
}

import { useState } from 'react';
import { NavLink, useLocation, useNavigate } from 'react-router-dom';
import { motion, AnimatePresence } from 'framer-motion';
import {
  LayoutDashboard,
  HardDrive,
  FolderTree,
  TerminalSquare,
  Cpu,
  Container,
  Settings,
  ChevronLeft,
  Plus,
  Wifi,
  WifiOff,
  Trash2,
  RefreshCw,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { useSettingsStore } from '@/stores/settings';
import { useAuthStore } from '@/stores/auth';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { ConfirmDialog } from '@/components/ui/alert-dialog';
import { api } from '@/lib/api';
import { useTranslation } from 'react-i18next';
import {
  staggerContainer,
  navItemVariants,
  springSnappy,
  springGentle,
} from '@/lib/motion';

interface NavItem {
  to: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  hint: string;
  requiresSSH?: boolean;
}

/** Check if a path matches the current location. */
function isPathActive(currentPath: string, itemPath: string): boolean {
  if (itemPath === '/') return currentPath === '/';
  return currentPath.startsWith(itemPath);
}

export default function Sidebar() {
  const { t } = useTranslation();
  const collapsed = useSettingsStore((s) => s.sidebarCollapsed);
  const toggle = useSettingsStore((s) => s.toggleSidebar);
  const server = useAuthStore((s) => s.server);
  const servers = useAuthStore((s) => s.servers);
  const activeServerId = useAuthStore((s) => s.activeServerId);
  const sshAvailable = useAuthStore((s) => s.sshAvailable);
  const selectServer = useAuthStore((s) => s.selectServer);
  const refreshServers = useAuthStore((s) => s.refreshServers);
  const navigate = useNavigate();
  const location = useLocation();

  const NAV: NavItem[] = [
    { to: '/', label: t('nav.dashboard'), icon: LayoutDashboard, hint: t('nav.dashboardHint') },
    { to: '/storage', label: t('nav.storage'), icon: HardDrive, hint: t('nav.storageHint') },
    { to: '/files', label: t('nav.files'), icon: FolderTree, hint: t('nav.filesHint'), requiresSSH: true },
    { to: '/terminal', label: t('nav.terminal'), icon: TerminalSquare, hint: t('nav.terminalHint'), requiresSSH: true },
    { to: '/vms', label: t('nav.vms'), icon: Cpu, hint: t('nav.vmsHint') },
    { to: '/docker', label: t('nav.docker'), icon: Container, hint: t('nav.dockerHint') },
    { to: '/settings', label: t('nav.settings'), icon: Settings, hint: t('nav.settingsHint') },
  ];

  const handleReconnect = async (id: string) => {
    try {
      await api.post(`/servers/${encodeURIComponent(id)}/reconnect`);
      await refreshServers();
      selectServer(id);
    } catch {
      // Error will be shown in the server list status
    }
  };

  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

  const handleDelete = async (id: string) => {
    try {
      await api.delete(`/servers/${encodeURIComponent(id)}`);
      await refreshServers();
      if (activeServerId === id) {
        const remaining = servers.filter((s) => s.id !== id);
        if (remaining.length > 0) {
          selectServer(remaining[0].id);
        } else {
          selectServer('');
          navigate('/onboarding', { replace: true });
        }
      }
    } catch {
      // ignore
    } finally {
      setDeleteTarget(null);
    }
  };

  return (
    <motion.aside
      className="relative z-10 flex h-full flex-col border-r border-border/40 bg-card/50 backdrop-blur-2xl"
      animate={{ width: collapsed ? 68 : 248 }}
      transition={springGentle}
    >
      {/* Logo */}
      <div className="flex h-16 items-center gap-3 border-b border-border/40 px-4">
        <motion.div
          className="grid h-9 w-9 shrink-0 place-items-center rounded-xl bg-gradient-to-br from-primary via-primary to-orange-600 text-sm font-bold text-primary-foreground shadow-lg shadow-primary/30"
          whileHover={{ scale: 1.06, rotate: -3 }}
          whileTap={{ scale: 0.94 }}
          transition={springSnappy}
        >
          U+
        </motion.div>
        <AnimatePresence>
          {!collapsed && (
            <motion.div
              className="flex flex-col leading-tight overflow-hidden"
              initial={{ opacity: 0, x: -8 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: -8 }}
              transition={springSnappy}
            >
              <span className="text-sm font-semibold tracking-tight">unraid-plus</span>
              <span className="text-[10px] text-muted-foreground tracking-wide">
                NAS manager
              </span>
            </motion.div>
          )}
        </AnimatePresence>
      </div>

      {/* Server list */}
      <AnimatePresence>
        {!collapsed && (
          <motion.div
            className="mx-3 mt-3 space-y-1"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
          >
            <div className="flex items-center justify-between px-1">
              <span className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground/70">
                {t('sidebar.server')}
              </span>
              <motion.button
                onClick={() => navigate('/onboarding?mode=add')}
                className="rounded-md p-1 text-muted-foreground/70 hover:bg-accent hover:text-accent-foreground transition-colors"
                title={t('sidebar.addServer')}
                whileHover={{ scale: 1.1 }}
                whileTap={{ scale: 0.9 }}
              >
                <Plus className="h-3.5 w-3.5" />
              </motion.button>
            </div>
            {servers.map((s) => {
              const online = !!(s.connected || s.sshAvailable || s.apiAvailable);
              const mode =
                s.sshAvailable && s.apiAvailable ? t('connection.dual') :
                s.apiAvailable ? t('connection.api') :
                s.sshAvailable ? t('connection.ssh') : t('connection.disconnected');
              return (
              <motion.div
                key={s.id}
                className={cn(
                  'group flex items-center gap-2 rounded-lg border px-2.5 py-2 text-sm cursor-pointer transition-all duration-200',
                  activeServerId === s.id
                    ? 'border-primary/20 bg-primary/5 text-foreground'
                    : 'border-transparent hover:bg-accent/50 text-muted-foreground',
                )}
                onClick={() => selectServer(s.id)}
                whileHover={{ x: 2 }}
                whileTap={{ scale: 0.98 }}
              >
                {online ? (
                  <Wifi className={cn(
                    'h-3 w-3 shrink-0',
                    s.sshAvailable && s.apiAvailable ? 'text-emerald-500' :
                    s.apiAvailable ? 'text-amber-500' : 'text-sky-500',
                  )} />
                ) : (
                  <WifiOff className="h-3 w-3 shrink-0 text-muted-foreground/50" />
                )}
                <div className="min-w-0 flex-1">
                  <div className="truncate text-xs font-medium">
                    {s.label || s.host}
                  </div>
                  <div className="truncate text-[10px] text-muted-foreground/60">
                    {mode}
                  </div>
                </div>
                {/* Action buttons (shown on hover) */}
                <div className="hidden gap-0.5 group-hover:flex">
                  {!online && (
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        handleReconnect(s.id);
                      }}
                      className="rounded p-0.5 hover:bg-accent"
                      title={t('sidebar.reconnect')}
                    >
                      <RefreshCw className="h-3 w-3" />
                    </button>
                  )}
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      setDeleteTarget(s.id);
                    }}
                    className="rounded p-0.5 hover:bg-destructive/10 hover:text-destructive"
                    title={t('sidebar.delete')}
                  >
                    <Trash2 className="h-3 w-3" />
                  </button>
                </div>
              </motion.div>
            );})}
            {servers.length === 0 && (
              <div className="px-2 py-3 text-center text-[10px] text-muted-foreground/60">
                {t('sidebar.noServer')}
              </div>
            )}
          </motion.div>
        )}
      </AnimatePresence>

      {/* Collapsed: server initial */}
      {collapsed && server && (
        <Tooltip>
          <TooltipTrigger asChild>
            <motion.div
              className="mx-auto mt-3 flex h-9 w-9 items-center justify-center rounded-xl border border-border/50 bg-card text-xs font-bold cursor-pointer hover:border-primary/30 transition-colors"
              whileHover={{ scale: 1.08 }}
              whileTap={{ scale: 0.95 }}
            >
              {(server.label || server.host)[0]?.toUpperCase()}
            </motion.div>
          </TooltipTrigger>
          <TooltipContent side="right">
            {server.label || server.host}
          </TooltipContent>
        </Tooltip>
      )}

      {/* Divider */}
      <div className="mx-3 my-3 h-px bg-border/40" />

      {/* Nav */}
      <motion.nav
        className="flex-1 overflow-y-auto px-2.5"
        variants={staggerContainer}
        initial="hidden"
        animate="visible"
      >
        <ul className="space-y-0.5">
          {NAV.filter((item) => !item.requiresSSH || sshAvailable).map((item) => {
            const Icon = item.icon;
            const active = isPathActive(location.pathname, item.to);
            const link = (
              <NavLink
                key={item.to}
                to={item.to}
                className={cn(
                  'relative flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium transition-all duration-200',
                  collapsed && 'justify-center px-0',
                  active
                    ? 'bg-primary/10 text-primary'
                    : 'text-muted-foreground hover:bg-accent/60 hover:text-accent-foreground',
                )}
              >
                {active && (
                  <motion.div
                    className="absolute inset-0 rounded-xl bg-primary/8"
                    layoutId="nav-active"
                    transition={springSnappy}
                  />
                )}
                <Icon className="h-[18px] w-[18px] shrink-0 relative z-10" />
                {!collapsed && (
                  <span className="relative z-10">{item.label}</span>
                )}
                {/* Active indicator dot */}
                {active && !collapsed && (
                  <motion.div
                    className="absolute right-3 h-1.5 w-1.5 rounded-full bg-primary"
                    layoutId="nav-dot"
                    transition={springSnappy}
                  />
                )}
              </NavLink>
            );
            return (
              <motion.li key={item.to} variants={navItemVariants}>
                {collapsed ? (
                  <Tooltip>
                    <TooltipTrigger asChild>{link}</TooltipTrigger>
                    <TooltipContent side="right">{item.hint}</TooltipContent>
                  </Tooltip>
                ) : (
                  link
                )}
              </motion.li>
            );
          })}
        </ul>
      </motion.nav>

      {/* Footer: collapse toggle */}
      <div className="border-t border-border/40 p-2.5">
        <motion.button
          onClick={toggle}
          className={cn(
            'flex w-full items-center gap-3 rounded-xl px-3 py-2 text-sm text-muted-foreground hover:bg-accent/60 hover:text-accent-foreground transition-colors',
            collapsed && 'justify-center',
          )}
          whileHover={{ x: collapsed ? 0 : 2 }}
          whileTap={{ scale: 0.97 }}
        >
          <motion.div
            animate={{ rotate: collapsed ? 180 : 0 }}
            transition={springSnappy}
          >
            <ChevronLeft className="h-4 w-4" />
          </motion.div>
          {!collapsed && (
            <span className="text-xs font-medium">{t('sidebar.collapseSidebar')}</span>
          )}
        </motion.button>
      </div>

      <ConfirmDialog
        open={!!deleteTarget}
        title={t('sidebar.confirmDeleteTitle')}
        description={t('sidebar.confirmDeleteDesc')}
        confirmText={t('common.delete')}
        variant="destructive"
        onConfirm={() => deleteTarget && handleDelete(deleteTarget)}
        onCancel={() => setDeleteTarget(null)}
      />
    </motion.aside>
  );
}

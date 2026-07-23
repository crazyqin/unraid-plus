import { useState } from 'react';
import { NavLink, useLocation, useNavigate } from 'react-router-dom';
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
    <aside
      className={cn(
        'flex h-full flex-col border-r bg-card transition-[width] duration-200',
        collapsed ? 'w-[68px]' : 'w-[240px]',
      )}
    >
      {/* Logo */}
      <div className="flex h-14 items-center gap-2 border-b px-4">
        <div className="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-primary text-primary-foreground font-bold">
          U+
        </div>
        {!collapsed && (
          <div className="flex flex-col leading-tight">
            <span className="text-sm font-semibold">unraid-plus</span>
            <span className="text-[10px] text-muted-foreground">
              friendlier NAS manager
            </span>
          </div>
        )}
      </div>

      {/* Server list */}
      {!collapsed && (
        <div className="mx-3 mt-3 space-y-1">
          <div className="flex items-center justify-between px-1">
            <span className="text-[10px] font-medium uppercase text-muted-foreground">
              {t('sidebar.server')}
            </span>
            <button
              onClick={() => navigate('/onboarding?mode=add')}
              className="rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              title={t('sidebar.addServer')}
            >
              <Plus className="h-3 w-3" />
            </button>
          </div>
          {servers.map((s) => (
            <div
              key={s.id}
              className={cn(
                'group flex items-center gap-2 rounded-md border px-2 py-1.5 text-sm cursor-pointer transition-colors',
                activeServerId === s.id
                  ? 'border-primary/30 bg-primary/5 text-foreground'
                  : 'border-transparent hover:bg-accent text-muted-foreground',
              )}
              onClick={() => {
                selectServer(s.id);
              }}
            >
              {s.connected ? (
                <Wifi className="h-3 w-3 shrink-0 text-emerald-500" />
              ) : (
                <WifiOff className="h-3 w-3 shrink-0 text-muted-foreground" />
              )}
              <div className="min-w-0 flex-1">
                <div className="truncate text-xs font-medium">
                  {s.label || s.host}
                </div>
                <div className="truncate text-[10px] text-muted-foreground">
                  {s.host}:{s.port}
                </div>
              </div>
              {/* Action buttons (shown on hover) */}
              <div className="hidden gap-0.5 group-hover:flex">
                {!s.connected && (
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
            </div>
          ))}
          {servers.length === 0 && (
            <div className="px-2 py-2 text-center text-[10px] text-muted-foreground">
              {t('sidebar.noServer')}
            </div>
          )}
        </div>
      )}

      {/* Collapsed: just show a server icon */}
      {collapsed && server && (
        <Tooltip>
          <TooltipTrigger asChild>
            <div className="mx-auto mt-3 flex h-8 w-8 items-center justify-center rounded-md border text-xs font-medium">
              {(server.label || server.host)[0]?.toUpperCase()}
            </div>
          </TooltipTrigger>
          <TooltipContent side="right">
            {server.label || server.host}
          </TooltipContent>
        </Tooltip>
      )}

      {/* Divider between server list and nav */}
      <div className="mx-3 my-2 border-b" />

      {/* Nav */}
      <nav className="flex-1 overflow-y-auto p-2">
        <ul className="space-y-1">
          {NAV.filter((item) => !item.requiresSSH || sshAvailable).map((item) => {
            const Icon = item.icon;
            const active = isPathActive(location.pathname, item.to);
            const link = (
              <NavLink
                key={item.to}
                to={item.to}
                className={cn(
                  'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
                  collapsed && 'justify-center',
                  active
                    ? 'bg-primary/10 text-primary'
                    : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground',
                )}
              >
                <Icon className="h-4 w-4 shrink-0" />
                {!collapsed && <span>{item.label}</span>}
              </NavLink>
            );
            return (
              <li key={item.to}>
                {collapsed ? (
                  <Tooltip>
                    <TooltipTrigger asChild>{link}</TooltipTrigger>
                    <TooltipContent side="right">{item.hint}</TooltipContent>
                  </Tooltip>
                ) : (
                  link
                )}
              </li>
            );
          })}
        </ul>
      </nav>

      {/* Footer: collapse */}
      <div className="border-t p-2">
        <button
          onClick={toggle}
          className={cn(
            'flex w-full items-center gap-3 rounded-md px-3 py-2 text-sm text-muted-foreground hover:bg-accent hover:text-accent-foreground',
            collapsed && 'justify-center',
          )}
        >
          <ChevronLeft
            className={cn('h-4 w-4 transition-transform', collapsed && 'rotate-180')}
          />
          {!collapsed && <span>{t('sidebar.collapseSidebar')}</span>}
        </button>
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
    </aside>
  );
}

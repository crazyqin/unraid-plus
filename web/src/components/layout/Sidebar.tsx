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
import { api } from '@/lib/api';

interface NavItem {
  to: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  hint: string;
}

const NAV: NavItem[] = [
  { to: '/', label: '仪表盘', icon: LayoutDashboard, hint: '服务器 CPU、内存、网络、磁盘的整体状态' },
  { to: '/storage', label: '存储', icon: HardDrive, hint: '磁盘阵列与缓存盘的容量、温度、健康' },
  { to: '/files', label: '文件', icon: FolderTree, hint: '基于 SFTP 的文件管理，可上传/下载/在线预览' },
  { to: '/terminal', label: '终端', icon: TerminalSquare, hint: '浏览器内的 SSH 命令行' },
  { to: '/vms', label: '虚拟机', icon: Cpu, hint: 'Unraid 上的 KVM 虚拟机启停' },
  { to: '/docker', label: 'Docker', icon: Container, hint: 'Docker 容器列表、启停与日志' },
  { to: '/settings', label: '设置', icon: Settings, hint: '连接配置、安全选项、界面偏好' },
];

/** Check if a path matches the current location. */
function isPathActive(currentPath: string, itemPath: string): boolean {
  if (itemPath === '/') return currentPath === '/';
  return currentPath.startsWith(itemPath);
}

export default function Sidebar() {
  const collapsed = useSettingsStore((s) => s.sidebarCollapsed);
  const toggle = useSettingsStore((s) => s.toggleSidebar);
  const showHelpers = useSettingsStore((s) => s.showHelpers);
  const server = useAuthStore((s) => s.server);
  const servers = useAuthStore((s) => s.servers);
  const activeServerId = useAuthStore((s) => s.activeServerId);
  const selectServer = useAuthStore((s) => s.selectServer);
  const refreshServers = useAuthStore((s) => s.refreshServers);
  const navigate = useNavigate();
  const location = useLocation();

  const handleReconnect = async (id: string) => {
    try {
      await api.post(`/servers/${encodeURIComponent(id)}/reconnect`);
      await refreshServers();
      selectServer(id);
    } catch {
      // Error will be shown in the server list status
    }
  };

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
              服务器
            </span>
            <button
              onClick={() => navigate('/onboarding?mode=add')}
              className="rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              title="添加服务器"
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
                    title="重连"
                  >
                    <RefreshCw className="h-3 w-3" />
                  </button>
                )}
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    handleDelete(s.id);
                  }}
                  className="rounded p-0.5 hover:bg-destructive/10 hover:text-destructive"
                  title="删除"
                >
                  <Trash2 className="h-3 w-3" />
                </button>
              </div>
            </div>
          ))}
          {servers.length === 0 && (
            <div className="px-2 py-2 text-center text-[10px] text-muted-foreground">
              尚未添加服务器
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
          {NAV.map((item) => {
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
                {collapsed || !showHelpers ? (
                  link
                ) : (
                  <Tooltip>
                    <TooltipTrigger asChild>{link}</TooltipTrigger>
                    <TooltipContent side="right">{item.hint}</TooltipContent>
                  </Tooltip>
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
          {!collapsed && <span>收起侧栏</span>}
        </button>
      </div>
    </aside>
  );
}

import { NavLink } from 'react-router-dom';
import {
  LayoutDashboard,
  Boxes,
  HardDrive,
  FolderTree,
  TerminalSquare,
  Cpu,
  Settings,
  ChevronLeft,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { useSettingsStore } from '@/stores/settings';
import { useAuthStore } from '@/stores/auth';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';

interface NavItem {
  to: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  /** A short glossary shown when "showHelpers" is on. */
  hint: string;
}

const NAV: NavItem[] = [
  { to: '/', label: '仪表盘', icon: LayoutDashboard, hint: '服务器 CPU、内存、网络、磁盘的整体状态' },
  { to: '/docker', label: 'Docker', icon: Boxes, hint: '运行在 Unraid 上的应用容器（如 Jellyfin、qBittorrent）' },
  { to: '/storage', label: '存储', icon: HardDrive, hint: '磁盘阵列与缓存盘的容量、温度、健康' },
  { to: '/files', label: '文件', icon: FolderTree, hint: '基于 SFTP 的文件管理，可上传/下载/在线预览' },
  { to: '/terminal', label: '终端', icon: TerminalSquare, hint: '浏览器内的 SSH 命令行' },
  { to: '/vms', label: '虚拟机', icon: Cpu, hint: 'Unraid 上的 KVM 虚拟机启停' },
  { to: '/settings', label: '设置', icon: Settings, hint: '连接配置、安全选项、界面偏好' },
];

export default function Sidebar() {
  const collapsed = useSettingsStore((s) => s.sidebarCollapsed);
  const toggle = useSettingsStore((s) => s.toggleSidebar);
  const showHelpers = useSettingsStore((s) => s.showHelpers);
  const server = useAuthStore((s) => s.server);

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
          D+
        </div>
        {!collapsed && (
          <div className="flex flex-col leading-tight">
            <span className="text-sm font-semibold">unraid++</span>
            <span className="text-[10px] text-muted-foreground">
              friendlier NAS manager
            </span>
          </div>
        )}
      </div>

      {/* Server chip */}
      {server && !collapsed && (
        <div className="mx-3 mt-3 rounded-md border bg-muted/40 px-3 py-2">
          <div className="truncate text-xs text-muted-foreground">已连接</div>
          <div className="truncate text-sm font-medium">
            {server.label || server.host}
          </div>
        </div>
      )}

      {/* Nav */}
      <nav className="flex-1 overflow-y-auto p-2">
        <ul className="space-y-1">
          {NAV.map((item) => {
            const Icon = item.icon;
            const link = (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.to === '/'}
                className={({ isActive }) =>
                  cn(
                    'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
                    collapsed && 'justify-center',
                    isActive
                      ? 'bg-primary/10 text-primary'
                      : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground',
                  )
                }
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

      {/* Footer: collapse + help */}
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

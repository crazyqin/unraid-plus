import { useNavigate } from 'react-router-dom';
import {
  HelpCircle,
  KeyRound,
  LogOut,
  RefreshCw,
  Server,
  Shield,
} from 'lucide-react';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';
import { Badge } from '@/components/ui/badge';
import { Separator } from '@/components/ui/separator';
import { useAuthStore } from '@/stores/auth';
import { useSettingsStore } from '@/stores/settings';
import { api } from '@/lib/api';

export default function SettingsPage() {
  const server = useAuthStore((s) => s.server);
  const reset = useAuthStore((s) => s.reset);
  const navigate = useNavigate();

  const {
    showHelpers,
    toggleHelpers,
    refreshInterval,
    setRefreshInterval,
    onboardingDone,
    setOnboardingDone,
  } = useSettingsStore();

  const disconnect = async () => {
    if (!confirm('断开当前服务器连接？需要重新走一遍连接向导。')) return;
    try {
      await api.post('/disconnect');
    } catch {
      /* ignore — backend may already be gone */
    }
    reset();
    navigate('/onboarding', { replace: true });
  };

  const rotateKey = async () => {
    if (
      !confirm(
        '生成新的密钥对并部署到 Unraid？此操作会替换服务器上现有的 authorized_keys。',
      )
    )
      return;
    await api.post('/auth/rotate-key');
  };

  return (
    <div className="space-y-4 p-4 md:p-6">
      <div>
        <h1 className="text-xl font-semibold">设置</h1>
        <p className="text-sm text-muted-foreground">连接、安全、界面偏好</p>
      </div>

      {/* Connection */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <Server className="h-4 w-4" /> 当前连接
          </CardTitle>
          <CardDescription>这台 unraid++ 正在管理的服务器</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          <Row label="昵称 / 主机" value={server ? `${server.label} · ${server.host}` : '—'} />
          <Row label="SSH 端口" value={server ? String(server.sshPort) : '—'} />
          <Row label="用户" value={server?.user ?? '—'} />
          <Row
            label="认证模式"
            value={
              server ? (
                <Badge variant={server.authMode === 'key' ? 'success' : 'warning'}>
                  {server.authMode === 'key' ? '密钥对免密' : '密码（建议切到密钥）'}
                </Badge>
              ) : (
                '—'
              )
            }
          />
          <Separator />
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" size="sm" onClick={() => location.reload()}>
              <RefreshCw className="h-3.5 w-3.5" /> 刷新页面
            </Button>
            <Button variant="destructive" size="sm" onClick={disconnect}>
              <LogOut className="h-3.5 w-3.5" /> 断开连接
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* Security */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <Shield className="h-4 w-4" /> 安全
          </CardTitle>
          <CardDescription>
            切到「密钥对免密」模式后，root 密码完全不再被使用。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          <div className="flex items-center justify-between gap-2">
            <div>
              <div className="font-medium">密钥对免密模式</div>
              <div className="text-xs text-muted-foreground">
                后端自动生成并托管 SSH 密钥对，root 密码不出现在内存之外的任何位置。
              </div>
            </div>
            <Button variant="outline" size="sm" onClick={rotateKey}>
              <KeyRound className="h-3.5 w-3.5" /> 生成 / 轮换密钥
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* UI / preferences */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <HelpCircle className="h-4 w-4" /> 界面与引导
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 text-sm">
          <ToggleRow
            label="显示帮助提示"
            desc="在导航和重要字段旁显示术语解释气泡。"
            checked={showHelpers}
            onChange={(v) => toggleHelpers(v)}
          />
          <ToggleRow
            label="已完成新手引导"
            desc="关闭后将强制再次显示欢迎向导。"
            checked={onboardingDone}
            onChange={(v) => setOnboardingDone(v)}
          />
          <Separator />
          <div className="flex items-center justify-between gap-2">
            <div>
              <div className="font-medium">自动刷新间隔</div>
              <div className="text-xs text-muted-foreground">
                影响仪表盘、Docker、存储等实时数据。
              </div>
            </div>
            <select
              className="rounded border bg-background px-2 py-1 text-sm"
              value={refreshInterval}
              onChange={(e) => setRefreshInterval(Number(e.target.value))}
            >
              <option value={1000}>1 秒</option>
              <option value={2000}>2 秒</option>
              <option value={5000}>5 秒</option>
              <option value={15000}>15 秒</option>
              <option value={0}>暂停</option>
            </select>
          </div>
        </CardContent>
      </Card>

      <p className="text-center text-xs text-muted-foreground">
        unraid++ · Apache 2.0 · 完全开源
      </p>
    </div>
  );
}

function Row({
  label,
  value,
}: {
  label: string;
  value: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-muted-foreground">{label}</span>
      <span>{value}</span>
    </div>
  );
}

function ToggleRow({
  label,
  desc,
  checked,
  onChange,
}: {
  label: string;
  desc: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-2">
      <div>
        <div className="font-medium">{label}</div>
        <div className="text-xs text-muted-foreground">{desc}</div>
      </div>
      <Switch checked={checked} onCheckedChange={onChange} />
    </div>
  );
}

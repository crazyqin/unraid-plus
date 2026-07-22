import { useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import {
  ArrowLeft,
  ArrowRight,
  CheckCircle2,
  Key,
  Loader2,
  Lock,
  Server,
  Sparkles,
  ShieldCheck,
  Terminal,
  Upload,
  Wifi,
} from 'lucide-react';
import { api, ApiError } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { useAuthStore } from '@/stores/auth';
import { useSettingsStore } from '@/stores/settings';
import { useOnboardingStore } from '@/stores/onboarding';
import type { ConnectResult, ServerConfig } from '@/types';

type Skill = 'novice' | 'intermediate' | 'expert';
type AuthMode = 'password' | 'key';

const STEPS = ['欢迎', '熟悉度', '连接', '验证', '安全', '完成'] as const;

/** Step index for the "连接" step — used to skip ahead when re-entering from sidebar "+" */
const CONNECT_STEP = 2;

export default function OnboardingPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const configure = useAuthStore((s) => s.configure);
  const uiAuthEnabled = useAuthStore((s) => s.uiAuthEnabled);
  const setOnboardingDone = useSettingsStore((s) => s.setOnboardingDone);
  const skill = useOnboardingStore((s) => s.skill);
  const setSkill = useOnboardingStore((s) => s.setSkill);

  // If navigated here with ?mode=add (from sidebar "+"), jump straight to connect step
  const isAddMode = new URLSearchParams(location.search).get('mode') === 'add';
  const [step, setStep] = useState<number>(isAddMode ? CONNECT_STEP : 0);

  // form
  const [host, setHost] = useState('192.168.1.99');
  const [apiBase, setApiBase] = useState('');
  const [sshPort, setSshPort] = useState(22);
  const [user, setUser] = useState('root');
  const [password, setPassword] = useState('');
  const [authMode, setAuthMode] = useState<AuthMode>('password');
  const [privateKey, setPrivateKey] = useState<string>('');
  const [keyFileName, setKeyFileName] = useState('');
  const [label, setLabel] = useState('');

  // connect state
  const [connecting, setConnecting] = useState(false);
  const [result, setResult] = useState<ConnectResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  const next = () => setStep((s) => Math.min(STEPS.length - 1, s + 1));
  const prev = () => setStep((s) => Math.max(0, s - 1));

  const handleConnect = async () => {
    setError(null);
    setResult(null);
    setConnecting(true);
    try {
      const body: Record<string, unknown> = {
        host,
        apiBase: apiBase || undefined,
        sshPort,
        user,
      };
      if (authMode === 'password') {
        body.password = password;
      } else {
        if (!privateKey) {
          setError('请选择或粘贴私钥文件');
          setConnecting(false);
          return;
        }
        body.privateKey = btoa(privateKey); // base64 encode for transport
      }
      const r = await api.post<ConnectResult>('/connect', body);
      setResult(r);
      if (r.ok) next();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : '连接失败，请检查参数');
    } finally {
      setConnecting(false);
    }
  };

  const handleFinish = () => {
    const cfg: ServerConfig = {
      host,
      apiBase: apiBase || undefined,
      sshPort,
      user,
      authMode,
      status: 'connected',
      label: label || host,
      id: result?.serverId,
    };
    configure(cfg);
    setOnboardingDone(true);
    // Refresh server list from backend
    useAuthStore.getState().refreshServers();
    navigate('/', { replace: true });
  };

  return (
    <div className="grid min-h-screen grid-rows-[auto_1fr_auto] bg-background">
      {/* Header */}
      <header className="flex items-center gap-3 border-b px-6 py-4">
        <div className="grid h-9 w-9 place-items-center rounded-md bg-primary text-primary-foreground font-bold">
          U+
        </div>
        <div className="leading-tight">
          <div className="text-base font-semibold">unraid-plus 初始化</div>
          <div className="text-xs text-muted-foreground">
            三分钟把你的 Unraid 接入管理
          </div>
        </div>
      </header>

      {/* Step content */}
      <main className="mx-auto flex w-full max-w-3xl flex-col justify-center px-6 py-8">
        <Stepper current={step} />

        {step === 0 && <WelcomeStep onNext={next} />}
        {step === 1 && (
          <SkillStep
            skill={skill}
            onChange={setSkill}
            onNext={next}
            onPrev={prev}
          />
        )}
        {step === 2 && (
          <ConnectStep
            host={host}
            apiBase={apiBase}
            sshPort={sshPort}
            user={user}
            password={password}
            authMode={authMode}
            privateKey={privateKey}
            keyFileName={keyFileName}
            label={label}
            skill={skill}
            connecting={connecting}
            error={error}
            onHost={setHost}
            onApiBase={setApiBase}
            onSshPort={setSshPort}
            onUser={setUser}
            onPassword={setPassword}
            onAuthMode={setAuthMode}
            onPrivateKey={setPrivateKey}
            onKeyFileName={setKeyFileName}
            onLabel={setLabel}
            onConnect={handleConnect}
            onPrev={prev}
          />
        )}
        {step === 3 && (
          <VerifyStep
            result={result}
            host={host}
            onPrev={prev}
            onNext={next}
          />
        )}
        {step === 4 && (
          <SecurityStep
            uiAuthEnabled={uiAuthEnabled}
            onNext={next}
            onPrev={prev}
          />
        )}
        {step === 5 && <DoneStep host={host} onFinish={handleFinish} />}
      </main>

      <footer className="border-t px-6 py-3 text-center text-xs text-muted-foreground">
        Apache 2.0 · 完全开源 · 你的密码仅在后端内存中短暂使用
      </footer>
    </div>
  );
}

/* ---------------------------------- UI ----------------------------------- */

function Stepper({ current }: { current: number }) {
  return (
    <div className="mb-8 flex items-center justify-between">
      {STEPS.map((label, i) => (
        <div key={label} className="flex flex-1 items-center">
          <div
            className={cn(
              'flex h-8 w-8 shrink-0 items-center justify-center rounded-full border text-xs font-medium',
              i < current && 'border-primary bg-primary text-primary-foreground',
              i === current && 'border-primary bg-primary/10 text-primary',
              i > current && 'border-muted text-muted-foreground',
            )}
          >
            {i < current ? <CheckCircle2 className="h-4 w-4" /> : i + 1}
          </div>
          <span
            className={cn(
              'ml-2 text-xs',
              i === current ? 'text-foreground' : 'text-muted-foreground',
            )}
          >
            {label}
          </span>
          {i < STEPS.length - 1 && (
            <div className="mx-3 h-px flex-1 bg-border" />
          )}
        </div>
      ))}
    </div>
  );
}

function WelcomeStep({ onNext }: { onNext: () => void }) {
  const features = [
    { icon: Server, title: '一站式监控', desc: 'CPU / 内存 / 磁盘 / 网络，图形化看板' },
    { icon: Terminal, title: '内置 SSH 终端', desc: '浏览器直连服务器命令行，免装客户端' },
    { icon: ShieldCheck, title: '本地直连', desc: '不经过任何云服务，流量不出局域网' },
    { icon: Sparkles, title: '新手友好', desc: '全部名词都有解释，第一次用也看得懂' },
  ];
  return (
    <div className="animate-fade-in space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">欢迎使用 unraid-plus</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          一个比官方 WebUI 更直观、比手机客户端更好部署的 Unraid 管理器。
          只需要你的 Unraid 局域网 IP 和 root 密码，三分钟接入。
        </p>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        {features.map((f) => (
          <Card key={f.title}>
            <CardContent className="flex items-start gap-3 p-4">
              <div className="grid h-9 w-9 place-items-center rounded-md bg-primary/10 text-primary">
                <f.icon className="h-4 w-4" />
              </div>
              <div>
                <div className="text-sm font-medium">{f.title}</div>
                <div className="text-xs text-muted-foreground">{f.desc}</div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
      <div className="flex justify-end">
        <Button onClick={onNext} size="lg">
          开始
          <ArrowRight className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}

function SkillStep({
  skill,
  onChange,
  onNext,
  onPrev,
}: {
  skill: Skill;
  onChange: (s: Skill) => void;
  onNext: () => void;
  onPrev: () => void;
}) {
  const options: { value: Skill; title: string; desc: string }[] = [
    {
      value: 'novice',
      title: '我是新手',
      desc: '我刚装好 Unraid，连 SSH 是什么都不太清楚',
    },
    {
      value: 'intermediate',
      title: '会用就行',
      desc: '玩过 Docker 和命令行，但不想折腾配置',
    },
    {
      value: 'expert',
      title: '我是老手',
      desc: '精通 Linux，给我最快的接入路径',
    },
  ];
  return (
    <div className="animate-fade-in space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">先简单了解一下你</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          我们会据此调整后续界面的提示详略，让说明既不啰嗦也不漏关键信息。
        </p>
      </div>
      <div className="space-y-2">
        {options.map((o) => (
          <button
            key={o.value}
            onClick={() => onChange(o.value)}
            className={cn(
              'flex w-full items-center justify-between rounded-lg border p-4 text-left transition-colors',
              skill === o.value
                ? 'border-primary bg-primary/5'
                : 'hover:bg-accent',
            )}
          >
            <div>
              <div className="text-sm font-medium">{o.title}</div>
              <div className="text-xs text-muted-foreground">{o.desc}</div>
            </div>
            <div
              className={cn(
                'grid h-5 w-5 place-items-center rounded-full border',
                skill === o.value
                  ? 'border-primary bg-primary text-primary-foreground'
                  : 'border-muted-foreground/40',
              )}
            >
              {skill === o.value && <CheckCircle2 className="h-3 w-3" />}
            </div>
          </button>
        ))}
      </div>
      <div className="flex justify-between">
        <Button variant="ghost" onClick={onPrev}>
          <ArrowLeft className="h-4 w-4" /> 上一步
        </Button>
        <Button onClick={onNext}>
          下一步 <ArrowRight className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}

function ConnectStep(props: {
  host: string;
  apiBase: string;
  sshPort: number;
  user: string;
  password: string;
  authMode: AuthMode;
  privateKey: string;
  keyFileName: string;
  label: string;
  skill: Skill;
  connecting: boolean;
  error: string | null;
  onHost: (v: string) => void;
  onApiBase: (v: string) => void;
  onSshPort: (v: number) => void;
  onUser: (v: string) => void;
  onPassword: (v: string) => void;
  onAuthMode: (v: AuthMode) => void;
  onPrivateKey: (v: string) => void;
  onKeyFileName: (v: string) => void;
  onLabel: (v: string) => void;
  onConnect: () => void;
  onPrev: () => void;
}) {
  const fileRef = useRef<HTMLInputElement>(null);
  const isExpert = props.skill === 'expert';

  const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    props.onKeyFileName(file.name);
    const reader = new FileReader();
    reader.onload = () => {
      props.onPrivateKey(reader.result as string);
    };
    reader.readAsText(file);
  };

  return (
    <div className="animate-fade-in space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">把 unraid-plus 连到你的 NAS</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          支持密码和密钥两种认证方式。密码仅在后端内存中用于本次配对，
          密钥模式下后端会保存私钥用于自动重连。
        </p>
      </div>

      <Card>
        <CardContent className="space-y-4 p-6">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field
              label="Unraid IP / 域名"
              hint="在路由器后台或 Unraid 主页右上角能看到。形如 192.168.x.x 或 tower.local"
              required
            >
              <Input
                value={props.host}
                onChange={(e) => props.onHost(e.target.value)}
                placeholder="192.168.1.99"
              />
            </Field>
            <Field
              label="SSH 端口"
              hint="Unraid 默认 22，没改过就不用动。"
            >
              <Input
                type="number"
                value={props.sshPort}
                onChange={(e) => props.onSshPort(Number(e.target.value))}
              />
            </Field>
          </div>

          {isExpert && (
            <Field
              label="Unraid API 地址"
              hint="可选。默认根据 IP 推断为 https://<host>。如果你的 Unraid 走反向代理，可在这里填。"
            >
              <Input
                value={props.apiBase}
                onChange={(e) => props.onApiBase(e.target.value)}
                placeholder="https://tower.local"
              />
            </Field>
          )}

          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="用户名" hint="Unraid 默认 root，绝大多数情况不要改。">
              <Input
                value={props.user}
                onChange={(e) => props.onUser(e.target.value)}
              />
            </Field>
            <Field label="服务器昵称（可选）" hint="好记的名字，比如「客厅NAS」「Tower」。">
              <Input
                value={props.label}
                onChange={(e) => props.onLabel(e.target.value)}
                placeholder="Tower"
              />
            </Field>
          </div>

          {/* Auth mode selector */}
          <div>
            <Label className="mb-2 block">认证方式</Label>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => props.onAuthMode('password')}
                className={cn(
                  'flex flex-1 items-center gap-2 rounded-lg border p-3 text-left transition-colors',
                  props.authMode === 'password'
                    ? 'border-primary bg-primary/5'
                    : 'hover:bg-accent',
                )}
              >
                <Lock className="h-4 w-4 shrink-0" />
                <div>
                  <div className="text-sm font-medium">密码认证</div>
                  <div className="text-[10px] text-muted-foreground">输入 root 密码连接</div>
                </div>
              </button>
              <button
                type="button"
                onClick={() => props.onAuthMode('key')}
                className={cn(
                  'flex flex-1 items-center gap-2 rounded-lg border p-3 text-left transition-colors',
                  props.authMode === 'key'
                    ? 'border-primary bg-primary/5'
                    : 'hover:bg-accent',
                )}
              >
                <Key className="h-4 w-4 shrink-0" />
                <div>
                  <div className="text-sm font-medium">密钥认证</div>
                  <div className="text-[10px] text-muted-foreground">使用 SSH 私钥连接</div>
                </div>
              </button>
            </div>
          </div>

          {/* Password or Key input based on auth mode */}
          {props.authMode === 'password' ? (
            <Field
              label="root 密码"
              hint="就是你在 Unraid WebUI 登录用的密码。"
              required
            >
              <Input
                type="password"
                value={props.password}
                onChange={(e) => props.onPassword(e.target.value)}
                autoComplete="current-password"
              />
            </Field>
          ) : (
            <div className="space-y-2">
              <Field label="SSH 私钥" hint="选择你的私钥文件（如 id_ed25519），或直接粘贴 PEM 内容。" required>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => fileRef.current?.click()}
                  >
                    <Upload className="mr-1 h-3 w-3" /> 选择文件
                  </Button>
                  {props.keyFileName && (
                    <span className="flex items-center text-xs text-muted-foreground">
                      {props.keyFileName}
                    </span>
                  )}
                  <input
                    ref={fileRef}
                    type="file"
                    className="hidden"
                    accept=".pem,.key,id_ed25519,id_rsa,id_ecdsa"
                    onChange={handleFileSelect}
                  />
                </div>
              </Field>
              <textarea
                className="w-full rounded-md border bg-muted/30 p-2 font-mono text-xs focus:outline-none focus:ring-1 focus:ring-primary"
                rows={4}
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;...&#10;-----END OPENSSH PRIVATE KEY-----"
                value={props.privateKey}
                onChange={(e) => {
                  props.onPrivateKey(e.target.value);
                  if (!props.keyFileName) props.onKeyFileName('(手动粘贴)');
                }}
              />
            </div>
          )}

          <div className="flex items-center gap-2 rounded-md border border-warning/30 bg-warning/10 p-3 text-xs text-warning-foreground">
            <Lock className="h-4 w-4 shrink-0 text-warning" />
            <span className="text-foreground/80">
              {props.authMode === 'password'
                ? '密码从浏览器到后端走 HTTPS（或局域网明文），仅在后端内存中短暂使用，不会写入磁盘。'
                : '私钥将保存在后端数据目录中用于自动重连，建议配对后用 RotateKey 生成专用密钥。'}
            </span>
          </div>
        </CardContent>
      </Card>

      {props.error && (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
          {props.error}
        </div>
      )}

      <div className="flex justify-between">
        <Button variant="ghost" onClick={props.onPrev}>
          <ArrowLeft className="h-4 w-4" /> 上一步
        </Button>
        <Button onClick={props.onConnect} disabled={props.connecting}>
          {props.connecting ? (
            <>
              <Loader2 className="h-4 w-4 animate-spin" /> 正在连接…
            </>
          ) : (
            <>
              <Wifi className="h-4 w-4" /> 测试并连接
            </>
          )}
        </Button>
      </div>
    </div>
  );
}

function VerifyStep({
  result,
  host,
  onNext,
  onPrev,
}: {
  result: ConnectResult | null;
  host: string;
  onNext: () => void;
  onPrev: () => void;
}) {
  return (
    <div className="animate-fade-in space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">连接验证</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          确认下方的服务器指纹是你的 Unraid，避免中间人攻击。
        </p>
      </div>

      <Card>
        <CardContent className="space-y-4 p-6">
          <div className="flex items-center gap-3">
            <CheckCircle2 className="h-8 w-8 text-success" />
            <div>
              <div className="text-base font-medium">连接成功</div>
              <div className="text-xs text-muted-foreground">
                已与 {host} 建立 SSH 会话
              </div>
            </div>
          </div>
          <div className="rounded-md bg-muted/40 p-3 text-sm">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">服务器指纹</span>
              <Badge variant="secondary" className="font-mono text-[10px]">
                {result?.hostFingerprint ?? 'N/A'}
              </Badge>
            </div>
            <div className="mt-2 flex items-center justify-between">
              <span className="text-muted-foreground">Unraid 版本</span>
              <span className="font-mono text-xs">
                {result?.serverVersion ?? 'unknown'}
              </span>
            </div>
          </div>
          <p className="text-xs text-muted-foreground">
            首次连接时浏览器/服务端会记住这个指纹。如果之后指纹变了，
            unraid-plus 会拒绝连接并提示你确认是否安全。
          </p>
        </CardContent>
      </Card>

      <div className="flex justify-between">
        <Button variant="ghost" onClick={onPrev}>
          <ArrowLeft className="h-4 w-4" /> 上一步
        </Button>
        <Button onClick={onNext}>
          确认无误 <ArrowRight className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}

function SecurityStep({
  uiAuthEnabled,
  onNext,
  onPrev,
}: {
  uiAuthEnabled: boolean;
  onNext: () => void;
  onPrev: () => void;
}) {
  const [uiPassword, setUiPassword] = useState('');
  const [setting, setSetting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // If password already set (e.g. via env var), skip the setup UI
  const alreadySecured = uiAuthEnabled;

  const handleSetup = async () => {
    if (!uiPassword) {
      // Skip — no password
      onNext();
      return;
    }
    if (uiPassword.length < 4) {
      setError('密码至少 4 位');
      return;
    }
    setSetting(true);
    setError(null);
    try {
      await api.post('/auth/setup', { password: uiPassword });
      onNext();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : '设置失败');
    } finally {
      setSetting(false);
    }
  };

  return (
    <div className="animate-fade-in space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">安全设置</h1>
        <p className="mt-1 text-sm text-muted-foreground">
            为 unraid-plus 网页界面设置一个访问密码，防止局域网内其他人随意操作你的 NAS。
        </p>
      </div>

      {alreadySecured ? (
        <Card>
          <CardContent className="flex items-center gap-3 p-6">
            <ShieldCheck className="h-8 w-8 text-success" />
            <div>
              <div className="text-base font-medium">访问密码已启用</div>
              <div className="text-xs text-muted-foreground">
                密码已通过环境变量配置，无需重复设置。
              </div>
            </div>
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardContent className="space-y-4 p-6">
            <div className="flex items-center gap-2 rounded-md border border-warning/30 bg-warning/10 p-3 text-xs text-warning-foreground">
              <Lock className="h-4 w-4 shrink-0 text-warning" />
              <span className="text-foreground/80">
                不设密码 = 局域网内任何人都能访问你的 unraid-plus，
                包括执行 SSH 命令、重启容器等高危操作。
              </span>
            </div>

            <Field
              label="界面访问密码"
              hint="留空跳过（后续可在设置中补设）。至少 4 位。"
            >
              <Input
                type="password"
                value={uiPassword}
                onChange={(e) => {
                  setUiPassword(e.target.value);
                  setError(null);
                }}
                placeholder="可选，留空跳过"
                autoComplete="new-password"
              />
            </Field>

            {error && (
              <div className="text-sm text-destructive">{error}</div>
            )}
          </CardContent>
        </Card>
      )}

      <div className="flex justify-between">
        <Button variant="ghost" onClick={onPrev}>
          <ArrowLeft className="h-4 w-4" /> 上一步
        </Button>
        <Button onClick={handleSetup} disabled={setting}>
          {setting ? (
            <>
              <Loader2 className="h-4 w-4 animate-spin" /> 设置中…
            </>
          ) : uiPassword || alreadySecured ? (
            <>
              下一步 <ArrowRight className="h-4 w-4" />
            </>
          ) : (
            '跳过，不设密码'
          )}
        </Button>
      </div>
    </div>
  );
}

function DoneStep({ host, onFinish }: { host: string; onFinish: () => void }) {
  return (
    <div className="animate-fade-in space-y-6 text-center">
      <div className="mx-auto grid h-16 w-16 place-items-center rounded-full bg-success/15 text-success">
        <CheckCircle2 className="h-8 w-8" />
      </div>
      <div>
        <h1 className="text-2xl font-semibold">完成！</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {host} 已接入。进入仪表盘看看你的服务器现在长什么样吧。
        </p>
      </div>
      <Button size="lg" onClick={onFinish}>
        进入仪表盘 <ArrowRight className="h-4 w-4" />
      </Button>
    </div>
  );
}

function Field({
  label,
  hint,
  required,
  children,
}: {
  label: string;
  hint?: string;
  required?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <Label className="flex items-center gap-1">
        {label}
        {required && <span className="text-destructive">*</span>}
      </Label>
      {children}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}

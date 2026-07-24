import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useLocation, useNavigate } from 'react-router-dom';
import { motion } from 'framer-motion';
import {
  ArrowLeft,
  ArrowRight,
  CheckCircle2,
  ChevronDown,
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
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { useAuthStore } from '@/stores/auth';
import { useSettingsStore } from '@/stores/settings';
import { useOnboardingStore } from '@/stores/onboarding';
import { springGentle } from '@/lib/motion';
import type { ConnectResult, ServerConfig } from '@/types';

type Skill = 'novice' | 'intermediate' | 'expert';
type AuthMode = 'password' | 'key';

const STEP_KEYS = [
  'onboarding.steps.welcome',
  'onboarding.steps.familiarity',
  'onboarding.steps.connect',
  'onboarding.steps.verify',
  'onboarding.steps.security',
  'onboarding.steps.done',
] as const;

const CONNECT_STEP = 2;

export default function OnboardingPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const configure = useAuthStore((s) => s.configure);
  const uiAuthEnabled = useAuthStore((s) => s.uiAuthEnabled);
  const hasServers = useAuthStore((s) => s.servers.length > 0);
  const setOnboardingDone = useSettingsStore((s) => s.setOnboardingDone);
  const skill = useOnboardingStore((s) => s.skill);
  const setSkill = useOnboardingStore((s) => s.setSkill);

  const isAddMode = new URLSearchParams(location.search).get('mode') === 'add';
  const [step, setStep] = useState<number>(isAddMode ? CONNECT_STEP : 0);

  const [host, setHost] = useState('');
  const [apiBase, setApiBase] = useState('tower.local');
  const [sshPort, setSshPort] = useState(22);
  const [user, setUser] = useState('root');
  const [password, setPassword] = useState('');
  const [authMode, setAuthMode] = useState<AuthMode>('password');
  const [privateKey, setPrivateKey] = useState<string>('');
  const [keyFileName, setKeyFileName] = useState('');
  const [label, setLabel] = useState('');
  const [showAdvanced, setShowAdvanced] = useState(false);

  const [connecting, setConnecting] = useState(false);
  const [result, setResult] = useState<ConnectResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  const STEPS = STEP_KEYS.map((k) => t(k));

  const next = () => setStep((s) => Math.min(STEPS.length - 1, s + 1));
  const prev = () => setStep((s) => Math.max(0, s - 1));

  const handleConnect = async () => {
    setError(null);
    setResult(null);
    setConnecting(true);
    try {
      const body: Record<string, unknown> = {
        apiBase: apiBase || undefined,
        host: host || undefined,
        sshPort,
        user,
      };
      if (authMode === 'password') {
        body.password = password;
      } else {
        if (!privateKey) {
          setError(t('onboarding.selectKeyFile'));
          setConnecting(false);
          return;
        }
        body.privateKey = btoa(privateKey);
      }
      const r = await api.post<ConnectResult>('/connect', body);
      setResult(r);
      if (r.ok) next();
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : t('onboarding.connectFailed');
      setError(msg);
      // Auto-expand advanced settings if SSH port issue is mentioned
      if (msg.includes('端口') || msg.includes('SSH')) {
        setShowAdvanced(true);
      }
    } finally {
      setConnecting(false);
    }
  };

  const handleFinish = () => {
    const cfg: ServerConfig = {
      host: host || apiBase,
      apiBase: apiBase || undefined,
      sshPort,
      user,
      authMode,
      status: 'connected',
      label: label || host || apiBase,
      id: result?.serverId,
    };
    configure(cfg);
    setOnboardingDone(true);
    useAuthStore.getState().refreshServers();
    navigate('/', { replace: true });
  };

  const handleCancel = () => {
    navigate('/', { replace: true });
  };

  return (
    <div className="grid min-h-screen grid-rows-[auto_1fr_auto] bg-background">
      {/* Header */}
      <header className="flex items-center justify-between border-b px-6 py-4">
        <div className="flex items-center gap-3">
          <div className="grid h-9 w-9 place-items-center rounded-xl bg-primary text-primary-foreground font-bold">
            U+
          </div>
          <div className="leading-tight">
            <div className="text-base font-semibold">{t('onboarding.title')}</div>
            <div className="text-xs text-muted-foreground">
              {t('onboarding.subtitle')}
            </div>
          </div>
        </div>
        {hasServers && (
          <Button variant="ghost" size="sm" className="rounded-lg" onClick={handleCancel}>
            <ArrowLeft className="h-4 w-4" /> {t('common.cancel')}
          </Button>
        )}
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
            showAdvanced={showAdvanced}
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
            onShowAdvanced={setShowAdvanced}
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
        {t('onboarding.openSource')}
      </footer>
    </div>
  );
}

/* ---------------------------------- UI ----------------------------------- */

function Stepper({ current }: { current: number }) {
  const { t } = useTranslation();
  const STEPS = STEP_KEYS.map((k) => t(k));
  return (
    <div className="mb-8 flex items-center justify-between">
      {STEPS.map((label, i) => (
        <div key={STEP_KEYS[i]} className="flex flex-1 items-center">
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
  const { t } = useTranslation();
  const features = [
    { icon: Server, title: t('onboarding.feature1Title'), desc: t('onboarding.feature1Desc') },
    { icon: Terminal, title: t('onboarding.feature2Title'), desc: t('onboarding.feature2Desc') },
    { icon: ShieldCheck, title: t('onboarding.feature3Title'), desc: t('onboarding.feature3Desc') },
    { icon: Sparkles, title: t('onboarding.feature4Title'), desc: t('onboarding.feature4Desc') },
  ];
  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={springGentle}
      className="space-y-6"
    >
      <div>
        <h1 className="text-display-md text-foreground">{t('onboarding.welcomeTitle')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {t('onboarding.welcomeDesc1')}
          {t('onboarding.welcomeDesc2')}
        </p>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        {features.map((f) => (
          <div key={f.title} className="card-bento">
            <div className="flex items-start gap-3 p-4">
              <div className="grid h-9 w-9 place-items-center rounded-xl bg-primary/10 text-primary">
                <f.icon className="h-4 w-4" />
              </div>
              <div>
                <div className="text-sm font-medium">{f.title}</div>
                <div className="text-xs text-muted-foreground">{f.desc}</div>
              </div>
            </div>
          </div>
        ))}
      </div>
      <div className="flex justify-end">
        <Button onClick={onNext} size="lg" className="rounded-lg">
          {t('onboarding.start')}
          <ArrowRight className="h-4 w-4" />
        </Button>
      </div>
    </motion.div>
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
  const { t } = useTranslation();
  const options: { value: Skill; title: string; desc: string }[] = [
    {
      value: 'novice',
      title: t('onboarding.beginner'),
      desc: t('onboarding.beginnerDesc'),
    },
    {
      value: 'intermediate',
      title: t('onboarding.casual'),
      desc: t('onboarding.casualDesc'),
    },
    {
      value: 'expert',
      title: t('onboarding.expert'),
      desc: t('onboarding.expertDesc'),
    },
  ];
  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={springGentle}
      className="space-y-6"
    >
      <div>
        <h1 className="text-display-md text-foreground">{t('onboarding.familiarityTitle')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {t('onboarding.familiarityDesc')}
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
        <Button variant="ghost" className="rounded-lg" onClick={onPrev}>
          <ArrowLeft className="h-4 w-4" /> {t('common.back')}
        </Button>
        <Button className="rounded-lg" onClick={onNext}>
          {t('common.next')} <ArrowRight className="h-4 w-4" />
        </Button>
      </div>
    </motion.div>
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
  showAdvanced: boolean;
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
  onShowAdvanced: (v: boolean) => void;
  onConnect: () => void;
  onPrev: () => void;
}) {
  const { t } = useTranslation();
  const fileRef = useRef<HTMLInputElement>(null);

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
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={springGentle}
      className="space-y-6"
    >
      <div>
        <h1 className="text-display-md text-foreground">{t('onboarding.connectTitle')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {t('onboarding.connectDesc')}
        </p>
      </div>

      <div className="card-bento">
        <div className="space-y-5 p-6">
          <Field
            label={t('onboarding.serverAddress')}
            hint={t('onboarding.serverAddressHint')}
            required
          >
            <Input
              value={props.apiBase}
              onChange={(e) => props.onApiBase(e.target.value)}
              placeholder={t('onboarding.serverPlaceholder')}
            />
          </Field>

          <Field
            label={t('onboarding.password')}
            hint={t('onboarding.passwordHint')}
            required
          >
            <Input
              type="password"
              value={props.password}
              onChange={(e) => props.onPassword(e.target.value)}
              autoComplete="current-password"
              placeholder={t('onboarding.rootPassword')}
            />
          </Field>

          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t('onboarding.nickname')} hint={t('onboarding.nicknameHint')}>
              <Input
                value={props.label}
                onChange={(e) => props.onLabel(e.target.value)}
                placeholder="Tower"
              />
            </Field>
          </div>

          <button
            type="button"
            className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
            onClick={() => props.onShowAdvanced(!props.showAdvanced)}
          >
            <ChevronDown className={cn('h-3.5 w-3.5 transition-transform', props.showAdvanced && 'rotate-180')} />
            {t('onboarding.advancedSettings')}
          </button>

          {props.showAdvanced && (
            <div className="space-y-5 rounded-xl border border-dashed p-4">
              <div className="grid gap-4 sm:grid-cols-2">
                <Field
                  label={t('onboarding.sshPort')}
                  hint={t('onboarding.sshPortHint')}
                >
                  <Input
                    type="number"
                    value={props.sshPort}
                    onChange={(e) => props.onSshPort(Number(e.target.value))}
                  />
                </Field>
                <Field label={t('onboarding.username')} hint={t('onboarding.usernameHint')}>
                  <Input
                    value={props.user}
                    onChange={(e) => props.onUser(e.target.value)}
                  />
                </Field>
              </div>

              <Field
                label={t('onboarding.sshHostOverride')}
                hint={t('onboarding.sshHostHint')}
              >
                <Input
                  value={props.host}
                  onChange={(e) => props.onHost(e.target.value)}
                  placeholder={t('onboarding.sshHostPlaceholder')}
                />
              </Field>

              <div>
                <Label className="mb-2 block">{t('onboarding.authMode')}</Label>
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
                      <div className="text-sm font-medium">{t('onboarding.passwordAuth')}</div>
                      <div className="text-[10px] text-muted-foreground">{t('onboarding.passwordAuthDesc')}</div>
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
                      <div className="text-sm font-medium">{t('onboarding.keyAuth')}</div>
                      <div className="text-[10px] text-muted-foreground">{t('onboarding.keyAuthDesc')}</div>
                    </div>
                  </button>
                </div>
              </div>

              {props.authMode === 'key' && (
                <div className="space-y-2">
                  <Field label={t('onboarding.sshKey')} hint={t('onboarding.sshKeyHint')} required>
                    <div className="flex gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        className="rounded-lg"
                        onClick={() => fileRef.current?.click()}
                      >
                        <Upload className="mr-1 h-3 w-3" /> {t('onboarding.selectFile')}
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
                    className="w-full rounded-xl border bg-muted/30 p-2 font-mono-data text-xs focus:outline-none focus:ring-1 focus:ring-primary"
                    rows={4}
                    placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;...&#10;-----END OPENSSH PRIVATE KEY-----"
                    value={props.privateKey}
                    onChange={(e) => {
                      props.onPrivateKey(e.target.value);
                      if (!props.keyFileName) props.onKeyFileName(t('onboarding.manualPaste'));
                    }}
                  />
                </div>
              )}
            </div>
          )}

          <div className="flex items-center gap-2 rounded-xl border border-primary/20 bg-primary/5 p-3 text-xs text-muted-foreground">
            <ShieldCheck className="h-4 w-4 shrink-0 text-primary" />
            <span>
              {t('onboarding.securityNote')}
            </span>
          </div>
        </div>
      </div>

      {props.error && (
        <div className="rounded-xl border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
          {props.error}
        </div>
      )}

      <div className="flex justify-between">
        <Button variant="ghost" className="rounded-lg" onClick={props.onPrev}>
          <ArrowLeft className="h-4 w-4" /> {t('common.back')}
        </Button>
        <Button className="rounded-lg" onClick={props.onConnect} disabled={props.connecting}>
          {props.connecting ? (
            <>
              <Loader2 className="h-4 w-4 animate-spin" /> {t('onboarding.connecting')}
            </>
          ) : (
            <>
              <Wifi className="h-4 w-4" /> {t('onboarding.connect')}
            </>
          )}
        </Button>
      </div>
    </motion.div>
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
  const { t } = useTranslation();
  const sshAvailable = result?.sshAvailable ?? false;
  const apiAvailable = result?.apiAvailable ?? false;

  let modeLabel = '';
  let modeDesc = '';
  if (sshAvailable && apiAvailable) {
    modeLabel = t('connection.dual');
    modeDesc = t('connection.dualTip');
  } else if (apiAvailable && !sshAvailable) {
    modeLabel = t('connection.api');
    modeDesc = t('connection.apiTip');
  } else if (sshAvailable && !apiAvailable) {
    modeLabel = t('connection.ssh');
    modeDesc = t('connection.sshTip');
  }

  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={springGentle}
      className="space-y-6"
    >
      <div>
        <h1 className="text-display-md text-foreground">{t('onboarding.connectionVerify')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {t('onboarding.verifyDesc')}
        </p>
      </div>

      <div className="card-bento">
        <div className="space-y-5 p-6">
          <div className="flex items-center gap-3">
            <CheckCircle2 className="h-8 w-8 text-success" />
            <div>
              <div className="text-base font-medium">{t('onboarding.connected')}</div>
              <div className="text-xs text-muted-foreground">
                {result?.message ?? `${t('onboarding.connectedTo')} ${host} ${t('onboarding.established')}`}
              </div>
            </div>
          </div>

          <div className="rounded-xl bg-muted/40 p-3 text-sm space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">{t('onboarding.connectionMode')}</span>
              <Badge
                variant="secondary"
                className={cn(
                  'tracking-wide',
                  sshAvailable && apiAvailable
                    ? 'bg-emerald-500/15 text-emerald-600 border-emerald-500/30'
                    : apiAvailable
                      ? 'bg-amber-500/15 text-amber-600 border-amber-500/30'
                      : 'bg-muted text-muted-foreground'
                )}
              >
                {modeLabel}
              </Badge>
            </div>
            <p className="text-xs text-muted-foreground">{modeDesc}</p>

            {sshAvailable && (
              <>
                <div className="flex items-center justify-between pt-1">
                  <span className="text-muted-foreground">{t('onboarding.serverFingerprint')}</span>
                  <Badge variant="secondary" className="font-mono-data text-[10px] tracking-wide">
                    {result?.hostFingerprint ?? 'N/A'}
                  </Badge>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">{t('onboarding.unraidVersion')}</span>
                  <span className="font-mono-data text-xs">
                    {result?.serverVersion ?? 'unknown'}
                  </span>
                </div>
              </>
            )}
          </div>

          {!sshAvailable && apiAvailable && (
            <div className="flex items-center gap-2 rounded-xl border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-700 dark:text-amber-400">
              <Terminal className="h-4 w-4 shrink-0" />
              <span>
                {t('onboarding.sshNotConnected')}
              </span>
            </div>
          )}

          <p className="text-xs text-muted-foreground">
            {t('onboarding.fingerprintNote')}
          </p>
        </div>
      </div>

      <div className="flex justify-between">
        <Button variant="ghost" className="rounded-lg" onClick={onPrev}>
          <ArrowLeft className="h-4 w-4" /> {t('common.back')}
        </Button>
        <Button className="rounded-lg" onClick={onNext}>
          {t('onboarding.confirmCorrect')} <ArrowRight className="h-4 w-4" />
        </Button>
      </div>
    </motion.div>
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
  const { t } = useTranslation();
  const [uiPassword, setUiPassword] = useState('');
  const [setting, setSetting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const alreadySecured = uiAuthEnabled;

  const handleSetup = async () => {
    if (!uiPassword) {
      onNext();
      return;
    }
    if (uiPassword.length < 4) {
      setError(t('onboarding.passwordMinLen'));
      return;
    }
    setSetting(true);
    setError(null);
    try {
      await api.post('/auth/setup', { password: uiPassword });
      onNext();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : t('onboarding.setupFailed'));
    } finally {
      setSetting(false);
    }
  };

  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={springGentle}
      className="space-y-6"
    >
      <div>
        <h1 className="text-display-md text-foreground">{t('onboarding.securityTitle')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">
            {t('onboarding.securityDesc')}
        </p>
      </div>

      {alreadySecured ? (
        <div className="card-bento">
          <div className="flex items-center gap-3 p-6">
            <ShieldCheck className="h-8 w-8 text-success" />
            <div>
              <div className="text-base font-medium">{t('onboarding.passwordAlreadySet')}</div>
              <div className="text-xs text-muted-foreground">
                {t('onboarding.passwordFromEnv')}
              </div>
            </div>
          </div>
        </div>
      ) : (
      <div className="card-bento">
        <div className="space-y-5 p-6">
            <div className="flex items-center gap-2 rounded-xl border border-warning/30 bg-warning/10 p-3 text-xs text-warning-foreground">
              <Lock className="h-4 w-4 shrink-0 text-warning" />
              <span className="text-foreground/80">
                {t('onboarding.noPasswordWarning')}
              </span>
            </div>

            <Field
              label={t('onboarding.uiPassword')}
              hint={t('onboarding.uiPasswordHint')}
            >
              <Input
                type="password"
                value={uiPassword}
                onChange={(e) => {
                  setUiPassword(e.target.value);
                  setError(null);
                }}
                placeholder={t('onboarding.optionalSkip')}
                autoComplete="new-password"
              />
            </Field>

            {error && (
              <div className="text-sm text-destructive">{error}</div>
            )}
          </div>
        </div>
      )}

      <div className="flex justify-between">
        <Button variant="ghost" className="rounded-lg" onClick={onPrev}>
          <ArrowLeft className="h-4 w-4" /> {t('common.back')}
        </Button>
        <Button className="rounded-lg" onClick={handleSetup} disabled={setting}>
          {setting ? (
            <>
              <Loader2 className="h-4 w-4 animate-spin" /> {t('common.saving')}
            </>
          ) : uiPassword || alreadySecured ? (
            <>
              {t('common.next')} <ArrowRight className="h-4 w-4" />
            </>
          ) : (
            t('onboarding.skipNoPassword')
          )}
        </Button>
      </div>
    </motion.div>
  );
}

function DoneStep({ host, onFinish }: { host: string; onFinish: () => void }) {
  const { t } = useTranslation();
  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={springGentle}
      className="space-y-6 text-center"
    >
      <div className="mx-auto grid h-16 w-16 place-items-center rounded-full bg-success/15 text-success">
        <CheckCircle2 className="h-8 w-8" />
      </div>
      <div>
        <h1 className="text-display-md text-foreground">{t('onboarding.doneTitle')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {t('onboarding.doneDesc', { host })}
        </p>
      </div>
      <Button size="lg" className="rounded-lg" onClick={onFinish}>
        {t('onboarding.goDashboard')} <ArrowRight className="h-4 w-4" />
      </Button>
    </motion.div>
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

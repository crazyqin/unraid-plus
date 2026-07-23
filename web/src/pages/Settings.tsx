import { useNavigate } from 'react-router-dom';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Globe,
  KeyRound,
  LogOut,
  Monitor,
  RefreshCw,
  Server,
  Shield,
  ShieldCheck,
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
import { useSettingsStore, THEMES } from '@/stores/settings';
import { api, ApiError } from '@/lib/api';
import { ConfirmDialog } from '@/components/ui/alert-dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import i18n from '@/i18n';

const LANGUAGES = [
  { code: 'zh', label: '中文' },
  { code: 'en', label: 'English' },
  { code: 'ja', label: '日本語' },
  { code: 'ko', label: '한국어' },
  { code: 'fr', label: 'Français' },
  { code: 'de', label: 'Deutsch' },
  { code: 'es', label: 'Español' },
];

export default function SettingsPage() {
  const { t } = useTranslation();
  const server = useAuthStore((s) => s.server);
  const reset = useAuthStore((s) => s.reset);
  const uiAuthEnabled = useAuthStore((s) => s.uiAuthEnabled);
  const isUiAuthenticated = useAuthStore((s) => s.isUiAuthenticated);
  const logout = useAuthStore((s) => s.logout);
  const navigate = useNavigate();
  const [confirmDisconnect, setConfirmDisconnect] = useState(false);
  const [confirmRotate, setConfirmRotate] = useState(false);
  const [rotateLoading, setRotateLoading] = useState(false);

  const {
    refreshInterval,
    setRefreshInterval,
    onboardingDone,
    setOnboardingDone,
    theme,
    setTheme,
  } = useSettingsStore();

  const disconnect = async () => {
    try {
      await api.post('/disconnect');
    } catch {
      /* ignore — backend may already be gone */
    }
    reset();
    navigate('/onboarding', { replace: true });
  };

  const rotateKey = async () => {
    setRotateLoading(true);
    try {
      await api.post('/auth/rotate-key');
    } finally {
      setRotateLoading(false);
      setConfirmRotate(false);
    }
  };

  return (
    <div className="space-y-4 p-4 md:p-6">
      <div>
        <h1 className="text-xl font-semibold">{t('settings.title')}</h1>
        <p className="text-sm text-muted-foreground">{t('settings.subtitle')}</p>
      </div>

      {/* Connection */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <Server className="h-4 w-4" /> {t('settings.currentConnection')}
          </CardTitle>
          <CardDescription>{t('settings.currentConnectionDesc')}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          <Row label={t('settings.nicknameHost')} value={server ? `${server.label} · ${server.host}` : '—'} />
          <Row label={t('settings.sshPort')} value={server ? String(server.sshPort) : '—'} />
          <Row label={t('settings.user')} value={server?.user ?? '—'} />
          <Row
            label={t('settings.authMode')}
            value={
              server ? (
                <Badge variant={server.authMode === 'key' ? 'success' : 'warning'}>
                  {server.authMode === 'key' ? t('settings.keyFree') : t('settings.passwordMode')}
                </Badge>
              ) : (
                '—'
              )
            }
          />
          <Separator />
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" size="sm" onClick={() => location.reload()}>
              <RefreshCw className="h-3.5 w-3.5" /> {t('settings.refreshPage')}
            </Button>
            <Button variant="destructive" size="sm" onClick={() => setConfirmDisconnect(true)}>
              <LogOut className="h-3.5 w-3.5" /> {t('settings.disconnect')}
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* Security */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <Shield className="h-4 w-4" /> {t('settings.security')}
          </CardTitle>
          <CardDescription>
            {t('settings.keyFreeNote')}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          <div className="flex items-center justify-between gap-2">
            <div>
              <div className="font-medium">{t('settings.keyFreeTitle')}</div>
              <div className="text-xs text-muted-foreground">
                {t('settings.keyFreeDesc')}
              </div>
            </div>
            <Button variant="outline" size="sm" onClick={() => setConfirmRotate(true)}>
              <KeyRound className="h-3.5 w-3.5" /> {t('settings.generateRotateKey')}
            </Button>
          </div>
          <Separator />
          <div className="flex items-center justify-between gap-2">
            <div>
              <div className="font-medium">{t('settings.uiLoginProtection')}</div>
              <div className="text-xs text-muted-foreground">
                {uiAuthEnabled
                  ? t('settings.passwordEnabledDesc')
                  : t('settings.passwordDisabledDesc')}
              </div>
            </div>
            <div className="flex items-center gap-2">
              <Badge variant={uiAuthEnabled ? 'success' : 'secondary'}>
                {uiAuthEnabled ? (
                  <>
                    <ShieldCheck className="mr-1 h-3 w-3" /> {t('settings.enabled')}
                  </>
                ) : (
                  t('settings.disabled')
                )}
              </Badge>
              <UIPasswordButton uiAuthEnabled={uiAuthEnabled} />
            </div>
          </div>
          {uiAuthEnabled && isUiAuthenticated && (
            <Button
              variant="outline"
              size="sm"
              onClick={async () => {
                await logout();
                navigate('/login', { replace: true });
              }}
            >
              <LogOut className="h-3.5 w-3.5" /> {t('settings.logout')}
            </Button>
          )}
        </CardContent>
      </Card>

      {/* UI / preferences */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <Monitor className="h-4 w-4" /> {t('settings.uiAndOnboarding')}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 text-sm">
          {/* Language picker */}
          <div className="flex items-center justify-between gap-2">
            <div>
              <div className="font-medium flex items-center gap-1.5">
                <Globe className="h-3.5 w-3.5" /> {t('settings.language')}
              </div>
            </div>
            <select
              className="rounded border bg-background px-2 py-1 text-sm"
              value={i18n.language?.split('-')[0] ?? 'zh'}
              onChange={(e) => i18n.changeLanguage(e.target.value)}
            >
              {LANGUAGES.map((lang) => (
                <option key={lang.code} value={lang.code}>{lang.label}</option>
              ))}
            </select>
          </div>

          <Separator />

          <div className="flex items-center justify-between gap-2">
            <div>
              <div className="font-medium">{t('settings.themeStyle')}</div>
              <div className="text-xs text-muted-foreground">
                {t('settings.currentTheme')}{t('themes.' + theme)} · {t('settings.clickTopbar')} <span className="font-mono">☽/☀</span> {t('settings.iconSwitch')}
              </div>
            </div>
            <select
              className="rounded border bg-background px-2 py-1 text-sm"
              value={theme}
              onChange={(e) => setTheme(e.target.value as typeof theme)}
            >
              {THEMES.map((th) => (
                <option key={th.id} value={th.id}>{t('themes.' + th.id)}</option>
              ))}
            </select>
          </div>

          <Separator />

          <ToggleRow
            label={t('settings.onboardingDone')}
            desc={t('settings.onboardingDoneDesc')}
            checked={onboardingDone}
            onChange={(v) => setOnboardingDone(v)}
          />
          <Separator />
          <div className="flex items-center justify-between gap-2">
            <div>
              <div className="font-medium">{t('settings.autoRefreshInterval')}</div>
              <div className="text-xs text-muted-foreground">
                {t('settings.autoRefreshDesc')}
              </div>
            </div>
            <select
              className="rounded border bg-background px-2 py-1 text-sm"
              value={refreshInterval}
              onChange={(e) => setRefreshInterval(Number(e.target.value))}
            >
              <option value={1000}>{t('settings.1sec')}</option>
              <option value={2000}>{t('settings.2sec')}</option>
              <option value={5000}>{t('settings.5sec')}</option>
              <option value={15000}>{t('settings.15sec')}</option>
              <option value={0}>{t('common.pause')}</option>
            </select>
          </div>
        </CardContent>
      </Card>

      <p className="text-center text-xs text-muted-foreground">
        {t('settings.footer')}
      </p>

      <ConfirmDialog
        open={confirmDisconnect}
        title={t('settings.confirmDisconnectTitle')}
        description={t('settings.confirmDisconnectDesc')}
        confirmText={t('settings.disconnectBtn')}
        variant="destructive"
        onConfirm={disconnect}
        onCancel={() => setConfirmDisconnect(false)}
      />
      <ConfirmDialog
        open={confirmRotate}
        title={t('settings.rotateKeyTitle')}
        description={t('settings.rotateKeyDesc')}
        confirmText={t('settings.generateKey')}
        loading={rotateLoading}
        onConfirm={rotateKey}
        onCancel={() => setConfirmRotate(false)}
      />
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

function UIPasswordButton({ uiAuthEnabled }: { uiAuthEnabled: boolean }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [pw, setPw] = useState('');
  const [currentPw, setCurrentPw] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const checkAuth = useAuthStore((s) => s.checkAuth);

  const handleSave = async () => {
    if (pw.length < 4) {
      setError(t('settings.passwordMinLen'));
      return;
    }
    setLoading(true);
    setError(null);
    try {
      if (uiAuthEnabled) {
        await api.post('/auth/change-password', { current: currentPw, new: pw });
      } else {
        await api.post('/auth/setup', { password: pw });
      }
      await checkAuth();
      setOpen(false);
      setPw('');
      setCurrentPw('');
    } catch (e) {
      setError(e instanceof ApiError ? e.message : t('common.failed'));
    } finally {
      setLoading(false);
    }
  };

  if (!open) {
    return (
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        {uiAuthEnabled ? t('settings.changePassword') : t('settings.setPassword')}
      </Button>
    );
  }

  return (
    <div className="mt-2 space-y-2 rounded-md border p-3">
      {uiAuthEnabled && (
        <div className="space-y-1">
          <Label className="text-xs">{t('settings.currentPassword')}</Label>
          <Input
            type="password"
            value={currentPw}
            onChange={(e) => setCurrentPw(e.target.value)}
            autoComplete="current-password"
          />
        </div>
      )}
      <div className="space-y-1">
        <Label className="text-xs">{uiAuthEnabled ? t('settings.newPassword') : t('settings.setPassword')}</Label>
        <Input
          type="password"
          value={pw}
          onChange={(e) => {
            setPw(e.target.value);
            setError(null);
          }}
          autoComplete="new-password"
        />
      </div>
      {error && <div className="text-xs text-destructive">{error}</div>}
      <div className="flex gap-2">
        <Button size="sm" onClick={handleSave} disabled={loading}>
          {loading ? t('common.saving') : t('common.save')}
        </Button>
        <Button size="sm" variant="ghost" onClick={() => { setOpen(false); setPw(''); setCurrentPw(''); setError(null); }}>
          {t('common.cancel')}
        </Button>
      </div>
    </div>
  );
}

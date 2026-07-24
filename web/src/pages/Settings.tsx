import { useNavigate } from 'react-router-dom';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { motion } from 'framer-motion';
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
import { springGentle } from '@/lib/motion';
import { PageHeader, PageOrb, PageShell } from '@/components/layout/PageShell';

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
    <PageShell>
      <PageOrb className="-right-16 -top-10 bg-amber-500/10" />

      <PageHeader
        eyebrow="Preferences"
        title={t('settings.title')}
        meta={<span className="text-sm text-muted-foreground">{t('settings.subtitle')}</span>}
      />

      {/* Connection */}
      <motion.div
        className="card-bento overflow-hidden"
        initial={{ opacity: 0, y: 10 }}
        animate={{ opacity: 1, y: 0 }}
        transition={springGentle}
      >
        <div className="space-y-1 border-b border-border/30 px-5 pt-5 pb-4">
          <div className="flex items-center gap-2 text-base font-semibold tracking-tight">
            <Server className="h-4 w-4 text-muted-foreground" /> {t('settings.currentConnection')}
          </div>
          <p className="text-sm text-muted-foreground">{t('settings.currentConnectionDesc')}</p>
        </div>
        <div className="space-y-3 px-5 pb-5 pt-4 text-sm">
          <Row label={t('settings.nicknameHost')} value={server ? `${server.label} · ${server.host}` : '—'} />
          <Row label={t('settings.sshPort')} value={server ? String(server.sshPort) : '—'} />
          <Row label={t('settings.user')} value={server?.user ?? '—'} />
          <Row
            label={t('settings.authMode')}
            value={
              server ? (
                <Badge variant={server.authMode === 'key' ? 'success' : 'warning'} className="tracking-wide">
                  {server.authMode === 'key' ? t('settings.keyFree') : t('settings.passwordMode')}
                </Badge>
              ) : (
                '—'
              )
            }
          />
          <Separator />
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" size="sm" className="h-9 rounded-xl" onClick={() => location.reload()}>
              <RefreshCw className="h-3.5 w-3.5" /> {t('settings.refreshPage')}
            </Button>
            <Button variant="destructive" size="sm" className="h-9 rounded-xl" onClick={() => setConfirmDisconnect(true)}>
              <LogOut className="h-3.5 w-3.5" /> {t('settings.disconnect')}
            </Button>
          </div>
        </div>
      </motion.div>

      {/* Security */}
      <div className="card-bento overflow-hidden">
        <div className="space-y-1 border-b border-border/30 px-5 pt-5 pb-4">
          <div className="flex items-center gap-2 text-base font-semibold tracking-tight">
            <Shield className="h-4 w-4 text-muted-foreground" /> {t('settings.security')}
          </div>
          <p className="text-sm text-muted-foreground">
            {t('settings.keyFreeNote')}
          </p>
        </div>
        <div className="space-y-4 px-5 pb-5 pt-4 text-sm">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="min-w-0">
              <div className="font-medium">{t('settings.keyFreeTitle')}</div>
              <div className="text-xs text-muted-foreground">
                {t('settings.keyFreeDesc')}
              </div>
            </div>
            <Button variant="outline" size="sm" className="h-9 shrink-0 rounded-xl" onClick={() => setConfirmRotate(true)}>
              <KeyRound className="h-3.5 w-3.5" /> {t('settings.generateRotateKey')}
            </Button>
          </div>
          <Separator />
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="min-w-0">
              <div className="font-medium">{t('settings.uiLoginProtection')}</div>
              <div className="text-xs text-muted-foreground">
                {uiAuthEnabled
                  ? t('settings.passwordEnabledDesc')
                  : t('settings.passwordDisabledDesc')}
              </div>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <Badge variant={uiAuthEnabled ? 'success' : 'secondary'} className="rounded-full tracking-wide">
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
              className="h-9 rounded-xl"
              onClick={async () => {
                await logout();
                navigate('/login', { replace: true });
              }}
            >
              <LogOut className="h-3.5 w-3.5" /> {t('settings.logout')}
            </Button>
          )}
        </div>
      </div>

      {/* UI / preferences */}
      <div className="card-bento overflow-hidden">
        <div className="border-b border-border/30 px-5 pt-5 pb-4">
          <div className="flex items-center gap-2 text-base font-semibold tracking-tight">
            <Monitor className="h-4 w-4 text-muted-foreground" /> {t('settings.uiAndOnboarding')}
          </div>
        </div>
        <div className="space-y-5 px-5 pb-5 pt-4 text-sm">
          {/* Language picker */}
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <div className="flex items-center gap-1.5 font-medium">
                <Globe className="h-3.5 w-3.5" /> {t('settings.language')}
              </div>
            </div>
            <select
              className="rounded-xl border border-border/50 bg-background/50 px-2.5 py-1.5 text-sm backdrop-blur-sm"
              value={i18n.language?.split('-')[0] ?? 'zh'}
              onChange={(e) => i18n.changeLanguage(e.target.value)}
            >
              {LANGUAGES.map((lang) => (
                <option key={lang.code} value={lang.code}>{lang.label}</option>
              ))}
            </select>
          </div>

          <Separator />

          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <div className="font-medium">{t('settings.themeStyle')}</div>
              <div className="text-xs text-muted-foreground">
                {t('settings.currentTheme')}{t('themes.' + theme)} · {t('settings.clickTopbar')} {t('settings.iconSwitch')}
              </div>
            </div>
            <select
              className="rounded-xl border border-border/50 bg-background/50 px-2.5 py-1.5 text-sm backdrop-blur-sm"
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
              className="rounded-xl border border-border/50 bg-background/50 px-2.5 py-1.5 text-sm backdrop-blur-sm"
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
        </div>
      </div>

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
    </PageShell>
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
      <Button variant="outline" size="sm" className="rounded-lg" onClick={() => setOpen(true)}>
        {uiAuthEnabled ? t('settings.changePassword') : t('settings.setPassword')}
      </Button>
    );
  }

  return (
    <div className="mt-2 space-y-2 rounded-xl border p-3">
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
        <Button size="sm" className="rounded-lg" onClick={handleSave} disabled={loading}>
          {loading ? t('common.saving') : t('common.save')}
        </Button>
        <Button size="sm" variant="ghost" className="rounded-lg" onClick={() => { setOpen(false); setPw(''); setCurrentPw(''); setError(null); }}>
          {t('common.cancel')}
        </Button>
      </div>
    </div>
  );
}

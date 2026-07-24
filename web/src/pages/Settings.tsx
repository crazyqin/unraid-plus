import { useNavigate } from 'react-router-dom';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { motion } from 'framer-motion';
import {
  Globe,
  KeyRound,
  Loader2,
  LogOut,
  Monitor,
  Pencil,
  Plus,
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
  const activeServerId = useAuthStore((s) => s.activeServerId);
  const reset = useAuthStore((s) => s.reset);
  const refreshServers = useAuthStore((s) => s.refreshServers);
  const selectServer = useAuthStore((s) => s.selectServer);
  const setConnectionMode = useAuthStore((s) => s.setConnectionMode);
  const uiAuthEnabled = useAuthStore((s) => s.uiAuthEnabled);
  const isUiAuthenticated = useAuthStore((s) => s.isUiAuthenticated);
  const logout = useAuthStore((s) => s.logout);
  const navigate = useNavigate();
  const [confirmDisconnect, setConfirmDisconnect] = useState(false);
  const [confirmRotate, setConfirmRotate] = useState(false);
  const [rotateLoading, setRotateLoading] = useState(false);
  const [editing, setEditing] = useState(false);
  const [saveLoading, setSaveLoading] = useState(false);
  const [saveMsg, setSaveMsg] = useState<{ kind: 'ok' | 'err'; text: string } | null>(null);

  // Editable connection fields
  const [formLabel, setFormLabel] = useState('');
  const [formHost, setFormHost] = useState('');
  const [formPort, setFormPort] = useState(22);
  const [formUser, setFormUser] = useState('root');
  const [formApiBase, setFormApiBase] = useState('');
  const [formPassword, setFormPassword] = useState('');

  const {
    refreshInterval,
    setRefreshInterval,
    onboardingDone,
    setOnboardingDone,
    theme,
    setTheme,
  } = useSettingsStore();

  // Load full server detail (includes apiBase) when editing opens / server changes
  useEffect(() => {
    if (!server) return;
    setFormLabel(server.label || '');
    setFormHost(server.host || '');
    setFormPort(server.sshPort || 22);
    setFormUser(server.user || 'root');
    setFormApiBase(server.apiBase || `http://${server.host}`);
    setFormPassword('');
  }, [server?.id, server?.host, server?.sshPort, server?.user, server?.label, server?.apiBase]);

  useEffect(() => {
    const id = activeServerId || server?.id;
    if (!id || !editing) return;
    let cancelled = false;
    (async () => {
      try {
        const detail = await api.get<{
          id: string;
          host: string;
          port: number;
          user: string;
          apiBase: string;
          label: string;
        }>(`/servers/${encodeURIComponent(id)}`);
        if (cancelled) return;
        setFormLabel(detail.label || '');
        setFormHost(detail.host || '');
        setFormPort(detail.port || 22);
        setFormUser(detail.user || 'root');
        setFormApiBase(detail.apiBase || `http://${detail.host}`);
      } catch {
        /* keep local form */
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [editing, activeServerId, server?.id]);

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

  const saveServer = async () => {
    const id = activeServerId || server?.id;
    if (!id) return;
    if (!formHost.trim()) {
      setSaveMsg({ kind: 'err', text: t('settings.hostPlaceholder') });
      return;
    }
    setSaveLoading(true);
    setSaveMsg(null);
    try {
      const res = await api.put<{
        ok: boolean;
        message?: string;
        serverId?: string;
        sshAvailable?: boolean;
        apiAvailable?: boolean;
      }>(`/servers/${encodeURIComponent(id)}`, {
        host: formHost.trim(),
        sshPort: formPort || 22,
        user: formUser.trim() || 'root',
        apiBase: formApiBase.trim(),
        label: formLabel.trim(),
        password: formPassword,
        reconnect: true,
      });
      setSaveMsg({ kind: 'ok', text: res.message || t('settings.editServerSuccess') });
      setFormPassword('');
      setEditing(false);
      await refreshServers();
      if (res.serverId) {
        selectServer(res.serverId);
      }
      if (typeof res.sshAvailable === 'boolean' || typeof res.apiAvailable === 'boolean') {
        setConnectionMode(!!res.sshAvailable, !!res.apiAvailable);
      }
      window.setTimeout(() => setSaveMsg(null), 4000);
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : t('settings.editServerFailed');
      setSaveMsg({ kind: 'err', text: msg });
    } finally {
      setSaveLoading(false);
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
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2 text-base font-semibold tracking-tight">
              <Server className="h-4 w-4 text-muted-foreground" /> {t('settings.currentConnection')}
            </div>
            {server && !editing && (
              <Button
                variant="outline"
                size="sm"
                className="h-9 rounded-xl"
                onClick={() => setEditing(true)}
              >
                <Pencil className="h-3.5 w-3.5" /> {t('settings.editServer')}
              </Button>
            )}
          </div>
          <p className="text-sm text-muted-foreground">{t('settings.currentConnectionDesc')}</p>
        </div>
        <div className="space-y-3 px-5 pb-5 pt-4 text-sm">
          {!editing ? (
            <>
              <Row label={t('settings.nicknameHost')} value={server ? `${server.label || '—'} · ${server.host}` : '—'} />
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
            </>
          ) : (
            <div className="space-y-4">
              <Field label={t('settings.label')}>
                <Input
                  className="h-10 rounded-xl"
                  value={formLabel}
                  onChange={(e) => setFormLabel(e.target.value)}
                  placeholder={t('settings.labelPlaceholder')}
                />
              </Field>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label={t('settings.host')}>
                  <Input
                    className="h-10 rounded-xl"
                    value={formHost}
                    onChange={(e) => setFormHost(e.target.value)}
                    placeholder={t('settings.hostPlaceholder')}
                  />
                </Field>
                <Field label={t('settings.sshPort')}>
                  <Input
                    className="h-10 rounded-xl"
                    type="number"
                    min={1}
                    max={65535}
                    value={formPort}
                    onChange={(e) => setFormPort(Number(e.target.value) || 22)}
                  />
                </Field>
              </div>
              <Field label={t('settings.user')}>
                <Input
                  className="h-10 rounded-xl"
                  value={formUser}
                  onChange={(e) => setFormUser(e.target.value)}
                />
              </Field>
              <Field label={t('settings.apiBase')} hint={t('settings.apiBaseHint')}>
                <Input
                  className="h-10 rounded-xl"
                  value={formApiBase}
                  onChange={(e) => setFormApiBase(e.target.value)}
                  placeholder={t('settings.apiBasePlaceholder')}
                />
              </Field>
              <Field label={t('settings.rootPassword')} hint={t('settings.rootPasswordHint')}>
                <Input
                  className="h-10 rounded-xl"
                  type="password"
                  autoComplete="new-password"
                  value={formPassword}
                  onChange={(e) => setFormPassword(e.target.value)}
                  placeholder={t('settings.rootPasswordPlaceholder')}
                />
              </Field>
              <div className="flex flex-wrap gap-2 pt-1">
                <Button
                  size="sm"
                  className="h-9 rounded-xl"
                  disabled={saveLoading}
                  onClick={() => void saveServer()}
                >
                  {saveLoading ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <RefreshCw className="h-3.5 w-3.5" />
                  )}{' '}
                  {saveLoading ? t('settings.saving') : t('settings.saveAndReconnect')}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-9 rounded-xl"
                  disabled={saveLoading}
                  onClick={() => {
                    setEditing(false);
                    setFormPassword('');
                    setSaveMsg(null);
                  }}
                >
                  {t('common.cancel')}
                </Button>
              </div>
            </div>
          )}

          {saveMsg && (
            <div
              className={
                saveMsg.kind === 'ok'
                  ? 'rounded-xl border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-xs text-emerald-700 dark:text-emerald-400'
                  : 'rounded-xl border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive'
              }
            >
              {saveMsg.text}
            </div>
          )}

          <Separator />
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              size="sm"
              className="h-9 rounded-xl"
              onClick={() => navigate('/onboarding?mode=add')}
            >
              <Plus className="h-3.5 w-3.5" /> {t('settings.addServer')}
            </Button>
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
    <div className="flex items-center justify-between gap-3">
      <span className="text-muted-foreground">{label}</span>
      <span className="text-right font-medium">{value}</span>
    </div>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs font-medium text-muted-foreground">{label}</Label>
      {children}
      {hint && <p className="text-[11px] text-muted-foreground/70">{hint}</p>}
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

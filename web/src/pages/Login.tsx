import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { motion } from 'framer-motion';
import { Lock, Loader2, ShieldCheck } from 'lucide-react';
import { useAuthStore } from '@/stores/auth';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { ApiError } from '@/lib/api';
import { springGentle } from '@/lib/motion';

export default function LoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const login = useAuthStore((s) => s.login);
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!password) return;
    setLoading(true);
    setError('');
    try {
      const ok = await login(password);
      if (ok) {
        navigate('/', { replace: true });
      } else {
        setError(t('login.wrongPassword'));
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('login.loginFailed'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/30 p-5">
      <motion.div
        initial={{ opacity: 0, y: 20, scale: 0.98 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        transition={springGentle}
        className="glass-heavy w-full max-w-sm rounded-2xl p-0"
      >
        <div className="space-y-2 p-8 text-center">
          <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10 glow-primary-sm">
            <ShieldCheck className="h-7 w-7 text-primary" />
          </div>
          <h1 className="text-display-lg text-foreground">{t('login.title')}</h1>
          <p className="text-sm text-muted-foreground">{t('login.desc')}</p>
        </div>
        <div className="px-8 pb-8">
          <form onSubmit={submit} className="space-y-5">
            <div className="space-y-2">
              <Label htmlFor="password">{t('login.password')}</Label>
              <div className="relative">
                <Lock className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  id="password"
                  type="password"
                  className="pl-9 rounded-lg"
                  placeholder={t('login.placeholder')}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoFocus
                  disabled={loading}
                />
              </div>
            </div>
            {error && (
              <p className="text-sm text-destructive">{error}</p>
            )}
            <Button type="submit" className="w-full rounded-lg" disabled={loading || !password}>
              {loading ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" /> {t('login.loggingIn')}
                </>
              ) : (
                t('login.login')
              )}
            </Button>
          </form>
        </div>
      </motion.div>
    </div>
  );
}

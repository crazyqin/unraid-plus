import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import i18n from '@/i18n';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { motion } from 'framer-motion';
import { Plus, RotateCw, TerminalSquare, Trash2, X } from 'lucide-react';
import { wsUrl } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { springGentle } from '@/lib/motion';
import { PageHeader, PageOrb, PageShell } from '@/components/layout/PageShell';

interface Session {
  id: string;
  term: XTerm;
  fit: FitAddon;
  ws: WebSocket | null;
  alive: boolean;
}

function createSession(onClose: (id: string) => void): Session {
  const id = `s${Date.now()}`;
  const term = new XTerm({
    fontFamily: 'Menlo, Consolas, monospace',
    fontSize: 13,
    cursorBlink: true,
    theme: {
      background: '#00000000',
      foreground: '#e5e7eb',
      cursor: '#f97316',
    },
  });
  const fit = new FitAddon();
  term.loadAddon(fit);

  const ws = new WebSocket(wsUrl(`/ws/terminal?id=${id}`));
  ws.binaryType = 'arraybuffer';
  ws.onopen = () => {
    try {
      fit.fit();
      ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
    } catch {
      /* noop */
    }
  };
  ws.onmessage = (e) => {
    const data = typeof e.data === 'string' ? e.data : new TextDecoder().decode(e.data);
    term.write(data);
  };
  ws.onclose = () => {
    term.write(`\r\n\x1b[31m${i18n.t('terminal.wsDisconnected')}\x1b[0m\r\n`);
    onClose(id);
  };
  ws.onerror = () => {
    term.write(`\r\n\x1b[31m${i18n.t('terminal.wsError')}\x1b[0m\r\n`);
  };

  term.onData((d) => {
    if (ws.readyState === WebSocket.OPEN) ws.send(d);
  });

  return { id, term, fit, ws, alive: true };
}

export default function TerminalPage() {
  const { t } = useTranslation();
  const containerRef = useRef<HTMLDivElement>(null);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [activeId, setActiveId] = useState<string | null>(null);

  const markDead = (id: string) => {
    setSessions((ss) => ss.map((s) => (s.id === id ? { ...s, alive: false } : s)));
  };

  const openSession = () => {
    const session = createSession(markDead);
    setSessions((s) => [...s, session]);
    setActiveId(session.id);
  };

  const reconnectSession = (oldId: string) => {
    const old = sessions.find((s) => s.id === oldId);
    if (!old) return;

    old.ws?.close();
    old.term.dispose();

    const session = createSession(markDead);
    setSessions((ss) => ss.map((s) => (s.id === oldId ? session : s)));
    if (activeId === oldId) {
      setActiveId(session.id);
    }
  };

  const closeSession = (id: string) => {
    setSessions((ss) => {
      const target = ss.find((s) => s.id === id);
      target?.ws?.close();
      target?.term.dispose();
      const next = ss.filter((s) => s.id !== id);
      if (activeId === id) {
        setActiveId(next[next.length - 1]?.id ?? null);
      }
      return next;
    });
  };

  useEffect(() => {
    if (!containerRef.current || !activeId) return;
    const active = sessions.find((s) => s.id === activeId);
    if (!active) return;
    containerRef.current.innerHTML = '';
    active.term.open(containerRef.current);
    try {
      active.fit.fit();
    } catch {
      /* noop */
    }
    active.term.focus();
  }, [activeId, sessions]);

  useEffect(() => {
    const onResize = () => {
      const active = sessions.find((s) => s.id === activeId);
      if (!active) return;
      try {
        active.fit.fit();
        active.ws?.send(
          JSON.stringify({ type: 'resize', cols: active.term.cols, rows: active.term.rows }),
        );
      } catch {
        /* noop */
      }
    };
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, [sessions, activeId]);

  useEffect(() => {
    if (sessions.length === 0) openSession();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    return () => {
      setSessions((ss) => {
        for (const s of ss) {
          s.ws?.close();
          s.term.dispose();
        }
        return [];
      });
    };
  }, []);

  const activeSession = sessions.find((s) => s.id === activeId);
  const aliveCount = sessions.filter((s) => s.alive).length;

  return (
    <PageShell className="relative flex h-full min-h-0 flex-col">
      <PageOrb className="-right-16 -top-16 bg-emerald-500/10" />

      <PageHeader
        eyebrow={
          <span className="inline-flex items-center gap-2">
            <TerminalSquare className="h-3 w-3 text-primary" />
            Shell
          </span>
        }
        title={t('terminal.title')}
        meta={
          <>
            <Badge
              variant={aliveCount > 0 ? 'success' : 'secondary'}
              className="rounded-full px-2.5 text-[11px] font-semibold tracking-wide"
            >
              {aliveCount > 0 ? t('terminal.connected') : t('terminal.disconnected')}
            </Badge>
            <Badge variant="secondary" className="rounded-full px-2.5 font-mono-data text-[11px]">
              {sessions.length} {t('terminal.sessionCount')}
            </Badge>
            <span className="text-xs text-muted-foreground">{t('terminal.desc')}</span>
          </>
        }
        actions={
          <div className="flex flex-wrap items-center gap-2">
            {sessions.length > 0 && (
              <Button
                size="sm"
                variant="ghost"
                className="h-9 rounded-xl"
                onClick={() => sessions.forEach((s) => closeSession(s.id))}
              >
                <Trash2 className="h-3.5 w-3.5" /> {t('terminal.closeAll')}
              </Button>
            )}
            <Button size="sm" className="h-9 rounded-xl" onClick={openSession}>
              <Plus className="h-3.5 w-3.5" /> {t('terminal.newSession')}
            </Button>
          </div>
        }
      />

      {/* Session tabs */}
      {sessions.length > 0 && (
        <motion.div
          className="flex flex-wrap items-center gap-2"
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          transition={springGentle}
        >
          {sessions.map((s, idx) => (
            <button
              key={s.id}
              type="button"
              onClick={() => setActiveId(s.id)}
              className={cn(
                'group inline-flex items-center gap-2 rounded-xl border px-3 py-2 text-xs transition-colors',
                s.id === activeId
                  ? 'border-primary/40 bg-primary/10 text-foreground shadow-sm'
                  : 'border-border/50 bg-card/40 text-muted-foreground hover:bg-accent/60 hover:text-foreground',
              )}
            >
              <TerminalSquare className="h-3.5 w-3.5 shrink-0 opacity-80" />
              <span
                className={cn(
                  'inline-block h-1.5 w-1.5 shrink-0 rounded-full',
                  s.alive ? 'bg-emerald-500' : 'bg-destructive',
                )}
              />
              <span className="font-mono-data">
                {t('terminal.session')} {idx + 1}
              </span>
              {!s.alive && (
                <span
                  role="button"
                  tabIndex={0}
                  title={t('terminal.reconnect')}
                  onClick={(e) => {
                    e.stopPropagation();
                    reconnectSession(s.id);
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.stopPropagation();
                      reconnectSession(s.id);
                    }
                  }}
                  className="rounded-md p-0.5 text-amber-500 hover:bg-accent"
                >
                  <RotateCw className="h-3 w-3" />
                </span>
              )}
              <span
                role="button"
                tabIndex={0}
                onClick={(e) => {
                  e.stopPropagation();
                  closeSession(s.id);
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.stopPropagation();
                    closeSession(s.id);
                  }
                }}
                className="rounded-md p-0.5 opacity-50 transition-opacity hover:bg-accent hover:opacity-100 group-hover:opacity-100"
              >
                <X className="h-3 w-3" />
              </span>
            </button>
          ))}
        </motion.div>
      )}

      {/* Terminal pane */}
      <div className="card-bento relative min-h-0 flex-1 overflow-hidden p-0">
        {sessions.length === 0 ? (
          <div className="flex h-full min-h-[18rem] flex-col items-center justify-center gap-4 text-sm text-muted-foreground">
            <TerminalSquare className="h-12 w-12 text-muted-foreground/25" />
            <p>{t('terminal.openFirst')}</p>
            <Button className="h-9 rounded-xl" onClick={openSession}>
              <Plus className="h-3.5 w-3.5" /> {t('terminal.newSession')}
            </Button>
          </div>
        ) : (
          <div
            ref={containerRef}
            className="h-full min-h-[18rem] overflow-hidden bg-[#0b0b0d] p-3"
          />
        )}

        {/* Reconnect overlay when active session is dead */}
        {activeSession && !activeSession.alive && (
          <div className="absolute inset-0 z-10 flex items-center justify-center bg-background/40 backdrop-blur-sm">
            <motion.div
              className="flex flex-col items-center gap-3 rounded-2xl border border-border/50 bg-card/95 p-6 shadow-xl glass"
              initial={{ opacity: 0, scale: 0.96 }}
              animate={{ opacity: 1, scale: 1 }}
              transition={springGentle}
            >
              <TerminalSquare className="h-8 w-8 text-muted-foreground/40" />
              <p className="text-sm text-muted-foreground">{t('terminal.disconnected')}</p>
              <Button
                size="sm"
                className="h-9 rounded-xl"
                onClick={() => reconnectSession(activeSession.id)}
              >
                <RotateCw className="h-3.5 w-3.5" /> {t('terminal.reconnectBtn')}
              </Button>
            </motion.div>
          </div>
        )}
      </div>

      <p className="text-xs text-muted-foreground/80">{t('terminal.tip')}</p>
    </PageShell>
  );
}

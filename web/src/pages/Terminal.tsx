import { useEffect, useRef, useState } from 'react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { Plus, RotateCw, TerminalSquare, Trash2, X } from 'lucide-react';
import { wsUrl } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

interface Session {
  id: string;
  term: XTerm;
  fit: FitAddon;
  ws: WebSocket | null;
  alive: boolean;
}

// createSession builds a new terminal + WebSocket session. Shared by
// openSession and reconnectSession to avoid duplication.
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
    term.write('\r\n\x1b[31m[连接已断开]\x1b[0m\r\n');
    onClose(id);
  };
  ws.onerror = () => {
    term.write('\r\n\x1b[31m[WebSocket 错误 — 后端终端服务尚未就绪]\x1b[0m\r\n');
  };

  term.onData((d) => {
    if (ws.readyState === WebSocket.OPEN) ws.send(d);
  });

  return { id, term, fit, ws, alive: true };
}

export default function TerminalPage() {
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

    // Clean up old terminal and WS
    old.ws?.close();
    old.term.dispose();

    // Create fresh session in the same slot
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

  // When the active session changes, attach the active term to the container.
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

  // Resize listener
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

  // Open an initial session on mount.
  useEffect(() => {
    if (sessions.length === 0) openSession();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const activeSession = sessions.find((s) => s.id === activeId);

  return (
    <div className="flex h-full flex-col p-4 md:p-6">
      <div className="mb-3 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">SSH 终端</h1>
          <p className="text-sm text-muted-foreground">
            浏览器内直接进入 Unraid 命令行 · 支持多会话
          </p>
        </div>
        <Button size="sm" onClick={openSession}>
          <Plus className="h-3.5 w-3.5" /> 新会话
        </Button>
      </div>

      {/* Tabs */}
      {sessions.length > 0 && (
        <div className="mb-2 flex items-center gap-1 border-b pb-2">
          {sessions.map((s) => (
            <button
              key={s.id}
              onClick={() => setActiveId(s.id)}
              className={cn(
                'group flex items-center gap-2 rounded-t border-t border-l border-r px-3 py-1.5 text-xs',
                s.id === activeId
                  ? 'border-border bg-card'
                  : 'border-transparent text-muted-foreground hover:bg-accent',
              )}
            >
              <TerminalSquare className="h-3.5 w-3.5" />
              <span
                className={cn(
                  'inline-block h-1.5 w-1.5 rounded-full',
                  s.alive ? 'bg-success' : 'bg-destructive',
                )}
              />
              {s.id.slice(-4)}
              {!s.alive && (
                <span
                  role="button"
                  tabIndex={0}
                  title="重连"
                  onClick={(e) => {
                    e.stopPropagation();
                    reconnectSession(s.id);
                  }}
                  className="rounded p-0.5 hover:bg-accent"
                >
                  <RotateCw className="h-3 w-3 text-warning" />
                </span>
              )}
              <span
                role="button"
                tabIndex={0}
                onClick={(e) => {
                  e.stopPropagation();
                  closeSession(s.id);
                }}
                className="rounded p-0.5 opacity-0 hover:bg-accent group-hover:opacity-100"
              >
                <X className="h-3 w-3" />
              </span>
            </button>
          ))}
        </div>
      )}

      {/* Terminal pane */}
      <div
        ref={containerRef}
        className="flex-1 overflow-hidden rounded-md border bg-[#0b0b0d] p-2"
      />

      {/* Reconnect overlay when active session is dead */}
      {activeSession && !activeSession.alive && (
        <div className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2">
          <div className="flex flex-col items-center gap-3 rounded-lg border bg-card/95 p-6 shadow-lg">
            <p className="text-sm text-muted-foreground">终端连接已断开</p>
            <Button size="sm" onClick={() => reconnectSession(activeSession.id)}>
              <RotateCw className="h-3.5 w-3.5" /> 重新连接
            </Button>
          </div>
        </div>
      )}

      {sessions.length === 0 && (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          <Button variant="ghost" onClick={openSession}>
            <Plus className="h-4 w-4" /> 打开第一个会话
          </Button>
        </div>
      )}

      <div className="mt-2 flex items-center justify-between text-xs text-muted-foreground">
        <span>
          提示：会话通过后端 SSH 通道转发，断线后可点击重连按钮恢复。
        </span>
        {sessions.length > 0 && (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => sessions.forEach((s) => closeSession(s.id))}
          >
            <Trash2 className="h-3.5 w-3.5" /> 关闭全部
          </Button>
        )}
      </div>
    </div>
  );
}

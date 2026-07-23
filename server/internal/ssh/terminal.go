package ssh

import (
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	xssh "golang.org/x/crypto/ssh"

	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// TerminalHub multiplexes browser WebSocket terminals onto SSH pty sessions.
// Each browser tab dials /ws/terminal?id=<client-chosen-id>; the hub finds the
// active pool connection and attaches a new SSH shell to it.
type TerminalHub struct {
	pool *Pool

	mu       sync.Mutex
	sessions map[string]*terminalSession
}

// NewTerminalHub constructs a hub. The hub does not own the pool — it asks for
// the Active() client on demand so reconnects are transparent.
func NewTerminalHub(pool *Pool) *TerminalHub {
	return &TerminalHub{
		pool:     pool,
		sessions: make(map[string]*terminalSession),
	}
}

type terminalSession struct {
	id   string
	ws   *websocket.Conn
	sess *xssh.Session
}

// msgIn is the JSON envelope the browser sends over the WebSocket.
type msgIn struct {
	Type string `json:"type"`
	// raw stdin bytes (base64? no — we use TextMessage for json control,
	// BinaryMessage for raw bytes; see Serve).
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

// msgOut is the JSON envelope the server sends back for control messages.
type msgOut struct {
	Type string `json:"type"` // "stdout" | "stderr" | "exit" | "error"
	Data string `json:"data,omitempty"`
}

// Serve upgrades the WebSocket and bridges it to a new SSH pty session.
func (h *TerminalHub) Serve(c *websocket.Conn) {
	defer c.Close()

	client, err := h.pool.Active()
	if err != nil {
		_ = writeJSON(c, msgOut{Type: "error", Data: err.Error()})
		return
	}

	id := c.RemoteAddr().String() + "/" + time.Now().Format("150405.000")
	sess, stdin, stdout, stderr, err := client.NewInteractiveSession(80, 24)
	if err != nil {
		_ = writeJSON(c, msgOut{Type: "error", Data: err.Error()})
		return
	}

	h.register(id, sess)
	defer func() {
		_ = sess.Close()
		h.unregister(id)
	}()

	// stdout + stderr -> ws: both goroutines share a mutex because
	// WebSocket connections are not safe for concurrent writes (same
	// fix pattern as docker_logs.go).
	var wsMu sync.Mutex
	go pipe(stdout, c, "stdout", &wsMu)
	go pipe(stderr, c, "stderr", &wsMu)

	// Read loop: handle both control JSON (Text) and raw stdin (Binary).
	// xterm.js term.onData() sends keystrokes as TextMessage (raw UTF-8
	// strings), NOT BinaryMessage. If the TextMessage doesn't parse as
	// our JSON control envelope, treat it as raw stdin so keystrokes
	// flow through to the SSH PTY.
	for {
		mtype, data, err := c.ReadMessage()
		if err != nil {
			return
		}
		switch mtype {
		case websocket.TextMessage:
			var m msgIn
			if err := json.Unmarshal(data, &m); err != nil || m.Type == "" {
				// Not a JSON control message — treat as raw stdin.
				_, _ = stdin.Write(data)
				continue
			}
			switch m.Type {
			case "resize":
				if m.Cols > 0 && m.Rows > 0 {
					if err := sess.WindowChange(m.Rows, m.Cols); err != nil {
						logger.Warnf("terminal %s: window-change %dx%d failed: %v", id, m.Cols, m.Rows, err)
					} else {
						logger.Debugf("terminal %s: resized to %dx%d", id, m.Cols, m.Rows)
					}
				}
			case "stdin":
				if m.Data != "" {
					_, _ = stdin.Write([]byte(m.Data))
				}
			default:
				// Unknown JSON type — write raw data as stdin anyway.
				_, _ = stdin.Write(data)
			}
		case websocket.BinaryMessage:
			_, _ = stdin.Write(data)
		}
	}
}

func (h *TerminalHub) register(id string, sess *xssh.Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions[id] = &terminalSession{id: id, sess: sess}
	logger.Debugf("terminal attached: %s (total=%d)", id, len(h.sessions))
}

func (h *TerminalHub) unregister(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, id)
	logger.Debugf("terminal detached: %s (total=%d)", id, len(h.sessions))
}

// pipe forwards bytes from an SSH pipe to the WebSocket as TextMessage
// (which xterm.write accepts directly). On EOF or error it stops.
// A mutex is required because both stdout and stderr goroutines call
// WriteMessage concurrently — WebSocket connections are not safe for
// concurrent writes (same fix as docker_logs.go).
func pipe(r io.Reader, c *websocket.Conn, kind string, mu *sync.Mutex) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			mu.Lock()
			werr := c.WriteMessage(websocket.TextMessage, buf[:n])
			mu.Unlock()
			if werr != nil {
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				logger.Debugf("terminal %s pipe ended: %v", kind, err)
			}
			mu.Lock()
			_ = writeJSON(c, msgOut{Type: "exit"})
			mu.Unlock()
			return
		}
	}
}

func writeJSON(c *websocket.Conn, m msgOut) error {
	b, _ := json.Marshal(m)
	return c.WriteMessage(websocket.TextMessage, b)
}

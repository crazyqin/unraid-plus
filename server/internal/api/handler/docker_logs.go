package handler

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// dockerLogsUpgrader is purpose-built for /ws/docker-logs.
// Origin: allow same-host LAN deployments (not only localhost).
var dockerLogsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     AllowWSOrigin,
}

// containerIDRe matches:
//   - docker container id (hex, 12-64)
//   - container name ([A-Za-z0-9_.-])
//   - Unraid PrefixedID ("<64hex>:<localId>")
// Anything else is rejected — the value flows into `docker logs <id>`.
var containerIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.\-:]{0,200}$`)

// DockerLogs streams a container's logs over WebSocket. Query params:
//
//	container (required) — container id, PrefixedID, or name
//	tail              (optional, default 200)  — number of trailing lines first
//	follow            (optional, default true) — keep streaming new logs
//	serverId          (optional) — multi-server routing
//
// Wire format: server sends TextMessage frames containing raw log bytes
// (stdout+stderr merged by `docker logs` already). On stream end the server
// sends a final TextMessage with JSON `{"type":"exit"}` and closes.
func (h *Handler) DockerLogs(c *gin.Context) {
	container := c.Query("container")
	if container == "" || !containerIDRe.MatchString(container) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"ok":      false,
			"message": "缺少或非法的 container 参数",
		})
		return
	}
	// Strip Unraid PrefixedID machine hash — docker CLI only knows local ids.
	container = stripDockerContainerID(container)
	if container == "" || !containerIDRe.MatchString(container) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"ok":      false,
			"message": "无法解析 container id",
		})
		return
	}

	tail := 200
	if v := c.Query("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 100000 {
			tail = n
		}
	}
	follow := true
	if v := c.Query("follow"); v == "0" || v == "false" {
		follow = false
	}

	cli, _, hasSSH, _ := h.prepareServer(c)
	if !hasSSH {
		// Docker logs requires SSH (docker logs command via WebSocket)
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"ok":          false,
			"message":     "Docker 日志需要 SSH 连接",
			"requiresSSH": true,
		})
		return
	}

	conn, err := dockerLogsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Warnf("docker-logs ws upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Build the docker logs command. We merge stderr into stdout (--timestamps
	// optional; skipping it keeps raw bytes which the frontend shows verbatim).
	cmd := "docker logs --tail " + strconv.Itoa(tail)
	if follow {
		cmd += " -f"
	}
	cmd += " " + shellQuote(container)
	logger.Debugf("docker-logs: streaming %s (follow=%v tail=%d)", container, follow, tail)

	// wsWriter adapts the WebSocket connection to an io.Writer for RunStream.
	// Any write error (client gone, network drop) propagates up through
	// RunStream, which terminates the SSH session. The mutex serializes all
	// WebSocket writes so the exit-frame goroutine can't race with the
	// stream goroutine.
	w := &wsWriter{conn: conn}

	// Run the streaming command in a goroutine so the main goroutine can
	// watch for client-side disconnects (ReadMessage returns non-nil when
	// the browser closes the WS). Without this, RunStream would block
	// forever and the SSH session would leak after the user closes the
	// log dialog.
	done := make(chan error, 1)
	go func() {
		done <- cli.RunStream(cmd, w)
	}()

	// Also send an "exit" frame when RunStream returns so the frontend
	// can mark the stream as ended (and stop its spinner).
	go func() {
		err := <-done
		if err != nil {
			logger.Debugf("docker-logs stream ended: %v", err)
		}
		// Best-effort: client may already be gone. Use the same mutex
		// as wsWriter to avoid concurrent WriteMessage on the conn.
		w.writeRaw(websocket.TextMessage, []byte(`{"type":"exit"}`))
		_ = conn.Close()
	}()

	// Block until the client disconnects. ReadMessage returns an error as
	// soon as the underlying connection is closed by either side.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

// wsWriter is a minimal io.Writer that pushes each Write call as a single
// WebSocket TextMessage. We deliberately do NOT buffer or frame-chunk: the
// frontend appends whatever bytes arrive to a <pre>, so chunk boundaries are
// irrelevant and zero-buffer keeps memory tight on long-lived streams.
// The mutex serializes writes so concurrent callers (stream goroutine +
// exit-frame goroutine) don't corrupt the WebSocket frame stream.
type wsWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// writeRaw sends a single message under the mutex. Used by both Write and
// the exit-frame goroutine to guarantee serial access to conn.
func (w *wsWriter) writeRaw(msgType int, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(msgType, data)
}

func (w *wsWriter) Write(p []byte) (int, error) {
	if err := w.writeRaw(websocket.TextMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// stripDockerContainerID removes Unraid PrefixedID machine-hash prefix.
// "aabbcc...hash:601778a4deadbeef" → "601778a4deadbeef"
func stripDockerContainerID(id string) string {
	if i := strings.LastIndex(id, ":"); i >= 0 && i < len(id)-1 {
		prefix := id[:i]
		// machine hashes are long hex strings
		if len(prefix) >= 16 {
			return id[i+1:]
		}
	}
	return id
}

// AllowWSOrigin accepts empty Origin (non-browser clients) and same-host
// Origins so LAN deployments (http://192.168.x.x:9876) work. This app is
// self-hosted and never intentionally embedded cross-origin.
func AllowWSOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	// Same host as the request (scheme may differ if reverse-proxied)
	host := r.Host
	if host == "" {
		return true
	}
	if strings.Contains(origin, "://"+host) {
		return true
	}
	// Common local aliases
	return strings.HasPrefix(origin, "http://localhost") ||
		strings.HasPrefix(origin, "http://127.0.0.1") ||
		strings.HasPrefix(origin, "https://localhost") ||
		strings.HasPrefix(origin, "https://127.0.0.1")
}

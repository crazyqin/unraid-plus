package handler

import (
	"net/http"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// dockerLogsUpgrader is purpose-built for /ws/docker-logs. Same permissive
// origin check as the terminal upgrader — the API router already sits behind
// the same CORS policy.
var dockerLogsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// containerIDRe matches a docker container id (hex, 12-64 chars) OR a legal
// container name ([A-Za-z0-9_.-], up to 128 chars). Anything else is rejected
// to prevent shell injection — the value flows into `docker logs <id>`.
var containerIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.\-]{0,127}$`)

// DockerLogs streams a container's logs over WebSocket. Query params:
//
//	container (required) — container id or name
//	tail              (optional, default 200)  — number of trailing lines first
//	follow            (optional, default true) — keep streaming new logs
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

	cli, ok := h.activeClient(c)
	if !ok {
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
	// RunStream, which terminates the SSH session.
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
		// Best-effort: client may already be gone.
		_ = conn.WriteMessage(websocket.TextMessage,
			[]byte(`{"type":"exit"}`))
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
type wsWriter struct {
	conn *websocket.Conn
}

func (w *wsWriter) Write(p []byte) (int, error) {
	if err := w.conn.WriteMessage(websocket.TextMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

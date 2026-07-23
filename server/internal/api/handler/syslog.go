package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// Syslog returns recent system log entries.
// GET /api/syslog?lines=200
//
// Uses Unraid's WebGUI syslog endpoint. Falls back to SSH
// (tail /var/log/syslog) on API failure.
func (h *Handler) Syslog(c *gin.Context) {
	_, sid, ok := h.activeClientWithID(c)
	if !ok {
		return
	}

	lines := c.DefaultQuery("lines", "200")

	// Try Unraid HTTP API first
	if h.ur.HasSession(sid) {
		body, status, err := h.ur.SystemLog(sid)
		if err == nil && status == http.StatusOK && len(body) > 0 {
			logText := string(body)
			// If it's HTML, strip tags
			if strings.Contains(logText, "<") {
				logText = stripHTMLTags(logText)
			}
			c.JSON(http.StatusOK, gin.H{"ok": true, "log": logText, "via": "api"})
			return
		}
		if err != nil {
			logger.Debugf("syslog api failed, falling back to SSH: %v", err)
		}
	}

	// SSH fallback
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}

	cmd := "tail -n " + shellQuote(lines) + " /var/log/syslog 2>/dev/null"
	out, err := cli.Run(cmd)
	if err != nil {
		errOut(c, http.StatusInternalServerError, "读取系统日志失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "log": out, "via": "ssh"})
}

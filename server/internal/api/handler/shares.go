package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// ListShares returns all configured shares.
// GET /api/shares
//
// Uses Unraid's ShareList.php endpoint. Falls back to SSH
// (ls /mnt/user) on API failure.
func (h *Handler) ListShares(c *gin.Context) {
	_, sid, hasSSH, hasAPI := h.resolveServer(c)
	if !hasSSH && !hasAPI {
		return
	}

	// Try Unraid HTTP API first
	if hasAPI {
		body, status, err := h.ur.ShareList(sid)
		if err == nil && status == http.StatusOK && len(body) > 0 {
			shares := parseShareList(string(body))
			if shares != nil {
				c.JSON(http.StatusOK, gin.H{"ok": true, "shares": shares, "via": "api"})
				return
			}
		}
		if err != nil {
			logger.Debugf("share list api failed, falling back to SSH: %v", err)
		}
	}

	// SSH fallback
	if !hasSSH {
		c.JSON(http.StatusOK, gin.H{"ok": true, "shares": []string{}, "message": "SSH 不可用，无法通过命令行列出共享（WebGUI API 未返回数据）"})
		return
	}
	cli, _ := h.pool.Active()
	if cli == nil {
		if h.sm != nil {
			entry := h.sm.Get(sid)
			if entry != nil {
				cli, _ = h.pool.Get(entry.Host, entry.Port)
			}
		}
	}
	if cli == nil {
		c.JSON(http.StatusOK, gin.H{"ok": true, "shares": []string{}, "message": "SSH 连接不可用"})
		return
	}

	out, err := cli.Run("ls -1 /mnt/user/ 2>/dev/null")
	if err != nil || strings.TrimSpace(out) == "" {
		c.JSON(http.StatusOK, gin.H{"ok": true, "shares": []string{}})
		return
	}

	shares := []string{}
	for _, s := range strings.Split(strings.TrimSpace(out), "\n") {
		s = strings.TrimSpace(s)
		if s != "" {
			shares = append(shares, s)
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "shares": shares, "via": "ssh"})
}

// shareInfo holds basic share info.
type shareInfo struct {
	Name     string `json:"name"`
	Comment  string `json:"comment,omitempty"`
	Freezer  string `json:"freezer,omitempty"`  // "yes" or "no" (cache setting)
	Security string `json:"security,omitempty"` // "public", "private", "secure"
}

// parseShareList extracts share names from ShareList.php HTML output.
func parseShareList(html string) []shareInfo {
	if strings.TrimSpace(html) == "" {
		return nil
	}
	// ShareList.php returns HTML with share entries.
	// Do a simple extraction: look for share names in the output.
	stripped := stripHTMLTags(html)
	if strings.TrimSpace(stripped) == "" {
		return nil
	}

	var shares []shareInfo
	for _, line := range strings.Split(stripped, "\n") {
		name := strings.TrimSpace(line)
		if name != "" && !strings.HasPrefix(name, "#") {
			shares = append(shares, shareInfo{Name: name})
		}
	}
	if len(shares) == 0 {
		return nil
	}
	return shares
}

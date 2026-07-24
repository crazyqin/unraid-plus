package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// UPSStatus returns the current UPS status.
// GET /api/ups
//
// Uses Unraid's UPSstatus.php endpoint. Falls back to SSH
// (apcaccess) on API failure.
func (h *Handler) UPSStatus(c *gin.Context) {
	_, sid, hasSSH, hasAPI := h.resolveServer(c)
	if !hasSSH && !hasAPI {
		return
	}

	// Try Unraid HTTP API first
	if hasAPI {
		body, status, err := h.ur.UPSStatus(sid)
		if err == nil && status == http.StatusOK && len(body) > 0 {
			// UPSstatus.php returns HTML, try to parse the useful bits
			result := parseUPSHTML(string(body))
			if result != nil {
				c.JSON(http.StatusOK, gin.H{"ok": true, "ups": result, "via": "api"})
				return
			}
		}
		if err != nil {
			logger.Debugf("ups api failed, falling back to SSH: %v", err)
		}
	}

	// SSH fallback: apcaccess
	if !hasSSH {
		c.JSON(http.StatusOK, gin.H{"ok": true, "ups": nil, "message": "SSH 不可用，无法读取 UPS 信息（WebGUI API 未返回数据）"})
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
		c.JSON(http.StatusOK, gin.H{"ok": true, "ups": nil, "message": "SSH 连接不可用"})
		return
	}

	out, err := cli.Run("apcaccess 2>/dev/null || upsc ups 2>/dev/null")
	if err != nil || strings.TrimSpace(out) == "" {
		c.JSON(http.StatusOK, gin.H{"ok": true, "ups": nil, "message": "未检测到 UPS 设备"})
		return
	}

	result := parseAPCAccess(out)
	c.JSON(http.StatusOK, gin.H{"ok": true, "ups": result, "via": "ssh"})
}

// upsInfo holds the structured UPS data we expose.
type upsInfo struct {
	Model       string `json:"model,omitempty"`
	Status      string `json:"status,omitempty"`
	Battery     string `json:"battery,omitempty"`
	Runtime     string `json:"runtime,omitempty"`
	LoadPercent string `json:"loadPercent,omitempty"`
	InputV      string `json:"inputV,omitempty"`
	OutputV     string `json:"outputV,omitempty"`
	LastTest    string `json:"lastTest,omitempty"`
	Raw         string `json:"raw,omitempty"` // raw output for advanced view
}

// parseAPCAccess parses output from `apcaccess` (apcupsd).
// Format is key-value pairs like:
//
//	APC      : 001,036,0865
//	STATUS   : ONLINE
//	BCHARGE  : 100.0 Percent
//	TIMELEFT : 62.0 Minutes
//	LOADPCT  : 12.0 Percent
func parseAPCAccess(out string) *upsInfo {
	info := &upsInfo{Raw: out}
	for _, line := range strings.Split(out, "\n") {
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		switch key {
		case "MODEL", "APCMODEL":
			info.Model = val
		case "STATUS":
			info.Status = val
		case "BCHARGE":
			info.Battery = val
		case "TIMELEFT":
			info.Runtime = val
		case "LOADPCT":
			info.LoadPercent = val
		case "LINEV", "INPUTV":
			info.InputV = val
		case "OUTPUTV":
			info.OutputV = val
		case "LASTSTEST":
			info.LastTest = val
		}
	}
	return info
}

// parseUPSHTML tries to extract UPS info from UPSstatus.php HTML output.
// Unraid's UPS status page renders as HTML with key-value rows.
func parseUPSHTML(html string) *upsInfo {
	if strings.TrimSpace(html) == "" {
		return nil
	}
	// The HTML typically contains the same data as apcaccess but wrapped
	// in HTML tags. We do a simple extraction by stripping tags and
	// falling through parseAPCAccess.
	stripped := stripHTMLTags(html)
	if strings.TrimSpace(stripped) == "" {
		return nil
	}
	return parseAPCAccess(stripped)
}

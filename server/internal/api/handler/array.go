package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/crazyqin/unraid-plus/server/internal/unraid"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// ArrayAction starts or stops the Unraid array.
// v0.9+: GraphQL mutation first, then PHP API (ParityControl.php), then SSH.
func (h *Handler) ArrayAction(c *gin.Context) {
	_, sid, hasSSH, hasAPI := h.resolveServer(c)
	if !hasSSH && !hasAPI {
		return
	}
	action := c.Param("action")
	switch action {
	case "start", "stop":
	default:
		errOut(c, http.StatusBadRequest, "不支持的操作: "+action+"（仅支持 start / stop）")
		return
	}

	// GraphQL mutation first (official API)
	if hasAPI && h.ur.HasGraphQL(sid) {
		var query string
		var opName string
		switch action {
		case "start":
			query = unraid.MutStartArray
			opName = "StartArray"
		case "stop":
			query = unraid.MutStopArray
			opName = "StopArray"
		}
		data, err := h.ur.GraphQLQueryWithOp(sid, query, nil, opName)
		if err == nil && data != nil {
			var result map[string]json.RawMessage
			if json.Unmarshal(data, &result) == nil {
				c.JSON(http.StatusOK, gin.H{
					"ok":      true,
					"message": "已发送 " + action + " 指令",
					"via":     "graphql",
				})
				return
			}
		}
		if err != nil {
			logger.Debugf("array graphql action %s failed, falling back: %v", action, err)
		}
	}

	// PHP API fallback (ParityControl.php)
	if hasAPI {
		body, status, err := h.ur.ParityControl(sid, action)
		if err == nil && status >= 200 && status < 300 {
			c.JSON(http.StatusOK, gin.H{
				"ok":      true,
				"message": "已发送 " + action + " 指令",
				"via":     "api",
				"detail":  string(body),
			})
			return
		}
		if err != nil {
			logger.Debugf("array api action %s failed, falling back to SSH: %v", action, err)
		}
	}

	// SSH fallback
	if !hasSSH {
		errOut(c, http.StatusServiceUnavailable, "阵列操作需要连接（GraphQL/API/SSH 均不可用）")
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
		errOut(c, http.StatusServiceUnavailable, "SSH 连接不可用")
		return
	}

	out, err := cli.Run("mdcmd " + action + " 2>&1")
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{
			"ok":      false,
			"message": "阵列操作失败",
			"detail":  strings.TrimSpace(out),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "已发送 " + action + " 指令",
		"via":     "ssh",
	})
}

// ParityCheckAction starts / stops / resumes a parity check / sync.
// v0.9+: GraphQL does not yet have parity check mutations, so we use PHP API
// then SSH fallback.
func (h *Handler) ParityCheckAction(c *gin.Context) {
	_, sid, hasSSH, hasAPI := h.resolveServer(c)
	if !hasSSH && !hasAPI {
		return
	}
	action := c.Param("action")

	switch action {
	case "start", "stop", "correcting", "resume":
	default:
		errOut(c, http.StatusBadRequest, "不支持的操作: "+action+"（仅支持 start / stop / correcting / resume）")
		return
	}

	// PHP API first (no GraphQL mutation for parity check yet)
	if hasAPI {
		body, status, err := h.ur.ParityControl(sid, action)
		if err == nil && status >= 200 && status < 300 {
			c.JSON(http.StatusOK, gin.H{
				"ok":      true,
				"message": map[string]string{"start": "Parity 检查已启动", "stop": "Parity 检查已停止", "correcting": "Parity 纠错检查已启动", "resume": "Parity 检查已恢复"}[action],
				"via":     "api",
				"detail":  string(body),
			})
			return
		}
		if err != nil {
			logger.Debugf("parity api action %s failed, falling back to SSH: %v", action, err)
		}
	}

	// SSH fallback
	if !hasSSH {
		errOut(c, http.StatusServiceUnavailable, "Parity 操作需要连接（API/SSH 均不可用）")
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
		errOut(c, http.StatusServiceUnavailable, "SSH 连接不可用")
		return
	}

	var cmd string
	switch action {
	case "start":
		cmd = "mdcmd check 2>&1"
	case "stop":
		cmd = "mdcmd nocheck 2>&1"
	case "correcting":
		cmd = "mdcmd check CORRECT 2>&1"
	case "resume":
		cmd = "mdcmd check 2>&1"
	default:
		errOut(c, http.StatusBadRequest, "不支持的操作: "+action)
		return
	}

	out, err := cli.Run(cmd)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{
			"ok":      false,
			"message": "Parity 操作失败",
			"detail":  strings.TrimSpace(out),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": map[string]string{"start": "Parity 检查已启动", "stop": "Parity 检查已停止", "correcting": "Parity 纠错检查已启动", "resume": "Parity 检查已恢复"}[action],
		"via":     "ssh",
	})
}

// parityStatusResp reports the current parity check progress.
type parityStatusResp struct {
	State     string  `json:"state"`     // "checking" | "idle" | "unknown"
	Progress  float64 `json:"progress"`  // 0-100
	Speed     string  `json:"speed"`     // e.g. "152 MB/s"
	Remaining string  `json:"remaining"` // e.g. "2h 15m"
	Errors    int     `json:"errors"`    // sync errors found
	Via       string  `json:"via"`       // "graphql", "ssh" or "api"
}

// parityCheckRe matches the parity check progress line in /proc/mdstat.
var parityCheckRe = regexp.MustCompile(`check\s*=\s*([\d.]+)%.*finish=([\d.]+)min.*speed=(\d+)([KMG])`)

// ParityStatus returns parity check progress.
// v0.9+: GraphQL-first (GetParityStatus query), SSH fallback, API-only mode.
func (h *Handler) ParityStatus(c *gin.Context) {
	cli, sid, hasSSH, hasAPI := h.prepareServer(c)
	if sid == "" {
		return
	}

	// GraphQL-first: use official API when available
	if hasAPI && h.ur.HasGraphQL(sid) {
		resp := h.parityStatusGraphQL(sid)
		if resp != nil {
			// Enrich with /proc/mdstat via SSH if available (more precise speed/ETA)
			if hasSSH && cli != nil {
				out, _ := cli.Run("cat /proc/mdstat 2>/dev/null")
				mdstatResp := parseMdstat(out)
				// Override speed/remaining with SSH data when parity is checking
				if mdstatResp.State == "checking" {
					resp.Speed = mdstatResp.Speed
					resp.Remaining = mdstatResp.Remaining
				}
			}
			c.JSON(http.StatusOK, resp)
			return
		}
	}

	// SSH fallback
	if hasSSH && cli != nil {
		out, _ := cli.Run("cat /proc/mdstat 2>/dev/null")
		resp := parseMdstat(out)
		resp.Via = "ssh"
		c.JSON(http.StatusOK, resp)
		return
	}

	// API-only mode (no parity data available)
	resp := parityStatusResp{State: "unknown", Via: "api"}
	if hasAPI {
		resp.State = "idle"
	}
	c.JSON(http.StatusOK, resp)
}

// parityStatusGraphQL queries the GraphQL API for parity check status.
func (h *Handler) parityStatusGraphQL(sid string) *parityStatusResp {
	data, err := h.ur.GraphQLQuery(sid, unraid.QueryGetParityStatus, nil)
	if err != nil {
		logger.Debugf("parity graphql query failed for %s: %v", sid, err)
		return nil
	}

	arr, err := unraid.ParseArrayQuery(data)
	if err != nil {
		logger.Debugf("parity graphql parse failed for %s: %v", sid, err)
		return nil
	}

	if arr.ParityCheckStatus == nil {
		// No parity check status in response — return idle
		return &parityStatusResp{State: "idle", Via: "graphql"}
	}

	pc := arr.ParityCheckStatus
	resp := &parityStatusResp{Via: "graphql"}

	if pc.Running {
		resp.State = "checking"
		resp.Progress = pc.Progress
		resp.Errors = pc.Errors
		if pc.Correcting {
			resp.Speed = fmt.Sprintf("%.0f MB/s (correcting)", pc.Speed)
		} else {
			resp.Speed = fmt.Sprintf("%.0f MB/s", pc.Speed)
		}
		if pc.Paused {
			resp.State = "paused"
		}
	} else {
		resp.State = "idle"
	}

	return resp
}

// parseMdstat extracts parity check progress from /proc/mdstat content.
func parseMdstat(out string) parityStatusResp {
	resp := parityStatusResp{State: "idle"}

	if m := parityCheckRe.FindStringSubmatch(out); m != nil {
		resp.State = "checking"
		resp.Progress = atofSafe(m[1])
		speedNum := atoi64Safe(m[3])
		unit := m[4]
		resp.Speed = formatSpeed(speedNum, unit)
		mins := atofSafe(m[2])
		resp.Remaining = formatRemaining(mins)
	}

	if strings.Contains(out, "[_") || strings.Contains(out, "_]") {
		resp.Errors = 1
	}

	return resp
}

// formatSpeed converts a numeric speed + SI suffix (K/M/G) into a
// human-readable MB/s string.
func formatSpeed(num int64, unit string) string {
	switch unit {
	case "G":
		return strconv.FormatFloat(float64(num)*1024, 'f', 0, 64) + " MB/s"
	case "M":
		return strconv.FormatInt(num, 10) + " MB/s"
	case "K":
		return strconv.FormatFloat(float64(num)/1024, 'f', 1, 64) + " MB/s"
	default:
		return strconv.FormatInt(num, 10) + " B/s"
	}
}

// formatRemaining converts minutes (as float) to "Xh Ym" or "Ym".
func formatRemaining(mins float64) string {
	totalMin := int(mins)
	h := totalMin / 60
	m := totalMin % 60
	if h > 0 {
		return strconv.Itoa(h) + "h " + strconv.Itoa(m) + "m"
	}
	return strconv.Itoa(m) + "m"
}

package handler

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// arrayActionReq is the body for POST /api/storage/array/:action.
// Currently no fields — action is in the URL path — but we keep the struct
// for future extensibility (e.g. forced stop, read-only mode).
type arrayActionReq struct{}

// ArrayAction starts or stops the Unraid array.
// v0.3+: Prefer Unraid HTTP API (ParityControl.php) with SSH fallback.
//
// Unraid's md driver is controlled through /root/mdcmd which writes to
// /proc/mdstat. The commands are:
//   mdcmd start   — bring the array online
//   mdcmd stop    — stop the array (all disks must be spun down, no mounts)
//
// Security: action is validated against a strict whitelist (start|stop)
// before being passed to the shell.
var allowedArrayActions = map[string]string{
	"start": "start",
	"stop":  "stop",
}

func (h *Handler) ArrayAction(c *gin.Context) {
	_, sid, ok := h.activeClientWithID(c)
	if !ok {
		return
	}
	action := c.Param("action")
	cmd, valid := allowedArrayActions[action]
	if !valid {
		errOut(c, http.StatusBadRequest, "不支持的操作: "+action+"（仅支持 start / stop）")
		return
	}

	// Try Unraid HTTP API first (ParityControl.php handles array start/stop too)
	if h.ur.HasSession(sid) {
		body, status, err := h.ur.ParityControl(sid, cmd)
		if err == nil && status >= 200 && status < 300 {
			c.JSON(http.StatusOK, gin.H{
				"ok":      true,
				"message": "已发送 " + cmd + " 指令",
				"via":     "api",
				"detail":  string(body),
			})
			return
		}
		if err != nil {
			logger.Debugf("array api action %s failed, falling back to SSH: %v", cmd, err)
		}
	}

	// SSH fallback
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}

	out, err := cli.Run("mdcmd " + cmd + " 2>&1")
	if err != nil {
		// mdcmd returns non-zero for "array already started" etc.
		// We still return the output so the UI can show the reason.
		c.JSON(http.StatusConflict, gin.H{
			"ok":      false,
			"message": "阵列操作失败",
			"detail":  strings.TrimSpace(out),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "已发送 " + cmd + " 指令",
		"via":     "ssh",
	})
}

// ParityCheckAction starts / stops / resumes a parity check / sync.
// v0.3+: Prefer Unraid HTTP API (ParityControl.php) with SSH fallback.
//
// Actions:
//   - "start": start non-correcting parity check
//   - "stop": pause/cancel ongoing parity check
//   - "correcting": start correcting parity check (writes fixes)
//   - "resume": resume a paused parity check
func (h *Handler) ParityCheckAction(c *gin.Context) {
	_, sid, ok := h.activeClientWithID(c)
	if !ok {
		return
	}
	action := c.Param("action")

	// Validate action
	switch action {
	case "start", "stop", "correcting", "resume":
	default:
		errOut(c, http.StatusBadRequest, "不支持的操作: "+action+"（仅支持 start / stop / correcting / resume）")
		return
	}

	// Try Unraid HTTP API first
	if h.ur.HasSession(sid) {
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
	cli, ok := h.activeClient(c)
	if !ok {
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
		cmd = "mdcmd check 2>&1" // resume is same as check in mdcmd
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
	Via       string  `json:"via"`       // "ssh" or "api"
}

// parityCheckRe matches the parity check progress line in /proc/mdstat.
var parityCheckRe = regexp.MustCompile(`check\s*=\s*([\d.]+)%.*finish=([\d.]+)min.*speed=(\d+)([KMG])`)

// ParityStatus returns parity check progress.
// v0.4+: Tries SSH /proc/mdstat first (richest data), then falls back to
// var.ini mdState for API-only mode (returns basic state without progress details).
func (h *Handler) ParityStatus(c *gin.Context) {
	sid, hasSid := h.getServerID(c)

	// Try SSH first (richest data source: /proc/mdstat has progress/speed/ETA)
	cli, _, hasCli := h.activeClientWithID(c)
	if hasCli {
		out, _ := cli.Run("cat /proc/mdstat 2>/dev/null")
		resp := parseMdstat(out)
		resp.Via = "ssh"
		c.JSON(http.StatusOK, resp)
		return
	}

	// SSH unavailable — API-only mode
	// We can't get /proc/mdstat, but we can infer basic state from var.ini
	// if we have a cached state file read. For now, return a basic response.
	resp := parityStatusResp{State: "unknown", Via: "api"}

	if hasSid && h.ur.HasSession(sid) {
		// API session available but no SSH — we can at least report the array state
		// The mdState from var.ini tells us if the array is started, but not parity
		// check progress. Return "idle" as best-effort when SSH is unavailable.
		resp.State = "idle"
		resp.Via = "api"
	}

	c.JSON(http.StatusOK, resp)
}

// parseMdstat extracts parity check progress from /proc/mdstat content.
// Extracted as a pure function for testability.
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
// human-readable MB/s string. mdstat reports in K (KiB/s) typically.
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

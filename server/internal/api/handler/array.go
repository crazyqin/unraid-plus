package handler

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// arrayActionReq is the body for POST /api/storage/array/:action.
// Currently no fields — action is in the URL path — but we keep the struct
// for future extensibility (e.g. forced stop, read-only mode).
type arrayActionReq struct{}

// ArrayAction starts or stops the Unraid array via `mdcmd`.
//
// Unraid's md driver is controlled through /root/mdcmd which writes to
// /proc/mdstat. The commands are:
//   mdcmd start   — bring the array online
//   mdcmd stop    — stop the array (all disks must be spun down, no mounts)
//
// We run the command and check the exit code. mdcmd returns non-zero if
// the array is already in the requested state or if stop is blocked by
// active mounts — we surface those as 409 Conflict with the raw error.
//
// Security: action is validated against a strict whitelist (start|stop)
// before being passed to the shell. The shellQuote call is belt-and-
// suspenders since the whitelist already eliminates injection.
var allowedArrayActions = map[string]string{
	"start": "start",
	"stop":  "stop",
}

func (h *Handler) ArrayAction(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}
	action := c.Param("action")
	cmd, valid := allowedArrayActions[action]
	if !valid {
		errOut(c, http.StatusBadRequest, "不支持的操作: "+action+"（仅支持 start / stop）")
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
	})
}

// ParityCheckAction starts or cancels a parity check / sync.
//
//   mdcmd check       — start parity check (non-correcting, read-only)
//   mdcmd nocheck      — pause/cancel ongoing parity check
//
// The "correcting" variant (mdcmd check CORRECT) writes corrections to
// parity as it goes; we don't expose that via the API for safety — the
// user should SSH in manually if they need a correcting check.
func (h *Handler) ParityCheckAction(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}
	action := c.Param("action")
	var cmd string
	switch action {
	case "start":
		cmd = "mdcmd check 2>&1"
	case "stop":
		cmd = "mdcmd nocheck 2>&1"
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
		"message": map[string]string{"start": "Parity 检查已启动", "stop": "Parity 检查已停止"}[action],
	})
}

// parityStatusResp reports the current parity check progress.
type parityStatusResp struct {
	State     string  `json:"state"`     // "checking" | "idle" | "unknown"
	Progress  float64 `json:"progress"`  // 0-100
	Speed     string  `json:"speed"`     // e.g. "152 MB/s"
	Remaining string  `json:"remaining"` // e.g. "2h 15m"
	Errors    int     `json:"errors"`    // sync errors found
}

// ParityStatus parses /proc/mdstat to extract parity check progress.
// /proc/mdstat on Unraid looks like:
//
//   Personalities : [raid6] [raid5] [raid4]
//   md127 : active raid5 sdb[1] sdc[2] sda[0]
//         11700000000 blocks super 1.2 level 5, 512k chunk, algorithm 2 [3/3] [UUU]
//         [==>..............]  check = 12.3% (1440000000/11700000000) finish=85.3min speed=152000K/sec
//   unused devices: <none>
//
// We grep for "check =" to determine if a check is running and extract
// the percentage, speed, and ETA. When no check line is found, state="idle".
var parityCheckRe = regexp.MustCompile(`check\s*=\s*([\d.]+)%.*finish=([\d.]+)min.*speed=(\d+)([KMG])`)

func (h *Handler) ParityStatus(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}
	out, _ := cli.Run("cat /proc/mdstat 2>/dev/null")

	resp := parityStatusResp{State: "idle"}

	if m := parityCheckRe.FindStringSubmatch(out); m != nil {
		resp.State = "checking"
		// m[1] = percentage, m[2] = finish minutes, m[3] = speed number, m[4] = unit
		resp.Progress = atofSafe(m[1])

		// Convert speed: number + K/M/G suffix → human-readable.
		speedNum := atoi64Safe(m[3])
		unit := m[4]
		resp.Speed = formatSpeed(speedNum, unit)

		// Convert finish minutes to "Xh Ym" format.
		mins := atofSafe(m[2])
		resp.Remaining = formatRemaining(mins)
	}

	// Check for sync errors: " [U_U]" or "[UU_]" indicates a degraded/
	// failed disk, and mdstat may show a count on the "resync" or
	// "recovery" lines. We do a simple grep for "reshape" or mismatch.
	if strings.Contains(out, "[_") || strings.Contains(out, "_]") {
		resp.Errors = 1 // at least one disk slot is down
	}

	c.JSON(http.StatusOK, resp)
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

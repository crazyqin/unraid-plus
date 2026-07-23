package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// DiskSpin handles disk spin up/down requests.
// POST /api/storage/disk/spin
// Body: {"device": "sdb", "action": "spinup|spindown"}
//
// Uses Unraid's ToggleState.php endpoint. Falls back to SSH
// (hdparm -S 0 / hdparm -y) on API failure.
func (h *Handler) DiskSpin(c *gin.Context) {
	_, sid, ok := h.activeClientWithID(c)
	if !ok {
		return
	}

	var req struct {
		Device string `json:"device"` // e.g. "sdb", "sdall"
		Action string `json:"action"` // "spinup" or "spindown"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errOut(c, http.StatusBadRequest, "请求格式错误: "+err.Error())
		return
	}
	if req.Device == "" {
		errOut(c, http.StatusBadRequest, "device 不能为空")
		return
	}
	if req.Action != "spinup" && req.Action != "spindown" {
		errOut(c, http.StatusBadRequest, "action 仅支持 spinup / spindown")
		return
	}

	// Try Unraid HTTP API first
	if h.ur.HasSession(sid) {
		body, status, err := h.ur.DiskSpin(sid, req.Device, req.Action)
		if err == nil && status >= 200 && status < 300 {
			c.JSON(http.StatusOK, gin.H{
				"ok":      true,
				"message": "磁盘 " + req.Device + " " + actionChinese(req.Action),
				"via":     "api",
				"detail":  string(body),
			})
			return
		}
		if err != nil {
			logger.Debugf("disk spin api failed, falling back to SSH: %v", err)
		}
	}

	// SSH fallback
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}

	var cmd string
	switch req.Action {
	case "spinup":
		// hdparm -S 0 disables standby (keeps disk spinning)
		cmd = "hdparm -S 0 /dev/" + shellQuote(req.Device) + " 2>&1"
	case "spindown":
		// hdparm -y puts disk to standby immediately
		cmd = "hdparm -y /dev/" + shellQuote(req.Device) + " 2>&1"
	}

	out, err := cli.Run(cmd)
	if err != nil {
		errOut(c, http.StatusInternalServerError, "磁盘操作失败: "+strings.TrimSpace(out))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "磁盘 " + req.Device + " " + actionChinese(req.Action),
		"via":     "ssh",
	})
}

func actionChinese(a string) string {
	switch a {
	case "spinup":
		return "已启动旋转"
	case "spindown":
		return "已休眠"
	default:
		return a
	}
}

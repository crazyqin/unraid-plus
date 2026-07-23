package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

type vm struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Vcpus       int    `json:"vcpus"`
	MemoryBytes int64  `json:"memoryBytes"`
	Autostart   bool   `json:"autostart"`
}

// ListVMs calls `virsh list --all` plus `virsh dominfo` per VM. Returns an
// empty list (not an error) if libvirtd is not available — that's a normal
// state on Unraid servers with no VMs configured.
func (h *Handler) ListVMs(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}

	out, err := cli.Run(`virsh list --all --name 2>/dev/null`)
	if err != nil || strings.TrimSpace(out) == "" {
		c.JSON(http.StatusOK, []vm{})
		return
	}

	vms := []vm{}
	for _, name := range strings.Split(strings.TrimSpace(out), "\n") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		info, _ := cli.Run("virsh dominfo " + shellQuote(name) + " 2>/dev/null")
		vms = append(vms, parseDominfo(name, info))
	}
	c.JSON(http.StatusOK, vms)
}

// VMAction starts / stops / restarts / pauses / resumes a VM.
// v0.3+: Prefer Unraid HTTP API (VMajax.php) with SSH fallback.
func (h *Handler) VMAction(c *gin.Context) {
	_, sid, ok := h.activeClientWithID(c)
	if !ok {
		return
	}
	id := c.Param("id")
	action := c.Param("action")

	// Map our action names to VMajax.php action names
	apiActionMap := map[string]string{
		"start":    "domain-start",
		"stop":     "domain-destroy",
		"shutdown": "domain-stop",
		"resume":   "domain-resume",
		"suspend":  "domain-pause",
		"restart":  "domain-restart",
	}

	// Try Unraid HTTP API first
	if apiAction, ok := apiActionMap[action]; ok && h.ur.HasSession(sid) {
		resp, err := h.ur.VMActionOK(sid, apiAction, id)
		if err == nil && resp != nil && resp.Success {
			c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已发送 " + action, "via": "api"})
			return
		}
		if err != nil {
			logger.Debugf("vm api action %s/%s failed, falling back to SSH: %v", action, id, err)
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
		cmd = "virsh start " + shellQuote(id)
	case "stop":
		cmd = "virsh destroy " + shellQuote(id)
	case "shutdown":
		cmd = "virsh shutdown " + shellQuote(id)
	case "resume":
		cmd = "virsh resume " + shellQuote(id)
	case "suspend":
		cmd = "virsh suspend " + shellQuote(id)
	default:
		errOut(c, http.StatusBadRequest, "不支持的操作: "+action)
		return
	}
	if _, err := cli.Run(cmd); err != nil {
		errOut(c, http.StatusInternalServerError, "执行 virsh "+action+" 失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已发送 " + action, "via": "ssh"})
}

// parseDominfo extracts the bits we expose from `virsh dominfo` text output.
func parseDominfo(name, info string) vm {
	v := vm{ID: name, Name: name, Status: "unknown"}
	for _, line := range strings.Split(info, "\n") {
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		switch key {
		case "State":
			v.Status = normalizeVMState(val)
		case "CPU(s)":
			v.Vcpus = atoiSafe(val, 0)
		case "Max memory":
			// virsh reports in KiB; convert to bytes
			fields := strings.Fields(val)
			if len(fields) > 0 {
				v.MemoryBytes = atoi64Safe(fields[0]) * 1024
			}
		case "Autostart":
			v.Autostart = val == "enable"
		}
	}
	return v
}

func normalizeVMState(s string) string {
	s = strings.ToLower(s)
	switch {
	case strings.Contains(s, "running"):
		return "running"
	case strings.Contains(s, "shut off"):
		return "shutoff"
	case strings.Contains(s, "paused"):
		return "paused"
	}
	return "unknown"
}

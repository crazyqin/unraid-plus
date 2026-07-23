package handler

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/crazyqin/unraid-plus/server/internal/ssh"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

type vm struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Vcpus       int    `json:"vcpus"`
	MemoryBytes int64  `json:"memoryBytes"`
	Autostart   bool   `json:"autostart"`
	VNCPort     int    `json:"vncPort,omitempty"` // VNC display port (5900+N), 0 if not available
	Via         string `json:"via,omitempty"`     // "api" or "ssh"
}

// ListVMs returns VMs.
// v0.4+: Prefer Unraid HTTP API (VMMachines.php) with SSH fallback.
// The HTTP API returns HTML which we parse to extract VM data including VNC port.
func (h *Handler) ListVMs(c *gin.Context) {
	_, sid, ok := h.activeClientWithID(c)
	if !ok {
		return
	}

	// Try HTTP API first (API-only mode compatible)
	if h.ur.HasSession(sid) {
		vms, err := h.listVMsAPI(sid)
		if err == nil {
			for i := range vms {
				vms[i].Via = "api"
			}
			c.JSON(http.StatusOK, vms)
			return
		}
		logger.Debugf("vm api list failed, falling back to SSH: %v", err)
	}

	// SSH fallback
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}
	vms := h.listVMsSSH(cli)
	for i := range vms {
		vms[i].Via = "ssh"
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

// ---------------------------------------------------------------------------
// VM list via HTTP API (VMMachines.php HTML parsing)
// ---------------------------------------------------------------------------

// addVMCtxRe matches addVMContext() JS calls in the HTML.
// Example: addVMContext('Debian','51be9109-9d13-fb08-fe16-85fc24d77f8a','Debian','running','/plugins/dynamix.vm.manager/vnc.html?...&host=25432002.cn:8001&port=&path=/wsproxy/5701/','VNC','libvirt/qemu/Debian.log','QEMU','web;no','', '', )
// Parameters: name, uuid, displayName, state, vncUrl, vncType, logPath, emulator, keyboard, ?
var addVMCtxRe = regexp.MustCompile(
	`addVMContext\(\s*'([^']*)'\s*,` + // 1: name
		`\s*'([^']*)'\s*,` + // 2: uuid
		`\s*'([^']*)'\s*,` + // 3: display name
		`\s*'([^']*)'\s*,` + // 4: state (running/shut off/paused/etc.)
		`\s*'([^']*)'\s*,` + // 5: VNC URL
		`\s*'([^']*)'\s*,` + // 6: VNC type
		`\s*'([^']*)'\s*,` + // 7: log path
		`\s*'([^']*)'\s*,` + // 8: emulator
		`\s*'([^']*)'\s*` + // 9: keyboard mode
		`.*?\)`)

// vmVcpuRe matches vCPU count from HTML table cell.
// Example: <a class='vcpu-...' style='cursor:pointer'>8</a>
var vmVcpuRe = regexp.MustCompile(`class=['"]vcpu-[^'"]*['"][^>]*>(\d+)`)

// vmMemoryRe matches memory from HTML table cell.
// Example: <td>8G</td> — but only within VM rows, so we need context
// We'll parse vCPU and memory from table cells after the VM name cell.

// vmAutostartRe matches autostart checkbox.
// Example: <input class='autostart' type='checkbox' name='auto_Debian' uuid='51be9109...' >
var vmAutostartRe = regexp.MustCompile(`class=['"]autostart['"][^>]*uuid=['"]([^'"]+)['"][^>]*checked`)

// vmVNCPortRe matches VNC port info from HTML table cell.
// Example: VNC:5901 Driver:QXL
var vmVNCPortRe = regexp.MustCompile(`VNC:(\d+)`)

// vmWsproxyPortRe matches wsproxy port from VNC URL.
// Example: /wsproxy/5701/
var vmWsproxyPortRe = regexp.MustCompile(`/wsproxy/(\d+)/`)

// vmIconSrcRe matches VM icon image source.
// Example: <img src='/plugins/dynamix.vm.manager/templates/images/debian.png' class='img'>
var vmIconSrcRe = regexp.MustCompile(`<img\s+src=['"]([^'"]+)['"]\s+class=['"]img['"]`)

// listVMsAPI fetches VM list from Unraid HTTP API.
// Returns parsed VMs or an error if the API is unavailable.
func (h *Handler) listVMsAPI(sid string) ([]vm, error) {
	body, status, err := h.ur.VMMachines(sid)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("VMMachines.php returned HTTP %d", status)
	}

	html := string(body)

	// Quick check: if there's no addVMContext, VMs may not be installed
	if !strings.Contains(html, "addVMContext") {
		return []vm{}, nil
	}

	// Parse addVMContext calls
	ctxMatches := addVMCtxRe.FindAllStringSubmatch(html, -1)
	vms := make([]vm, 0, len(ctxMatches))

	// Build a map of UUID → autostart status
	autostartMap := map[string]bool{}
	for _, m := range vmAutostartRe.FindAllStringSubmatch(html, -1) {
		autostartMap[m[1]] = true
	}

	for i, m := range ctxMatches {
		name := m[1]
		uuid := m[2]
		state := m[4]
		vncURL := m[5]

		v := vm{
			ID:     uuid,
			Name:   name,
			Status: normalizeVMHTMLState(state),
		}

		// Extract VNC port from VNC URL or wsproxy path
		if port := vmWsproxyPortRe.FindStringSubmatch(vncURL); len(port) > 1 {
			v.VNCPort, _ = strconv.Atoi(port[1])
		}
		// Also check for VNC:port pattern in the HTML near this VM
		// (The HTML table has a cell with "VNC:5901 Driver:QXL")

		// Autostart
		v.Autostart = autostartMap[uuid]

		// vCPU and memory are in table cells — parse from HTML rows
		// This is a best-effort extraction; the SSH path provides more reliable data
		vms = append(vms, v)

		// Try to parse vCPU and memory from table rows
		_ = i // We'll use row-based parsing below
	}

	// Parse additional per-row data (vCPUs, memory, VNC port)
	// Split by <tr> to process each row
	vcpuMatches := vmVcpuRe.FindAllStringSubmatch(html, -1)
	for i, m := range vcpuMatches {
		if i < len(vms) {
			vms[i].Vcpus = atoiSafe(m[1], 0)
		}
	}

	// Parse memory: look for <td>NG</td> or <td>NM</td> patterns
	// This is tricky without proper HTML parsing; we do a simple regex scan
	vmMemoryMatches := regexp.MustCompile(`<td>(\d+)(G|M)</td>`).FindAllStringSubmatch(html, -1)
	vmMemIdx := 0
	for i := range vms {
		if vmMemIdx < len(vmMemoryMatches) {
			val := atoi64Safe(vmMemoryMatches[vmMemIdx][1])
			unit := vmMemoryMatches[vmMemIdx][2]
			switch unit {
			case "G":
				vms[i].MemoryBytes = val * 1024 * 1024 * 1024
			case "M":
				vms[i].MemoryBytes = val * 1024 * 1024
			}
			vmMemIdx++
		}
	}

	// Parse VNC port from table cells containing "VNC:5901"
	vncPortMatches := vmVNCPortRe.FindAllStringSubmatch(html, -1)
	for i, m := range vncPortMatches {
		if i < len(vms) && vms[i].VNCPort == 0 {
			vms[i].VNCPort, _ = strconv.Atoi(m[1])
		}
	}

	return vms, nil
}

// normalizeVMHTMLState converts Unraid's VM state text to our standard strings.
func normalizeVMHTMLState(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(s, "running"):
		return "running"
	case strings.Contains(s, "shut off") || strings.Contains(s, "stopped"):
		return "shutoff"
	case strings.Contains(s, "paused") || strings.Contains(s, "pmsuspended"):
		return "paused"
	}
	return "unknown"
}

// ---------------------------------------------------------------------------
// VM list via SSH (fallback)
// ---------------------------------------------------------------------------

// listVMsSSH fetches VM list via SSH virsh commands.
func (h *Handler) listVMsSSH(cli *ssh.Client) []vm {
	out, err := cli.Run(`virsh list --all --name 2>/dev/null`)
	if err != nil || strings.TrimSpace(out) == "" {
		return []vm{}
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
	return vms
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

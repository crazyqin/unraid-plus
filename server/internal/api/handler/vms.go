package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/crazyqin/unraid-plus/server/internal/ssh"
	"github.com/crazyqin/unraid-plus/server/internal/unraid"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

type vm struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Vcpus       int    `json:"vcpus"`
	MemoryBytes int64  `json:"memoryBytes"`
	Autostart   bool   `json:"autostart"`
	VNCPort     int    `json:"vncPort,omitempty"`
	Via         string `json:"via,omitempty"` // "graphql", "api" or "ssh"
}

// ListVMs returns VMs.
// v0.9+: GraphQL-first (official Unraid GraphQL API), SSH fallback for
// vCPU/memory details, HTML scraping as last-resort fallback.
func (h *Handler) ListVMs(c *gin.Context) {
	cli, sid, hasSSH, hasAPI := h.resolveServer(c)
	if sid == "" {
		return
	}

	// GraphQL-first
	if hasAPI && h.ur.HasGraphQL(sid) {
		vms, err := h.listVMsGraphQL(sid)
		if err == nil && vms != nil {
			// SSH enrichment for vCPU, memory, autostart (GraphQL may not have all details)
			if hasSSH && cli != nil {
				h.enrichVMsWithSSH(cli, vms)
			}
			for i := range vms {
				vms[i].Via = "graphql"
			}
			c.JSON(http.StatusOK, vms)
			return
		}
		logger.Debugf("vm graphql list failed for %s, falling back: %v", sid, err)
	}

	// SSH fallback
	if hasSSH && cli != nil {
		vms := h.listVMsSSH(cli)
		// Supplement VNC port from API when available
		if hasAPI {
			apiVMs, err := h.listVMsAPI(sid)
			if err == nil {
				vncMap := map[string]int{}
				for _, vm := range apiVMs {
					if vm.VNCPort > 0 {
						vncMap[vm.Name] = vm.VNCPort
					}
				}
				for i := range vms {
					if vms[i].VNCPort == 0 {
						if port, ok := vncMap[vms[i].Name]; ok {
							vms[i].VNCPort = port
						}
					}
				}
			}
		}
		for i := range vms {
			vms[i].Via = "ssh"
		}
		c.JSON(http.StatusOK, vms)
		return
	}

	// Last resort: HTML scraping
	if hasAPI {
		vms, err := h.listVMsAPI(sid)
		if err == nil {
			for i := range vms {
				vms[i].Via = "api"
			}
			c.JSON(http.StatusOK, vms)
			return
		}
	}

	c.JSON(http.StatusOK, []vm{})
}

// ---------------------------------------------------------------------------
// VM list via official Unraid GraphQL API
// ---------------------------------------------------------------------------

// listVMsGraphQL fetches VM list via the Unraid GraphQL API.
// Uses the ListVMs query which returns structured JSON data.
func (h *Handler) listVMsGraphQL(sid string) ([]vm, error) {
	data, err := h.ur.GraphQLQuery(sid, unraid.QueryListVMs, nil)
	if err != nil {
		return nil, fmt.Errorf("graphql vm query: %w", err)
	}

	gqlVMs, err := unraid.ParseVMsQuery(data)
	if err != nil {
		return nil, fmt.Errorf("parse vm graphql: %w", err)
	}

	if len(gqlVMs) == 0 {
		return []vm{}, nil
	}

	vms := make([]vm, 0)
	for _, gqlVM := range gqlVMs {
		for _, d := range gqlVM.Domains {
			vms = append(vms, vm{
				ID:     d.UUID,
				Name:   d.Name,
				Status: normalizeVMState(d.State),
			})
		}
	}

	return vms, nil
}

// enrichVMsWithSSH supplements GraphQL VM data with vCPU, memory, and autostart
// info from virsh dominfo (not available via GraphQL).
func (h *Handler) enrichVMsWithSSH(cli *ssh.Client, vms []vm) {
	for i := range vms {
		name := vms[i].Name
		if name == "" {
			name = vms[i].ID
		}
		if name == "" {
			continue
		}
		info, _ := cli.Run("virsh dominfo " + shellQuote(name) + " 2>/dev/null")
		if info == "" {
			continue
		}
		for _, line := range strings.Split(info, "\n") {
			kv := strings.SplitN(line, ":", 2)
			if len(kv) != 2 {
				continue
			}
			key := strings.TrimSpace(kv[0])
			val := strings.TrimSpace(kv[1])
			switch key {
			case "CPU(s)":
				vms[i].Vcpus = atoiSafe(val, 0)
			case "Max memory":
				fields := strings.Fields(val)
				if len(fields) > 0 {
					vms[i].MemoryBytes = atoi64Safe(fields[0]) * 1024
				}
			case "Autostart":
				vms[i].Autostart = val == "enable"
			}
		}
	}
}

// ---------------------------------------------------------------------------
// VM actions (start / stop / restart / pause / resume / suspend)
// ---------------------------------------------------------------------------

// VMAction starts / stops / restarts / pauses / resumes a VM.
// v0.9+: GraphQL mutation first, then PHP API (VMajax.php), then SSH fallback.
func (h *Handler) VMAction(c *gin.Context) {
	cli, sid, hasSSH, hasAPI := h.resolveServer(c)
	if sid == "" {
		return
	}
	id := c.Param("id")
	action := c.Param("action")

	// GraphQL mutation first
	if hasAPI && h.ur.HasGraphQL(sid) {
		if ok, via := h.vmActionGraphQL(sid, action, id); ok {
			c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已发送 " + action, "via": via})
			return
		}
	}

	// PHP API fallback (VMajax.php)
	apiActionMap := map[string]string{
		"start":    "domain-start",
		"stop":     "domain-destroy",
		"shutdown": "domain-stop",
		"resume":   "domain-resume",
		"suspend":  "domain-pause",
		"restart":  "domain-restart",
	}
	if apiAction, ok := apiActionMap[action]; ok && hasAPI {
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
	if hasSSH && cli != nil {
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
		return
	}

	errOut(c, http.StatusServiceUnavailable, "VM 操作不可用（GraphQL/API/SSH 均不可用）")
}

// vmActionGraphQL sends a VM action via GraphQL mutation.
func (h *Handler) vmActionGraphQL(sid, action, vmID string) (bool, string) {
	var query string
	var opName string
	switch action {
	case "start":
		query = unraid.MutStartVM
		opName = "StartVM"
	case "stop":
		query = unraid.MutStopVM
		opName = "StopVM"
	case "suspend":
		query = unraid.MutPauseVM
		opName = "PauseVM"
	case "resume":
		query = unraid.MutResumeVM
		opName = "ResumeVM"
	case "restart":
		query = unraid.MutRebootVM
		opName = "RebootVM"
	default:
		// shutdown, force-stop: not mapped to GraphQL yet
		return false, ""
	}

	vars := map[string]interface{}{
		"id": vmID,
	}
	data, err := h.ur.GraphQLQueryWithOp(sid, query, vars, opName)
	if err != nil {
		logger.Debugf("vm graphql action %s/%s failed: %v", action, vmID, err)
		return false, ""
	}

	if data == nil {
		return false, ""
	}

	// Verify mutation returned data
	var result map[string]json.RawMessage
	if json.Unmarshal(data, &result) == nil {
		return true, "graphql"
	}

	return false, ""
}

// ---------------------------------------------------------------------------
// VM list via HTTP API (VMMachines.php HTML parsing) -- fallback
// ---------------------------------------------------------------------------

// addVMCtxRe matches addVMContext() JS calls in the HTML.
var addVMCtxRe = regexp.MustCompile(
	`addVMContext\(\s*'([^']*)'\s*,` + // 1: name
		`\s*'([^']*)'\s*,` + // 2: uuid
		`\s*'([^']*)'\s*,` + // 3: display name
		`\s*'([^']*)'\s*,` + // 4: state
		`\s*'([^']*)'\s*,` + // 5: VNC URL
		`\s*'([^']*)'\s*,` + // 6: VNC type
		`\s*'([^']*)'\s*,` + // 7: log path
		`\s*'([^']*)'\s*,` + // 8: emulator
		`\s*'([^']*)'\s*` + // 9: keyboard mode
		`.*?\)`)

// vmVcpuRe matches vCPU count from HTML table cell.
var vmVcpuRe = regexp.MustCompile(`class=['"]vcpu-[^'"]*['"][^>]*>(\d+)`)

// vmAutostartRe matches autostart checkbox.
var vmAutostartRe = regexp.MustCompile(`class=['"]autostart['"][^>]*uuid=['"]([^'"]+)['"][^>]*checked`)

// vmVNCPortRe matches VNC port info from HTML table cell.
var vmVNCPortRe = regexp.MustCompile(`VNC:(\d+)`)

// vmWsproxyPortRe matches wsproxy port from VNC URL.
var vmWsproxyPortRe = regexp.MustCompile(`/wsproxy/(\d+)/`)

// listVMsAPI fetches VM list from Unraid HTTP API (HTML scraping).
// Last-resort fallback when GraphQL and SSH are both unavailable.
func (h *Handler) listVMsAPI(sid string) ([]vm, error) {
	body, status, err := h.ur.VMMachines(sid)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("VMMachines.php returned HTTP %d", status)
	}

	html := string(body)

	if !strings.Contains(html, "addVMContext") {
		return []vm{}, nil
	}

	ctxMatches := addVMCtxRe.FindAllStringSubmatch(html, -1)
	vms := make([]vm, 0, len(ctxMatches))

	autostartMap := map[string]bool{}
	for _, m := range vmAutostartRe.FindAllStringSubmatch(html, -1) {
		autostartMap[m[1]] = true
	}

	for _, m := range ctxMatches {
		name := m[1]
		uuid := m[2]
		state := m[4]
		vncURL := m[5]

		v := vm{
			ID:     uuid,
			Name:   name,
			Status: normalizeVMHTMLState(state),
		}

		if port := vmWsproxyPortRe.FindStringSubmatch(vncURL); len(port) > 1 {
			v.VNCPort, _ = strconv.Atoi(port[1])
		}

		v.Autostart = autostartMap[uuid]
		vms = append(vms, v)
	}

	// Parse vCPU from HTML rows
	vcpuMatches := vmVcpuRe.FindAllStringSubmatch(html, -1)
	for i, m := range vcpuMatches {
		if i < len(vms) {
			vms[i].Vcpus = atoiSafe(m[1], 0)
		}
	}

	// Parse memory: <td>NG</td> or <td>NM</td>
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

	// Parse VNC port from table cells
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
// VM list via SSH
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

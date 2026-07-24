package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
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
	Via         string `json:"via,omitempty"` // "graphql" or "ssh"
}

// ListVMs returns VMs.
// v0.10+: GraphQL-first (official Unraid GraphQL API), SSH as fallback.
// HTML scraping has been completely removed.
func (h *Handler) ListVMs(c *gin.Context) {
	cli, sid, hasSSH, hasAPI := h.prepareServer(c)
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
		logger.Warnf("vm graphql list failed for %s, falling back: %v", sid, err)
	}

	// SSH fallback
	if hasSSH && cli != nil {
		vms := h.listVMsSSH(cli)
		for i := range vms {
			vms[i].Via = "ssh"
		}
		c.JSON(http.StatusOK, vms)
		return
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
			// Prefer PrefixedID for GraphQL mutations; fall back to UUID/name for SSH.
			id := d.ID
			if id == "" {
				id = d.UUID
			}
			if id == "" {
				id = d.Name
			}
			vms = append(vms, vm{
				ID:     id,
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
			name = unraid.StripPrefixedID(vms[i].ID)
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
// v0.10+: GraphQL mutation first, then SSH fallback. HTML scraping removed.
func (h *Handler) VMAction(c *gin.Context) {
	cli, sid, hasSSH, hasAPI := h.prepareServer(c)
	if sid == "" {
		return
	}
	id := c.Param("id")
	action := c.Param("action")

	// GraphQL mutation first (expects PrefixedID)
	if hasAPI && h.ur.HasGraphQL(sid) {
		if ok, via := h.vmActionGraphQL(sid, action, id); ok {
			c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Action " + action + " sent", "via": via})
			return
		}
	}

	// SSH fallback — virsh accepts UUID or name; strip PrefixedID machine hash
	if hasSSH && cli != nil {
		virshID := unraid.StripPrefixedID(id)
		var cmd string
		switch action {
		case "start":
			cmd = "virsh start " + shellQuote(virshID)
		case "stop":
			cmd = "virsh destroy " + shellQuote(virshID)
		case "shutdown":
			cmd = "virsh shutdown " + shellQuote(virshID)
		case "resume":
			cmd = "virsh resume " + shellQuote(virshID)
		case "suspend":
			cmd = "virsh suspend " + shellQuote(virshID)
		default:
			errOut(c, http.StatusBadRequest, "Unsupported action: "+action)
			return
		}
		if _, err := cli.Run(cmd); err != nil {
			errOut(c, http.StatusInternalServerError, "virsh "+action+" failed")
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Action " + action + " sent", "via": "ssh"})
		return
	}

	errOut(c, http.StatusServiceUnavailable, "VM action unavailable (GraphQL/SSH both unavailable)")
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

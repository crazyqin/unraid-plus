package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/crazyqin/unraid-plus/server/internal/ssh"
	"github.com/crazyqin/unraid-plus/server/internal/unraid"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

type container struct {
	ID          string   `json:"id"`
	ShortID     string   `json:"shortId,omitempty"` // docker local id (no machine prefix) for stats match
	Name        string   `json:"name"`
	Image       string   `json:"image"`
	Icon        string   `json:"icon,omitempty"`    // base64-encoded PNG from Unraid state dir
	IconURL     string   `json:"iconUrl,omitempty"` // icon URL from Docker label
	Status      string   `json:"status"`           // machine keyword: running|exited|...
	State       string   `json:"state"`            // raw engine state
	StatusText  string   `json:"statusText,omitempty"` // human "Up 5 days (healthy)"
	CreatedAt   int64    `json:"createdAt"`
	StartedAt   int64    `json:"startedAt,omitempty"`
	Ports       []string `json:"ports"`
	Mounts      []mount  `json:"mounts"`
	AutoStart   bool     `json:"autoStart,omitempty"`
	NetworkMode string   `json:"networkMode,omitempty"`
	Command     string   `json:"command,omitempty"`
	Via         string   `json:"via,omitempty"` // "graphql" or "ssh"
}

type mount struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode"`
}

// ListContainers returns Docker containers.
// v0.10+: GraphQL-first (official Unraid GraphQL API), SSH as fallback.
// HTML scraping has been completely removed.
func (h *Handler) ListContainers(c *gin.Context) {
	cli, sid, hasSSH, hasAPI := h.prepareServer(c)
	if sid == "" {
		return
	}

	// GraphQL-first: use the official Unraid GraphQL API when available
	if hasAPI && h.ur.HasGraphQL(sid) {
		containers, err := h.listContainersGraphQL(sid)
		if err == nil && containers != nil {
			// Enrich with icons + inspect details from SSH when available
			if hasSSH && cli != nil {
				h.enrichContainerIcons(cli, containers)
				h.enrichContainersFromSSH(cli, containers)
			}
			for i := range containers {
				containers[i].Via = "graphql"
			}
			c.JSON(http.StatusOK, containers)
			return
		}
		logger.Warnf("docker graphql list failed for %s, falling back: %v", sid, err)
	}

	// SSH: docker ps gives rich data including icons
	if hasSSH && cli != nil {
		containers := h.listContainersSSH(cli)
		for i := range containers {
			containers[i].Via = "ssh"
		}
		c.JSON(http.StatusOK, containers)
		return
	}

	c.JSON(http.StatusOK, []container{})
}

// ---------------------------------------------------------------------------
// Docker list via official Unraid GraphQL API
// ---------------------------------------------------------------------------

// listContainersGraphQL fetches container list via the Unraid GraphQL API.
// Uses the ListDockerContainers query which returns structured JSON data
// (no HTML scraping needed).
func (h *Handler) listContainersGraphQL(sid string) ([]container, error) {
	data, err := h.ur.GraphQLQuery(sid, unraid.QueryListDockerContainers, nil)
	if err != nil {
		return nil, fmt.Errorf("graphql docker query: %w", err)
	}

	docker, err := unraid.ParseDockerQuery(data)
	if err != nil {
		return nil, fmt.Errorf("parse docker graphql: %w", err)
	}

	if len(docker.Containers) == 0 {
		return []container{}, nil
	}

	containers := make([]container, 0, len(docker.Containers))
	for _, gc := range docker.Containers {
		// Frontend uses `status` as the machine keyword (running/exited/...).
		// GraphQL `state` is RUNNING/EXITED; `status` is human text ("Up 5 days").
		st := normalizeDockerState(gc.State, gc.Status)
		localID := unraid.StripPrefixedID(gc.ID)
		ct := container{
			ID:         gc.ID, // PrefixedID for GraphQL mutations
			ShortID:    localID,
			Image:      gc.Image,
			State:      strings.ToLower(gc.State),
			Status:     st,
			StatusText: gc.Status,
			AutoStart:  gc.AutoStart,
			Command:    gc.Command,
			Ports:      []string{},
			Mounts:     []mount{},
		}
		if gc.HostConfig != nil {
			ct.NetworkMode = gc.HostConfig.NetworkMode
		}
		// created is unix seconds from official API
		if ts := gc.Created.Int64(); ts > 0 {
			ct.CreatedAt = ts
		}
		// Names: GraphQL returns an array like ["/FileBrowser"]
		if len(gc.Names) > 0 {
			ct.Name = strings.TrimPrefix(gc.Names[0], "/")
		}
		// Parse ports from GraphQL port structs (publicPort may be null → 0)
		for _, p := range gc.Ports {
			ptype := strings.ToLower(p.Type)
			if ptype == "" {
				ptype = "tcp"
			}
			var portStr string
			if p.PublicPort > 0 {
				ip := p.IP
				if ip == "" {
					ip = "0.0.0.0"
				}
				portStr = fmt.Sprintf("%s:%d→%d/%s", ip, p.PublicPort, p.PrivatePort, ptype)
			} else if p.PrivatePort > 0 {
				portStr = fmt.Sprintf("%d/%s", p.PrivatePort, ptype)
			} else {
				continue
			}
			ct.Ports = append(ct.Ports, portStr)
		}
		// Mounts are raw JSON from official API
		for _, m := range unraid.ParseDockerMounts(gc.Mounts) {
			ct.Mounts = append(ct.Mounts, mount{
				Source:      m.Source,
				Destination: m.Destination,
				Mode:        m.Mode,
			})
		}
		// Icon URL from Docker labels (if present)
		if gc.Labels != nil {
			if iconURL, ok := gc.Labels["net.unraid.docker.icon"]; ok && iconURL != "" {
				ct.IconURL = iconURL
			}
		}
		containers = append(containers, ct)
	}

	return containers, nil
}

// parseDockerCreated parses GraphQL created field (ISO datetime or unix seconds).
func parseDockerCreated(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		// seconds or milliseconds
		if n > 1e12 {
			return n / 1000
		}
		return n
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Unix()
		}
	}
	return 0
}

// enrichContainerIcons supplements GraphQL-fetched containers with base64-encoded
// icon data from the Unraid server's local filesystem (via SSH). The GraphQL API
// only provides the icon URL label, not the actual icon image data. We read the
// local PNG files via SSH and convert them to base64 data URIs for the frontend.
func (h *Handler) enrichContainerIcons(cli *ssh.Client, containers []container) {
	stateDir := "/usr/local/emhttp/state/plugins/dynamix.docker.manager/images"
	pluginDir := "/usr/local/emhttp/plugins/dynamix.docker.manager/images"

	var nameList []string
	for _, ct := range containers {
		name := strings.TrimPrefix(ct.Name, "/")
		if name != "" {
			nameList = append(nameList, shellQuote(name))
		}
	}
	if len(nameList) == 0 {
		return
	}

	iconCmd := `for n in ` + strings.Join(nameList, " ") + `; do ` +
		`f="` + stateDir + `/${n}-icon.png"; ` +
		`if [ -f "$f" ]; then echo "ICON:${n}:$(base64 -w0 "$f")"; continue; fi; ` +
		`f="` + pluginDir + `/${n}.png"; ` +
		`if [ -f "$f" ]; then echo "ICON:${n}:$(base64 -w0 "$f")"; continue; fi; ` +
		`done`
	iconOut, _ := cli.Run(iconCmd)
	iconMap := parseIconOutput(iconOut)
	for i := range containers {
		name := strings.TrimPrefix(containers[i].Name, "/")
		if b64, ok := iconMap[name]; ok {
			containers[i].Icon = "data:image/png;base64," + b64
		}
	}
}

// enrichContainersFromSSH fills gaps GraphQL leaves (startedAt, empty ports via
// docker inspect, network mode) using a single batch inspect when possible.
func (h *Handler) enrichContainersFromSSH(cli *ssh.Client, containers []container) {
	if cli == nil || len(containers) == 0 {
		return
	}
	// Build id list (local docker ids)
	ids := make([]string, 0, len(containers))
	indexByLocal := map[string]int{}
	for i, ct := range containers {
		lid := ct.ShortID
		if lid == "" {
			lid = unraid.StripPrefixedID(ct.ID)
		}
		if lid == "" {
			continue
		}
		// Use first 12 chars if full sha (docker accepts short ids)
		if len(lid) > 12 {
			lid = lid[:12]
		}
		ids = append(ids, lid)
		indexByLocal[lid] = i
		// also map full short id
		indexByLocal[unraid.StripPrefixedID(ct.ID)] = i
	}
	if len(ids) == 0 {
		return
	}

	// Batch inspect: Name|Created|StartedAt|NetworkMode|PortsJSON
	cmd := `docker inspect --format '{{.Id}}|{{.Name}}|{{.Created}}|{{if .State.StartedAt}}{{.State.StartedAt}}{{end}}|{{.HostConfig.NetworkMode}}|{{json .NetworkSettings.Ports}}' ` +
		strings.Join(ids, " ") + ` 2>/dev/null`
	out, err := cli.Run(cmd)
	if err != nil || strings.TrimSpace(out) == "" {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "|", 6)
		if len(parts) < 5 {
			continue
		}
		fullID := strings.TrimSpace(parts[0])
		name := strings.TrimPrefix(strings.TrimSpace(parts[1]), "/")
		created := strings.TrimSpace(parts[2])
		started := strings.TrimSpace(parts[3])
		netMode := strings.TrimSpace(parts[4])
		portsJSON := ""
		if len(parts) >= 6 {
			portsJSON = strings.TrimSpace(parts[5])
		}

		// Find matching container by local id prefix or name
		idx := -1
		for i, ct := range containers {
			local := unraid.StripPrefixedID(ct.ID)
			if local != "" && (strings.HasPrefix(fullID, local) || strings.HasPrefix(local, fullID[:min(12, len(fullID))])) {
				idx = i
				break
			}
			if ct.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			continue
		}
		if containers[idx].CreatedAt == 0 {
			if ts := parseDockerCreated(created); ts > 0 {
				containers[idx].CreatedAt = ts
			}
		}
		if ts := parseDockerCreated(started); ts > 0 {
			containers[idx].StartedAt = ts
		}
		if containers[idx].NetworkMode == "" && netMode != "" {
			containers[idx].NetworkMode = netMode
		}
		// Fill ports if GraphQL returned none
		if len(containers[idx].Ports) == 0 && portsJSON != "" && portsJSON != "null" && portsJSON != "{}" {
			containers[idx].Ports = parseInspectPorts(portsJSON)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// parseInspectPorts parses docker inspect NetworkSettings.Ports JSON into
// human port binding strings.
func parseInspectPorts(raw string) []string {
	// format: {"80/tcp":[{"HostIp":"0.0.0.0","HostPort":"8004"}], ...}
	var m map[string][]struct {
		HostIP   string `json:"HostIp"`
		HostPort string `json:"HostPort"`
	}
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	out := []string{}
	for priv, binds := range m {
		if len(binds) == 0 {
			out = append(out, priv)
			continue
		}
		for _, b := range binds {
			ip := b.HostIP
			if ip == "" {
				ip = "0.0.0.0"
			}
			out = append(out, fmt.Sprintf("%s:%s→%s", ip, b.HostPort, priv))
		}
	}
	return out
}

// parseIconOutput parses "ICON:name:base64data" lines from the batch icon read.
func parseIconOutput(out string) map[string]string {
	m := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ICON:") {
			continue
		}
		rest := line[5:] // strip "ICON:"
		idx := strings.Index(rest, ":")
		if idx < 0 {
			continue
		}
		name := rest[:idx]
		b64 := rest[idx+1:]
		if name != "" && b64 != "" {
			m[name] = b64
		}
	}
	return m
}

// ---------------------------------------------------------------------------
// Docker list via SSH
// ---------------------------------------------------------------------------

// listContainersSSH fetches container list via SSH `docker ps -a`.
func (h *Handler) listContainersSSH(cli *ssh.Client) []container {
	cmd := `docker ps -a --format '{{json .}}' 2>/dev/null`
	out, err := cli.Run(cmd)
	if err != nil && strings.TrimSpace(out) == "" {
		return []container{}
	}

	containers := []container{}
	type iconInfo struct {
		name    string
		iconURL string
	}
	var iconInfos []iconInfo

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ps struct {
			ID        string `json:"ID"`
			Names     string `json:"Names"`
			Image     string `json:"Image"`
			Status    string `json:"Status"`
			State     string `json:"State"`
			Running   string `json:"RunningFor"`
			Ports     string `json:"Ports"`
			CreatedAt string `json:"CreatedAt"`
		}
		if !unmarshalLooseJSON(line, &ps) {
			continue
		}
		st := strings.ToLower(ps.State)
		if st == "" {
			st = parseStatusFromStatus(ps.Status)
		}
		cName := strings.TrimPrefix(ps.Names, "/")
		containers = append(containers, container{
			ID:         ps.ID,
			ShortID:    ps.ID,
			Name:       cName,
			Image:      ps.Image,
			Status:     st,
			State:      ps.State,
			StatusText: ps.Status,
			CreatedAt:  parseDockerTime(ps.CreatedAt),
			StartedAt:  parseDockerTime(ps.CreatedAt),
			Ports:      splitPorts(ps.Ports),
		})
		iconInfos = append(iconInfos, iconInfo{name: cName})
	}

	// --- Icon resolution strategy ---
	if len(containers) > 0 && cli != nil {
		stateDir := "/usr/local/emhttp/state/plugins/dynamix.docker.manager/images"
		pluginDir := "/usr/local/emhttp/plugins/dynamix.docker.manager/images"

		var nameList []string
		for _, info := range iconInfos {
			if info.name != "" {
				nameList = append(nameList, shellQuote(info.name))
			}
		}
		if len(nameList) > 0 {
			iconCmd := `for n in ` + strings.Join(nameList, " ") + `; do ` +
				`f="` + stateDir + `/${n}-icon.png"; ` +
				`if [ -f "$f" ]; then echo "ICON:${n}:$(base64 -w0 "$f")"; continue; fi; ` +
				`f="` + pluginDir + `/${n}.png"; ` +
				`if [ -f "$f" ]; then echo "ICON:${n}:$(base64 -w0 "$f")"; continue; fi; ` +
				`done`
			iconOut, _ := cli.Run(iconCmd)
			iconMap := parseIconOutput(iconOut)
			for i := range containers {
				name := strings.TrimPrefix(containers[i].Name, "/")
				if b64, ok := iconMap[name]; ok {
					containers[i].Icon = "data:image/png;base64," + b64
				}
			}
		}

		// Get icon URL from Docker labels
		labelOut, _ := cli.Run(
			`docker inspect --format '{{.Name}}|{{index .Config.Labels "net.unraid.docker.icon"}}' ` +
				strings.Join(func() []string {
					ids := make([]string, 0, len(containers))
					for _, ct := range containers {
						ids = append(ids, ct.ID)
					}
					return ids
				}(), " ") + " 2>/dev/null",
		)
		if labelOut != "" {
			for _, line := range strings.Split(strings.TrimSpace(labelOut), "\n") {
				line = strings.TrimSpace(line)
				parts := strings.SplitN(line, "|", 2)
				if len(parts) != 2 {
					continue
				}
				cName := strings.TrimPrefix(parts[0], "/")
				iconURL := strings.TrimSpace(parts[1])
				if iconURL == "" {
					continue
				}
				for i := range containers {
					name := strings.TrimPrefix(containers[i].Name, "/")
					if name == cName && containers[i].Icon == "" {
						containers[i].IconURL = iconURL
					}
				}
			}
		}
	}

	return containers
}

// ---------------------------------------------------------------------------
// Docker actions (start / stop / restart / pause / unpause)
// ---------------------------------------------------------------------------

// ContainerAction starts / stops / restarts / pauses / unpauses a container.
// v0.10+: GraphQL mutation first, then SSH fallback. HTML scraping removed.
func (h *Handler) ContainerAction(c *gin.Context) {
	cli, sid, hasSSH, hasAPI := h.prepareServer(c)
	if sid == "" {
		return
	}
	id := c.Param("id")
	action := c.Param("action")
	switch action {
	case "start", "stop", "restart", "pause", "unpause":
	default:
		errOut(c, http.StatusBadRequest, "Unsupported action: "+action)
		return
	}

	// GraphQL mutation first (official API expects PrefixedID)
	if hasAPI && h.ur.HasGraphQL(sid) {
		if ok, via := h.dockerActionGraphQL(c, sid, action, id); ok {
			c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Action " + action + " sent", "via": via})
			return
		}
	}

	// SSH fallback — strip Unraid PrefixedID machine hash if present
	if hasSSH && cli != nil {
		dockerID := unraid.StripPrefixedID(id)
		if _, err := cli.Run("docker " + action + " " + shellQuote(dockerID)); err != nil {
			errOut(c, http.StatusInternalServerError, "docker "+action+" failed")
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Action " + action + " sent", "via": "ssh"})
		return
	}

	errOut(c, http.StatusServiceUnavailable, "Docker action unavailable (GraphQL/SSH both unavailable)")
}

// dockerActionGraphQL sends a Docker container action via GraphQL mutation.
// Returns (success, via) where via is "graphql" on success.
func (h *Handler) dockerActionGraphQL(c *gin.Context, sid, action, containerID string) (bool, string) {
	var query string
	var opName string
	switch action {
	case "start":
		query = unraid.MutStartContainer
		opName = "StartContainer"
	case "stop":
		query = unraid.MutStopContainer
		opName = "StopContainer"
	case "restart":
		query = unraid.MutRestartContainer
		opName = "RestartContainer"
	default:
		// pause/unpause not supported by GraphQL mutations yet
		return false, ""
	}

	vars := map[string]interface{}{
		"id": containerID,
	}
	data, err := h.ur.GraphQLQueryWithOp(sid, query, vars, opName)
	if err != nil {
		logger.Debugf("docker graphql action %s/%s failed: %v", action, containerID, err)
		return false, ""
	}

	// Verify the mutation returned data (not empty/null)
	if data == nil {
		return false, ""
	}

	// Quick check: did the mutation return a valid response?
	var result map[string]json.RawMessage
	if json.Unmarshal(data, &result) == nil {
		// Mutation succeeded if we got data back
		return true, "graphql"
	}

	return false, ""
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// normalizeDockerState maps GraphQL/engine state (+ optional human status)
// into the frontend keyword: running | exited | paused | restarting | created | dead.
func normalizeDockerState(state, humanStatus string) string {
	s := strings.ToLower(strings.TrimSpace(state))
	switch s {
	case "running", "exited", "paused", "restarting", "created", "dead", "removing":
		if s == "removing" {
			return "dead"
		}
		return s
	}
	// Fall back to human-readable status ("Up 5 days", "Exited (0) ...")
	return parseStatusFromStatus(humanStatus)
}

// parseStatusFromStatus extracts a normalized status keyword from
// docker's human-readable Status string.
func parseStatusFromStatus(s string) string {
	s = strings.ToLower(s)
	switch {
	case strings.HasPrefix(s, "up "):
		return "running"
	case strings.HasPrefix(s, "exited"):
		return "exited"
	case strings.HasPrefix(s, "paused"):
		return "paused"
	case strings.HasPrefix(s, "restarting"):
		return "restarting"
	case strings.HasPrefix(s, "created"):
		return "created"
	case strings.Contains(s, "dead"):
		return "dead"
	}
	return "unknown"
}

func splitPorts(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ", ") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// parseDockerTime parses docker's "2024-01-02 15:04:05 -0700 MST" format
// into a Unix seconds value. Best-effort: returns 0 on parse error.
func parseDockerTime(s string) int64 {
	for _, layout := range []string{
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05 -0700",
		time.RFC3339,
	} {
		if t, ok := tryParse(layout, s); ok {
			return t
		}
	}
	return 0
}

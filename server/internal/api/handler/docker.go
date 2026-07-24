package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/crazyqin/unraid-plus/server/internal/ssh"
	"github.com/crazyqin/unraid-plus/server/internal/unraid"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

type container struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Image     string   `json:"image"`
	Icon      string   `json:"icon,omitempty"`   // base64-encoded PNG from Unraid state dir
	IconURL   string   `json:"iconUrl,omitempty"` // icon URL from Docker label (fallback for client-side)
	Status    string   `json:"status"`
	State     string   `json:"state"`
	CreatedAt int64    `json:"createdAt"`
	StartedAt int64    `json:"startedAt,omitempty"`
	Ports     []string `json:"ports"`
	Mounts    []mount  `json:"mounts"`
	Via       string   `json:"via,omitempty"` // "graphql", "api" or "ssh"
}

type mount struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode"`
}

// ListContainers returns Docker containers.
// v0.9+: GraphQL-first (official Unraid GraphQL API), SSH for icon enrichment,
// HTML scraping as last-resort fallback.
func (h *Handler) ListContainers(c *gin.Context) {
	cli, sid, hasSSH, hasAPI := h.resolveServer(c)
	if sid == "" {
		return
	}

	// GraphQL-first: use the official Unraid GraphQL API when available
	if hasAPI && h.ur.HasGraphQL(sid) {
		containers, err := h.listContainersGraphQL(sid)
		if err == nil && containers != nil {
			// Enrich with icons from SSH when available
			if hasSSH && cli != nil {
				h.enrichContainerIcons(cli, containers)
			}
			for i := range containers {
				containers[i].Via = "graphql"
			}
			c.JSON(http.StatusOK, containers)
			return
		}
		logger.Debugf("docker graphql list failed for %s, falling back: %v", sid, err)
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

	// Last resort: HTML scraping via PHP API
	if hasAPI {
		containers, err := h.listContainersAPI(sid)
		if err == nil {
			for i := range containers {
				containers[i].Via = "api"
			}
			c.JSON(http.StatusOK, containers)
			return
		}
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
		ct := container{
			ID:     gc.ID,
			Image:  gc.Image,
			State:  strings.ToLower(gc.State),
			Status: strings.ToLower(gc.Status),
			Ports:  []string{},
			Mounts: []mount{},
		}
		// Names: GraphQL returns an array, docker ps returns "/name"
		if len(gc.Names) > 0 {
			ct.Name = gc.Names[0]
		}
		// Normalize state
		if ct.State == "" && ct.Status != "" {
			ct.State = ct.Status
		}
		// Parse ports from GraphQL port structs
		for _, p := range gc.Ports {
			portStr := ""
			if p.PublicPort > 0 {
				portStr = fmt.Sprintf("%s:%d->%d/%s", p.IP, p.PublicPort, p.PrivatePort, p.Type)
			} else {
				portStr = fmt.Sprintf("%d/%s", p.PrivatePort, p.Type)
			}
			ct.Ports = append(ct.Ports, portStr)
		}
		// Parse mounts from GraphQL mount structs
		for _, m := range gc.Mounts {
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
// Docker list via HTTP API (DockerContainers.php HTML parsing) -- fallback
// ---------------------------------------------------------------------------

// addDockerCtxRe matches addDockerContainerContext() JS calls in the HTML.
var addDockerCtxRe = regexp.MustCompile(
	`addDockerContainerContext\(\s*'([^']*)'\s*,` + // 1: name
		`\s*'([^']*)'\s*,` + // 2: id (short)
		`\s*'([^']*)'\s*,` + // 3: template path
		`\s*(\d+)\s*,` + // 4: running (1=running, 0=stopped)
		`\s*(\d+)\s*,` + // 5: ?
		`\s*(\d+)\s*,` + // 6: ?
		`\s*(true|false)\s*,` + // 7: autostart
		`\s*'([^']*)'\s*,` + // 8: ?
		`\s*'([^']*)'\s*,` + // 9: ?
		`\s*'([^']*)'\s*,` + // 10: shell type
		`\s*'([^']*)'\s*,` + // 11: container hash
		`\s*'([^']*)'\s*,` + // 12: ?
		`\s*'([^']*)'\s*,` + // 13: ?
		`\s*'([^']*)'\s*,` + // 14: ?
		`\s*'([^']*)'\s*` + // 15: icon URL
		`.*?\)`)

// dockerStateRe matches the state indicator in the HTML row.
var dockerStateRe = regexp.MustCompile(`<span\s+class=['"]state['"]\s*>([^<]+)</span>`)

// dockerAppnameRe matches the container name link.
var dockerAppnameRe = regexp.MustCompile(`<span\s+class=['"]appname\s*['"]\s*><a[^>]*>([^<]+)</a></span>`)

// dockerImageRe matches the image name from the container info div.
var dockerImageRe = regexp.MustCompile(`(?:来自|from):\s*([^\s<][^<]*)`)

// dockerIconSrcRe matches the icon image source.
var dockerIconSrcRe = regexp.MustCompile(`<img\s+src=['"]([^'"]+)['"]\s+class=['"]img['"]`)

// listContainersAPI fetches container list from Unraid HTTP API (HTML scraping).
// This is the last-resort fallback when GraphQL and SSH are both unavailable.
func (h *Handler) listContainersAPI(sid string) ([]container, error) {
	body, status, err := h.ur.DockerContainers(sid)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("DockerContainers.php returned HTTP %d", status)
	}

	html := string(body)

	if !strings.Contains(html, "addDockerContainerContext") {
		return []container{}, nil
	}

	ctxMatches := addDockerCtxRe.FindAllStringSubmatch(html, -1)
	containers := make([]container, 0, len(ctxMatches))

	for _, m := range ctxMatches {
		name := m[1]
		id := m[2]
		state := "exited"
		statusStr := "exited"
		if m[4] == "1" {
			state = "running"
			statusStr = "running"
		}
		iconURL := m[15]

		ct := container{
			ID:      id,
			Name:    name,
			Status:  statusStr,
			State:   state,
			IconURL: iconURL,
			Ports:   []string{},
			Mounts:  []mount{},
		}
		containers = append(containers, ct)
	}

	// Supplement with HTML row data (image name, state text, icon)
	rows := parseDockerHTMLRows(html)
	for i := range containers {
		if info, ok := rows[containers[i].Name]; ok {
			if containers[i].Image == "" {
				containers[i].Image = info.image
			}
			if info.state != "" && containers[i].State == "" {
				containers[i].State = info.state
				containers[i].Status = info.state
			}
			if containers[i].IconURL == "" && info.iconSrc != "" {
				sess := h.ur.GetSession(sid)
				if sess != "" {
					containers[i].IconURL = sess + info.iconSrc
				}
			}
		}
	}

	return containers, nil
}

// dockerRowInfo holds parsed data from an HTML table row.
type dockerRowInfo struct {
	image   string
	state   string
	icon    string
	iconSrc string
}

// parseDockerHTMLRows extracts container data from HTML <tr> rows.
func parseDockerHTMLRows(html string) map[string]*dockerRowInfo {
	rows := map[string]*dockerRowInfo{}

	appMatches := dockerAppnameRe.FindAllStringSubmatch(html, -1)
	names := make([]string, 0, len(appMatches))
	for _, m := range appMatches {
		name := strings.TrimSpace(m[1])
		names = append(names, name)
		if name != "" {
			rows[name] = &dockerRowInfo{}
		}
	}

	stateMatches := dockerStateRe.FindAllStringSubmatch(html, -1)
	for i, m := range stateMatches {
		state := strings.TrimSpace(m[1])
		st := normalizeDockerHTMLState(state)
		if i < len(names) && names[i] != "" {
			if rows[names[i]] == nil {
				rows[names[i]] = &dockerRowInfo{}
			}
			rows[names[i]].state = st
		}
	}

	imageMatches := dockerImageRe.FindAllStringSubmatch(html, -1)
	for i, m := range imageMatches {
		image := strings.TrimSpace(m[1])
		if image == "" {
			continue
		}
		if i < len(names) && names[i] != "" {
			if rows[names[i]] == nil {
				rows[names[i]] = &dockerRowInfo{}
			}
			rows[names[i]].image = image
		}
	}

	iconMatches := dockerIconSrcRe.FindAllStringSubmatch(html, -1)
	for i, m := range iconMatches {
		src := strings.TrimSpace(m[1])
		if idx := strings.Index(src, "?"); idx >= 0 {
			src = src[:idx]
		}
		if src == "" {
			continue
		}
		if i < len(names) && names[i] != "" {
			if rows[names[i]] == nil {
				rows[names[i]] = &dockerRowInfo{}
			}
			rows[names[i]].iconSrc = src
		}
	}

	return rows
}

// normalizeDockerHTMLState converts Unraid's localized state text to standard strings.
func normalizeDockerHTMLState(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(s, "started") || strings.Contains(s, "running") || strings.Contains(s, "up"):
		return "running"
	case strings.Contains(s, "stopped") || strings.Contains(s, "exited"):
		return "exited"
	case strings.Contains(s, "paused"):
		return "paused"
	case strings.Contains(s, "created"):
		return "created"
	case strings.Contains(s, "restarting"):
		return "restarting"
	}
	return s
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
			ID:        ps.ID,
			Name:      ps.Names,
			Image:     ps.Image,
			Status:    st,
			State:     ps.State,
			CreatedAt: parseDockerTime(ps.CreatedAt),
			StartedAt: parseDockerTime(ps.CreatedAt),
			Ports:     splitPorts(ps.Ports),
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
// v0.9+: GraphQL mutation first, then PHP API, then SSH fallback.
func (h *Handler) ContainerAction(c *gin.Context) {
	cli, sid, hasSSH, hasAPI := h.resolveServer(c)
	if sid == "" {
		return
	}
	id := c.Param("id")
	action := c.Param("action")
	switch action {
	case "start", "stop", "restart", "pause", "unpause":
	default:
		errOut(c, http.StatusBadRequest, "不支持的操作: "+action)
		return
	}

	// GraphQL mutation first (official API)
	if hasAPI && h.ur.HasGraphQL(sid) {
		if ok, via := h.dockerActionGraphQL(c, sid, action, id); ok {
			c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已发送 " + action, "via": via})
			return
		}
	}

	// PHP API fallback (Events.php)
	if hasAPI {
		resp, err := h.ur.DockerActionOK(sid, action, id)
		if err == nil && resp != nil && resp.Success {
			c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已发送 " + action, "via": "api"})
			return
		}
		if err != nil {
			logger.Debugf("docker api action %s/%s failed, falling back to SSH: %v", action, id, err)
		}
	}

	// SSH fallback
	if hasSSH && cli != nil {
		if _, err := cli.Run("docker " + action + " " + shellQuote(id)); err != nil {
			errOut(c, http.StatusInternalServerError, "执行 docker "+action+" 失败")
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已发送 " + action, "via": "ssh"})
		return
	}

	errOut(c, http.StatusServiceUnavailable, "Docker 操作不可用（GraphQL/API/SSH 均不可用）")
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

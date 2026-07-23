package handler

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/crazyqin/unraid-plus/server/internal/ssh"
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
	Via       string   `json:"via,omitempty"` // "api" or "ssh" — which channel provided the data
}

type mount struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode"`
}

// ListContainers returns Docker containers.
// v0.4+: Prefer Unraid HTTP API (DockerContainers.php) with SSH fallback.
// The HTTP API returns HTML which we parse to extract container data.
// SSH fallback uses `docker ps -a --format ...` for richer/structured data.
func (h *Handler) ListContainers(c *gin.Context) {
	_, sid, ok := h.activeClientWithID(c)
	if !ok {
		return
	}

	// Try HTTP API first (API-only mode compatible)
	if h.ur.HasSession(sid) {
		containers, err := h.listContainersAPI(sid)
		if err == nil && len(containers) >= 0 {
			// API path succeeded (even 0 containers is valid — Docker not installed)
			for i := range containers {
				containers[i].Via = "api"
			}
			c.JSON(http.StatusOK, containers)
			return
		}
		logger.Debugf("docker api list failed, falling back to SSH: %v", err)
	}

	// SSH fallback
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}
	containers := h.listContainersSSH(cli)
	for i := range containers {
		containers[i].Via = "ssh"
	}
	c.JSON(http.StatusOK, containers)
}

// parseIconOutput parses "ICON:name:base64data" lines from the batch icon read.
func parseIconOutput(out string) map[string]string {
	m := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ICON:") {
			continue
		}
		// Format: ICON:container-name:base64data
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
// Docker list via HTTP API (DockerContainers.php HTML parsing)
// ---------------------------------------------------------------------------

// addDockerCtxRe matches addDockerContainerContext() JS calls in the HTML.
// Example: addDockerContainerContext('cd2','8700ac35dbd1','/boot/config/plugins/dockerMan/templates-user/my-cd2.xml',1,0,3,true,'','','sh','c7422809893b','','','', '','')
// Parameters: name, id, template, running(1/0), ?, ?, autostart(bool), ?, ?, shell, containerHash, ?, ?, ?, iconUrl, ?
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
// Example: <span class='state'>已启动</span> or <span class='state'>Stopped</span>
var dockerStateRe = regexp.MustCompile(`<span\s+class=['"]state['"]\s*>([^<]+)</span>`)

// dockerAppnameRe matches the container name link.
// Example: <span class='appname'><a class='exec' ...>cd2</a></span>
var dockerAppnameRe = regexp.MustCompile(`<span\s+class=['"]appname['"]\s*><a[^>]*>([^<]+)</a></span>`)

// dockerImageRe matches the image name from the HTML table.
// Example: <td class='ct-image'>crazyqin/cd2:latest</td>
var dockerImageRe = regexp.MustCompile(`<td\s+class=['"]ct-image['"]\s*>([^<]+)</td>`)

// dockerIconSrcRe matches the icon image source.
// Example: <img src='/plugins/dynamix.docker.manager/images/question.png?1700089733' class='img' ...>
var dockerIconSrcRe = regexp.MustCompile(`<img\s+src=['"]([^'"]+)['"]\s+class=['"]img['"]`)

// listContainersAPI fetches container list from Unraid HTTP API.
// Returns parsed containers or an error if the API is unavailable.
func (h *Handler) listContainersAPI(sid string) ([]container, error) {
	body, status, err := h.ur.DockerContainers(sid)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("DockerContainers.php returned HTTP %d", status)
	}

	html := string(body)

	// Quick check: if there's no addDockerContainerContext, Docker may not be installed
	if !strings.Contains(html, "addDockerContainerContext") {
		// Could be empty page (no containers) or Docker not installed
		return []container{}, nil
	}

	// Parse addDockerContainerContext calls for structured data
	ctxMatches := addDockerCtxRe.FindAllStringSubmatch(html, -1)
	containers := make([]container, 0, len(ctxMatches))

	for _, m := range ctxMatches {
		name := m[1]
		id := m[2]
		// m[4]: running flag (1=running, 0=stopped)
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
		}
		containers = append(containers, ct)
	}

	// Supplement with HTML row data (image name, state text)
	// Parse HTML rows to extract additional info
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
		}
	}

	return containers, nil
}

// dockerRowInfo holds parsed data from an HTML table row.
type dockerRowInfo struct {
	image string
	state string
	icon  string
}

// parseDockerHTMLRows extracts container data from HTML <tr> rows.
// Returns a map keyed by container name.
func parseDockerHTMLRows(html string) map[string]*dockerRowInfo {
	rows := map[string]*dockerRowInfo{}

	// Parse appname (container name)
	appMatches := dockerAppnameRe.FindAllStringSubmatch(html, -1)
	names := make([]string, 0, len(appMatches))
	for _, m := range appMatches {
		name := strings.TrimSpace(m[1])
		names = append(names, name)
		if name != "" {
			rows[name] = &dockerRowInfo{}
		}
	}

	// Parse state text
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

	// Parse image name
	imageMatches := dockerImageRe.FindAllStringSubmatch(html, -1)
	for i, m := range imageMatches {
		image := strings.TrimSpace(m[1])
		if i < len(names) && names[i] != "" {
			if rows[names[i]] == nil {
				rows[names[i]] = &dockerRowInfo{}
			}
			rows[names[i]].image = image
		}
	}

	return rows
}

// normalizeDockerHTMLState converts Unraid's localized state text to our
// standard state strings.
func normalizeDockerHTMLState(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(s, "started") || strings.Contains(s, "启动") || strings.Contains(s, "running") || strings.Contains(s, "up"):
		return "running"
	case strings.Contains(s, "stopped") || strings.Contains(s, "停止") || strings.Contains(s, "exited"):
		return "exited"
	case strings.Contains(s, "paused") || strings.Contains(s, "暂停"):
		return "paused"
	case strings.Contains(s, "created") || strings.Contains(s, "created"):
		return "created"
	case strings.Contains(s, "restarting") || strings.Contains(s, "重启"):
		return "restarting"
	}
	return s
}

// ---------------------------------------------------------------------------
// Docker list via SSH (fallback)
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

		// Get icon URL from Docker labels for containers without local icons
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

// ContainerAction starts / stops / restarts / pauses a container.
// v0.3+: Prefer Unraid HTTP API (Events.php) with SSH fallback.
func (h *Handler) ContainerAction(c *gin.Context) {
	_, sid, ok := h.activeClientWithID(c)
	if !ok {
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

	// Try Unraid HTTP API first
	if h.ur.HasSession(sid) {
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
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}
	if _, err := cli.Run("docker " + action + " " + shellQuote(id)); err != nil {
		errOut(c, http.StatusInternalServerError, "执行 docker "+action+" 失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已发送 " + action, "via": "ssh"})
}

// parseStatusFromStatus extracts a normalized status keyword from
// docker's human-readable Status string ("Up 2 hours", "Exited (0) 3 days ago").
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

// tryParse / unmarshalLooseJSON / shellQuote are defined in helpers.go.

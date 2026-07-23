package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

type container struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Image     string   `json:"image"`
	Icon      string   `json:"icon,omitempty"` // base64-encoded PNG from Unraid
	Status    string   `json:"status"`
	State     string   `json:"state"`
	CreatedAt int64    `json:"createdAt"`
	StartedAt int64    `json:"startedAt,omitempty"`
	Ports     []string `json:"ports"`
	Mounts    []mount  `json:"mounts"`
}

type mount struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode"`
}

// ListContainers returns Docker containers via `docker ps -a --format ...`.
// We use a JSON-per-line format that's trivial to parse and stable across
// Docker versions. Container icons are read from Unraid's plugin image
// directory and returned as base64-encoded PNGs.
func (h *Handler) ListContainers(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}

	cmd := `docker ps -a --format '{{json .}}' 2>/dev/null`
	out, err := cli.Run(cmd)
	if err != nil && strings.TrimSpace(out) == "" {
		// Docker not installed or daemon down.
		c.JSON(http.StatusOK, []container{})
		return
	}

	containers := []container{}
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
	}

	// Fetch container icons from Unraid's image directory.
	// Icons are stored as <container-name>.png in the Docker plugin's state dir.
	// We batch-read them in one SSH command for efficiency.
	// Multiple possible locations across Unraid versions.
	if len(containers) > 0 && cli != nil {
		// Build a command that:
		// 1. Detects which icon directory exists
		// 2. Reads each icon and outputs "ICON:name:base64"
		// Uses semicolons instead of newlines to avoid shell parsing issues.
		var nameList []string
		for _, ct := range containers {
			name := strings.TrimPrefix(ct.Name, "/")
			if name != "" {
				nameList = append(nameList, shellQuote(name))
			}
		}
		if len(nameList) > 0 {
			// Probe icon directories in priority order, then read all found icons
			iconCmd := `ICONDIR="";` +
				`for d in /usr/local/emhttp/state/plugins/dynamix.docker.manager/images /boot/config/plugins/dynamix.docker.manager/images /usr/local/emhttp/plugins/dynamix.docker.manager/images; do ` +
				`if [ -d "$d" ]; then ICONDIR="$d"; break; fi; done; ` +
				`if [ -n "$ICONDIR" ]; then ` +
				`for n in ` + strings.Join(nameList, " ") + `; do ` +
				`f="${ICONDIR}/${n}.png"; ` +
				`if [ -f "$f" ]; then echo "ICON:${n}:$(base64 -w0 "$f")"; fi; ` +
				`done; fi`
			iconOut, _ := cli.Run(iconCmd)
			iconMap := parseIconOutput(iconOut)
			for i := range containers {
				name := strings.TrimPrefix(containers[i].Name, "/")
				if b64, ok := iconMap[name]; ok {
					containers[i].Icon = "data:image/png;base64," + b64
				}
			}
		}
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

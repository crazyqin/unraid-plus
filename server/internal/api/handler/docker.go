package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type container struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Image     string   `json:"image"`
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
// Docker versions.
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

	c.JSON(http.StatusOK, containers)
}

// ContainerAction starts / stops / restarts / pauses a container.
func (h *Handler) ContainerAction(c *gin.Context) {
	cli, ok := h.activeClient(c)
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
	if _, err := cli.Run("docker " + action + " " + shellQuote(id)); err != nil {
		errOut(c, http.StatusInternalServerError, "执行 docker "+action+" 失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已发送 " + action})
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

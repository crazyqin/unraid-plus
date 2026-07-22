package handler

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// containerStats holds resource usage for a single container.
type containerStats struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	CPUPct    float64 `json:"cpuPct"`
	MemUsage  int64   `json:"memUsageBytes"`
	MemLimit  int64   `json:"memLimitBytes"`
	MemPct    float64 `json:"memPct"`
	NetRx     int64   `json:"netRxBytes"`
	NetTx     int64   `json:"netTxBytes"`
	BlockRead int64   `json:"blockReadBytes"`
	BlockWr   int64   `json:"blockWriteBytes"`
	PIDs      int     `json:"pids"`
}

// statsCache avoids running `docker stats --no-stream` on every poll.
// docker stats takes ~1-2s even with --no-stream because it samples
// cgroup data over a 1s window. With a 2-5s UI poll, caching for 3s
// keeps the data fresh without hammering the Docker daemon.
var statsCache = struct {
	sync.RWMutex
	m    map[string]containerStats
	ts   time.Time
}{m: map[string]containerStats{}}

const statsCacheTTL = 3 * time.Second

// DockerStats returns resource usage for all running containers.
//
// Implementation: `docker stats --no-stream --format '{{json .}}'` gives
// one JSON line per container with fields:
//   CPUPerc ("0.50%"), MemUsage ("128MiB / 4GiB"), MemPerc ("3.20%"),
//   NetIO ("1.2kB / 3.4kB"), BlockIO ("4kB / 8kB"), PIDs ("12")
//
// We parse these human-readable strings (Docker doesn't provide raw bytes
// in the --format output) into structured numeric fields for the UI.
//
// Exit code 1 + empty output usually means the Docker daemon is down or
// no containers are running — we return an empty array in that case.
func (h *Handler) DockerStats(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}

	// Check cache first.
	statsCache.RLock()
	if time.Since(statsCache.ts) < statsCacheTTL && len(statsCache.m) > 0 {
		out := make([]containerStats, 0, len(statsCache.m))
		for _, s := range statsCache.m {
			out = append(out, s)
		}
		statsCache.RUnlock()
		c.JSON(http.StatusOK, out)
		return
	}
	statsCache.RUnlock()

	out, err := cli.Run(`docker stats --no-stream --format '{{json .}}' 2>/dev/null`)
	if err != nil && strings.TrimSpace(out) == "" {
		c.JSON(http.StatusOK, []containerStats{})
		return
	}

	stats := []containerStats{}
	newCache := map[string]containerStats{}

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw struct {
			Container string `json:"Container"`
			Name      string `json:"Name"`
			CPUPerc   string `json:"CPUPerc"`
			MemUsage  string `json:"MemUsage"`
			MemPerc   string `json:"MemPerc"`
			NetIO     string `json:"NetIO"`
			BlockIO   string `json:"BlockIO"`
			PIDs      string `json:"PIDs"`
		}
		if !unmarshalLooseJSON(line, &raw) {
			continue
		}

		memUsed, memLimit := parseMemUsage(raw.MemUsage)
		netRx, netTx := parseNetIO(raw.NetIO)
		blkRd, blkWr := parseBlockIO(raw.BlockIO)

		s := containerStats{
			ID:        raw.Container,
			Name:      raw.Name,
			CPUPct:    parsePercent(raw.CPUPerc),
			MemUsage:  memUsed,
			MemLimit:  memLimit,
			MemPct:    parsePercent(raw.MemPerc),
			NetRx:     netRx,
			NetTx:     netTx,
			BlockRead: blkRd,
			BlockWr:   blkWr,
			PIDs:      atoiSafe(raw.PIDs, 0),
		}
		stats = append(stats, s)
		newCache[raw.Container] = s
	}

	// Update cache.
	statsCache.Lock()
	statsCache.m = newCache
	statsCache.ts = time.Now()
	statsCache.Unlock()

	if stats == nil {
		stats = []containerStats{}
	}
	c.JSON(http.StatusOK, stats)
}

// parsePercent parses "0.50%" → 0.50.
func parsePercent(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	return atofSafe(s)
}

// parseMemUsage parses "128MiB / 4GiB" → (128*1024*1024, 4*1024*1024*1024).
// Docker always uses binary suffixes (KiB, MiB, GiB, TiB) in stats output.
// We also handle the edge case of "0B / 0B" (container just started).
func parseMemUsage(s string) (used, limit int64) {
	parts := strings.Split(s, " / ")
	if len(parts) != 2 {
		return 0, 0
	}
	return parseDockerSize(parts[0]), parseDockerSize(parts[1])
}

// parseNetIO parses "1.2kB / 3.4kB" → (1200, 3400).
// Docker uses SI suffixes (kB, MB, GB) for network — base 1000, not 1024.
func parseNetIO(s string) (rx, tx int64) {
	parts := strings.Split(s, " / ")
	if len(parts) != 2 {
		return 0, 0
	}
	return parseDockerSizeSI(parts[0]), parseDockerSizeSI(parts[1])
}

// parseBlockIO parses "4kB / 8kB" → (4000, 8000).
func parseBlockIO(s string) (read, write int64) {
	parts := strings.Split(s, " / ")
	if len(parts) != 2 {
		return 0, 0
	}
	return parseDockerSizeSI(parts[0]), parseDockerSizeSI(parts[1])
}

// parseDockerSize parses binary-suffixed sizes (KiB, MiB, GiB, TiB, B).
// "128MiB" → 128 * 1024 * 1024. "0B" → 0. Falls back to 0 on parse error.
func parseDockerSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "0B" {
		return 0
	}

	var mult int64 = 1
	switch {
	case strings.HasSuffix(s, "TiB"):
		mult = 1 << 40
		s = strings.TrimSuffix(s, "TiB")
	case strings.HasSuffix(s, "GiB"):
		mult = 1 << 30
		s = strings.TrimSuffix(s, "GiB")
	case strings.HasSuffix(s, "MiB"):
		mult = 1 << 20
		s = strings.TrimSuffix(s, "MiB")
	case strings.HasSuffix(s, "KiB"):
		mult = 1 << 10
		s = strings.TrimSuffix(s, "KiB")
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	}
	return int64(atofSafe(s) * float64(mult))
}

// parseDockerSizeSI parses SI-suffixed sizes (kB, MB, GB, TB, B).
// "1.2kB" → 1200 (base 1000). Falls back to parseDockerSize on unknown suffix.
func parseDockerSizeSI(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "0B" {
		return 0
	}

	var mult int64 = 1
	switch {
	case strings.HasSuffix(s, "TB"):
		mult = 1e12
		s = strings.TrimSuffix(s, "TB")
	case strings.HasSuffix(s, "GB"):
		mult = 1e9
		s = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "MB"):
		mult = 1e6
		s = strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "kB"):
		mult = 1e3
		s = strings.TrimSuffix(s, "kB")
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	}
	return int64(atofSafe(s) * float64(mult))
}

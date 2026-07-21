package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/your-org/unraidpp/server/internal/ssh"
)

type disk struct {
	Device           string     `json:"device"`
	Name             string     `json:"name"`
	FsType           string     `json:"fsType"`
	SizeBytes        int64      `json:"sizeBytes"`
	UsedBytes        int64      `json:"usedBytes"`
	TempC            *int       `json:"tempC,omitempty"`
	ReadBytesPerSec  int64      `json:"readBytesPerSec"`
	WriteBytesPerSec int64      `json:"writeBytesPerSec"`
	Errors           int        `json:"errors"`
	Status           string     `json:"status"`
	// Smart holds the structured SMART health data when smartctl is
	// available and the device supports SMART. nil for software raid
	// (md*), loop, zfs vdevs, USB bridges without SAT, or when smartctl
	// is not installed on the host. The frontend should render a SMART
	// detail panel only when Smart != nil && Smart.Available.
	Smart *smartInfo `json:"smart,omitempty"`
}

type arrayStatus struct {
	State     string `json:"state"`
	Disks     []disk `json:"disks"`
	CacheDisks []disk `json:"cacheDisks"`
}

// Storage returns a coarse view of the array + cache disks. For v0.x we rely
// on `df` and `/proc/diskstats`; SMART/temperature data is best-effort.
// Production will plug into Unraid's own `emcmd` / `disk.sh` for accurate
// array status, but `df` is enough for "is my disk full?" at a glance.
//
// v0.3 also probes smartctl per physical disk (see smart.go) and surfaces:
//   - disk.TempC     ← on-disk sensor (more reliable than thermal_zone)
//   - disk.Errors    ← sum of reallocated + pending + uncorrectable + media
//   - disk.Smart     ← structured SMART health data for the detail panel
//
// SMART probing is cached process-wide for 30s (smartCache) so the 5s UI
// poll doesn't fork smartctl on every request — see smart.go for rationale.
// Software raid (md*), loop, and zfs vdevs are skipped via baseDevName.
func (h *Handler) Storage(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}

	dfOut, _ := cli.Run(`df -PT 2>/dev/null | awk 'NR>1{print $1"|"$2"|"$3"|"$4"|"$7}'`)
	arrayState := "started"
	if out, _ := cli.Run("mdcmd status 2>/dev/null | head -n1"); strings.TrimSpace(out) != "" {
		arrayState = strings.TrimSpace(out)
	}

	disks, cache := parseDf(dfOut)
	enrichWithSmart(cli, disks)
	enrichWithSmart(cli, cache)

	c.JSON(http.StatusOK, arrayStatus{
		State:      arrayState,
		Disks:      disks,
		CacheDisks: cache,
	})
}

// enrichWithSmart probes SMART for each disk in-place. The probe is cached
// per base device name (smartCache), so calling this on every poll is cheap
// after the first hit. Disks whose device path doesn't map to a real block
// device (md*, loop, zfs vdevs) are left untouched.
func enrichWithSmart(cli *ssh.Client, disks []disk) {
	for i := range disks {
		base := baseDevName(disks[i].Device)
		if base == "" {
			continue
		}
		info := fetchSmart(cli, base)
		if !info.Available {
			continue
		}
		disks[i].Smart = &info
		if info.Temperature != nil {
			disks[i].TempC = info.Temperature
		}
		disks[i].Errors = info.Reallocated + info.Pending +
			info.Uncorrectable + info.MediaErrors
	}
}

// parseDf turns the pipe-separated df output into disk entries.
// Mounts under /mnt/disk* are array disks; under /mnt/cache* are cache.
func parseDf(s string) (array []disk, cache []disk) {
	for _, line := range strings.Split(s, "\n") {
		f := strings.Split(line, "|")
		if len(f) < 5 {
			continue
		}
		dev, fsType, used, avail, mount := f[0], f[1], atoi64Safe(f[2])*1024, atoi64Safe(f[3])*1024, f[4]
		size := used + avail
		if size <= 0 {
			continue
		}
		d := disk{
			Device:    dev,
			Name:      mount,
			FsType:    fsType,
			SizeBytes: size,
			UsedBytes: used,
			Status:    diskStatus(used, size),
		}
		switch {
		case strings.HasPrefix(mount, "/mnt/cache"):
			cache = append(cache, d)
		case strings.HasPrefix(mount, "/mnt/disk"):
			array = append(array, d)
		}
	}
	return array, cache
}

// diskStatus returns "warning" / "critical" / "ok" based on fill ratio.
func diskStatus(used, size int64) string {
	if size <= 0 {
		return "unknown"
	}
	pct := float64(used) / float64(size)
	switch {
	case pct >= 0.95:
		return "critical"
	case pct >= 0.85:
		return "warning"
	default:
		return "ok"
	}
}

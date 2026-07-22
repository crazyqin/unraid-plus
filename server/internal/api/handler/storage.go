package handler

import (
	"net/http"
	"strings"
	"time"

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
// v0.4 computes per-disk read/write byte rates from /proc/diskstats deltas
// (previously always 0). See enrichWithRW.
//
// SMART probing is cached process-wide for 30s (smartCache) so the 5s UI
// poll doesn't fork smartctl on every request — see smart.go for rationale.
// Software raid (md*), loop, and zfs vdevs are skipped via baseDevName.
func (h *Handler) Storage(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}

	dfOut, _ := cli.Run(`df -PT 2>/dev/null | awk 'NR>1{print $1"|"$2"|"$4"|"$5"|"$7}'`)
	arrayState := "started"
	if out, _ := cli.Run("mdcmd status 2>/dev/null | head -n1"); strings.TrimSpace(out) != "" {
		arrayState = strings.TrimSpace(out)
	}

	disks, cache := parseDf(dfOut)
	// enrichWithRW must run before enrichWithSmart because the latter may
	// fork smartctl (1-2s per disk); running the diskstats snapshot
	// *during* that wait would contaminate the time delta with SSH latency.
	enrichWithRW(cli, disks, cache)
	enrichWithSmart(cli, disks)
	enrichWithSmart(cli, cache)

	c.JSON(http.StatusOK, arrayStatus{
		State:      arrayState,
		Disks:      disks,
		CacheDisks: cache,
	})
}

// enrichWithRW populates per-disk read/write byte rates from /proc/diskstats
// deltas. Runs two snapshots ~900ms apart and reuses parseDiskstats (shared
// with the dashboard handler). Uses diskStatsKey to map each disk's device
// path to its whole-disk entry in diskstats (e.g. /dev/sda3 -> sda,
// /dev/md1 -> md1, /dev/nvme0n1p2 -> nvme0n1).
//
// One whole-disk rate is shared by all partitions on that disk — the UI
// shows it per-mount-point, so on a multi-partition disk each entry shows
// the same figure. Acceptable at v0.x because Unraid mounts whole disks
// single-partition in the common case.
//
// Before v0.4 disk.ReadBytesPerSec/WriteBytesPerSec were always 0 because
// nothing populated them — the frontend already rendered the "读 X · 写 Y"
// row, just with zeros.
func enrichWithRW(cli *ssh.Client, diskSlices ...[]disk) {
	s1, _ := cli.Run("cat /proc/diskstats")
	a := parseDiskstats(s1)

	time.Sleep(900 * time.Millisecond)

	s2, _ := cli.Run("cat /proc/diskstats")
	b := parseDiskstats(s2)

	const dt = 0.9
	for _, ds := range diskSlices {
		for i := range ds {
			key := diskStatsKey(ds[i].Device)
			if key == "" {
				continue
			}
			v1, ok1 := a[key]
			v2, ok2 := b[key]
			if !ok1 || !ok2 {
				continue
			}
			rd := int64(float64(v2.sectorsRead-v1.sectorsRead) * 512 / dt)
			wr := int64(float64(v2.sectorsWritten-v1.sectorsWritten) * 512 / dt)
			if rd < 0 {
				rd = 0
			}
			if wr < 0 {
				wr = 0
			}
			ds[i].ReadBytesPerSec = rd
			ds[i].WriteBytesPerSec = wr
		}
	}
}

// diskStatsKey maps a device path to its whole-disk entry name in
// /proc/diskstats. Differs from baseDevName in that md* software-raid
// arrays DO have diskstats rows (and we want their RW rates), even though
// they don't support SMART. loop*/zvol* return "" (loop never appears in
// the Storage UI because parseDf filters to /mnt/{disk,cache}*; zvol has
// no /dev/ prefix to strip so it falls through to "").
func diskStatsKey(devPath string) string {
	d := strings.TrimPrefix(devPath, "/dev/")
	if d == devPath {
		return "" // no /dev/ prefix (e.g. zvol pool/ds) — not a diskstats row
	}
	// md* whole-disk: /dev/md126 -> "md126". Partitions like /dev/md0p1
	// are vanishingly rare in practice; treat the whole name as the key.
	if strings.HasPrefix(d, "md") {
		return d
	}
	return baseDevName(devPath)
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

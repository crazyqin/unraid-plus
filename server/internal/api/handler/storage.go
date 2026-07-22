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
	// Unraid-specific fields from state files (v0.7+). Frontend can use
	// these for richer display (disk slot name, Unraid color indicator,
	// rotational flag, transport type) even when smartctl is absent.
	DiskName   string `json:"diskName,omitempty"`   // e.g. "disk1", "parity", "cache1"
	Color      string `json:"color,omitempty"`      // Unraid LED: "green-on", "yellow-on", etc.
	Rotational string `json:"rotational,omitempty"` // "0" = SSD, "1" = HDD
	Transport  string `json:"transport,omitempty"`  // "ata", "nvme", "usb"
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

// Storage returns the array + cache disk view. v0.7+ reads Unraid's own
// structured state files (/usr/local/emhttp/state/disks.ini, var.ini) via
// SSH — the same data source the Unraid WebUI uses. This replaces the old
// fragile approach of parsing `df` output and `mdcmd status` format.
//
// SMART data still comes from smartctl (cached 30s via smartCache), but the
// md→physical device mapping now uses the diskName/rdevName pairs from
// disks.ini (via state files) instead of parsing mdcmd status separately.
//
// Per-disk read/write rates still use /proc/diskstats deltas (enrichWithRW).
func (h *Handler) Storage(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}

	// Read all state files in one SSH batch (3 files, 1 SSH call).
	state, err := readStateFiles(cli)
	if err != nil || state == nil || len(state.Disks) == 0 {
		// Fallback to old df-based approach if state files unavailable
		// (e.g. very old Unraid versions or non-Unraid servers).
		h.storageFallback(c, cli)
		return
	}

	// Array state from var.ini mdState field (e.g. "STARTED" -> "started")
	arrayState := strings.ToLower(state.Var.MdState)
	if arrayState == "" {
		arrayState = "unknown"
	}

	// Build disk lists from state file data
	arrayDisks := stateArrayDisks(state.Disks)
	cacheDisks := stateCacheDisks(state.Disks)

	disks := make([]disk, 0, len(arrayDisks))
	for _, ds := range arrayDisks {
		disks = append(disks, stateToDisk(ds))
	}
	cache := make([]disk, 0, len(cacheDisks))
	for _, ds := range cacheDisks {
		cache = append(cache, stateToDisk(ds))
	}

	// Pre-populate md→physical mapping from state files so SMART probing
	// doesn't need a separate mdcmd status call.
	for _, ds := range state.Disks {
		if ds.DeviceSb != "" && ds.Device != "" {
			mdPhysicalCache.Store(ds.DeviceSb, ds.Device)
		}
	}

	// Enrich with RW rates and SMART data
	enrichWithRW(cli, disks, cache)
	enrichWithSmart(cli, disks)
	enrichWithSmart(cli, cache)

	c.JSON(http.StatusOK, arrayStatus{
		State:      arrayState,
		Disks:      disks,
		CacheDisks: cache,
	})
}

// storageFallback is the old df+mdcmd approach, used when state files are
// unavailable (non-Unraid servers or very old versions).
func (h *Handler) storageFallback(c *gin.Context, cli *ssh.Client) {
	dfOut, _ := cli.Run(`df -PT 2>/dev/null | awk 'NR>1{print $1"|"$2"|"$4"|"$5"|"$7}'`)
	arrayState := "unknown"
	if out, _ := cli.Run("mdcmd status 2>/dev/null"); strings.TrimSpace(out) != "" {
		arrayState = parseMdState(out)
	}

	disks, cache := parseDf(dfOut)
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
// after the first hit. For md* devices (Unraid array disks), we resolve the
// underlying physical device via sysfs and probe SMART on that.
//
// When smartctl is unavailable (not installed, device unsupported), we fall
// back to state file data (disks.ini color/status for health indicator,
// temp for temperature). This gives the frontend something useful to show
// even without smartctl.
func enrichWithSmart(cli *ssh.Client, disks []disk) {
	for i := range disks {
		base := baseDevName(disks[i].Device)
		if base == "" {
			// For md devices, try to find the underlying physical disk
			base = resolveMdBase(cli, disks[i].Device)
			if base == "" {
				continue
			}
		}
		info := fetchSmart(cli, base)
		if info.Available {
			disks[i].Smart = &info
			if info.Temperature != nil {
				disks[i].TempC = info.Temperature
			}
			disks[i].Errors = info.Reallocated + info.Pending +
				info.Uncorrectable + info.MediaErrors
		}
		// When smartctl unavailable: leave state-file temp in TempC (already
		// set by stateToDisk). The disk's Color/Rotational/Transport fields
		// from disks.ini let the frontend show health even without SMART.
	}
}

// parseDf turns the pipe-separated df output into disk entries.
// Mounts under /mnt/disk* are array disks; under /mnt/cache* are cache.
// Only real block devices (dev starts with /dev/) are included — this
// filters out tmpfs, fuse (CloudFS, shfs), overlay, etc. that may be
// mounted under /mnt/disk* or /mnt/cache* on Unraid.
func parseDf(s string) (array []disk, cache []disk) {
	for _, line := range strings.Split(s, "\n") {
		f := strings.Split(line, "|")
		if len(f) < 5 {
			continue
		}
		dev, fsType, used, avail, mount := f[0], f[1], atoi64Safe(f[2])*1024, atoi64Safe(f[3])*1024, f[4]
		// Skip pseudo filesystems — only include real block devices.
		if !strings.HasPrefix(dev, "/dev/") {
			continue
		}
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

// parseMdState extracts the array state from `mdcmd status` output.
// Looks for a "mdState=" line (e.g. "mdState=Started" → "started").
// Returns "unknown" if the line is not found.
func parseMdState(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "mdState=") {
			return strings.ToLower(strings.TrimPrefix(line, "mdState="))
		}
	}
	return "unknown"
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

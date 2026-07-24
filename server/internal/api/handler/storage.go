package handler

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/crazyqin/unraid-plus/server/internal/ssh"
	"github.com/crazyqin/unraid-plus/server/internal/unraid"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
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
	// physicalDev is the physical block device name (e.g. "sdb", "nvme0n1")
	// from disks.ini — used by enrichWithRW to look up /proc/diskstats.
	// Array disks are mounted as /dev/mdNp1, but Unraid's custom md driver
	// may not register in diskstats; their physical device always does.
	physicalDev string
}

type arrayStatus struct {
	State      string `json:"state"`
	Disks      []disk `json:"disks"`
	CacheDisks []disk `json:"cacheDisks"`
	DeviceCount string `json:"deviceCount,omitempty"` // number of devices from server metadata
}

// Storage returns the array + cache disk view.
// v0.9+: Uses the official Unraid GraphQL API (GetArrayStatus) when available.
// Falls back to SSH (state files + df) when GraphQL is not available.
// HTML scraping code has been removed.
func (h *Handler) Storage(c *gin.Context) {
	cli, sid, hasSSH, hasAPI := h.resolveServer(c)
	if sid == "" {
		return
	}

	// GraphQL-first
	if hasAPI && h.ur.HasGraphQL(sid) {
		h.storageGraphQL(c, sid, cli, hasSSH)
		return
	}

	if hasSSH {
		h.storageSSH(c, cli, sid)
		return
	}

	errOut(c, http.StatusServiceUnavailable, "Storage unavailable: GraphQL API not available and SSH not connected")
}

// storageSSH is the full SSH-based storage handler.
func (h *Handler) storageSSH(c *gin.Context, cli *ssh.Client, sid string) {

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

	// Enrich with RW rates and SMART data (use sid from function parameter)
	enrichWithRW(cli, disks, cache)
	enrichWithSmart(cli, disks, sid)
	enrichWithSmart(cli, cache, sid)

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
	sid := c.Query("serverId")
	if sid == "" {
		sid = "_default"
	}
	enrichWithRW(cli, disks, cache)
	enrichWithSmart(cli, disks, sid)
	enrichWithSmart(cli, cache, sid)

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
			// Resolve the diskstats lookup key:
			// 1. Prefer physicalDev (from disks.ini "device" field) — always
			//    a real block device that appears in diskstats.
			// 2. Fall back to diskStatsKey(Device path) for disks without
			//    a physical device mapping (e.g. non-Unraid fallback path).
			var key string
			if ds[i].physicalDev != "" {
				key = baseDevName("/dev/" + ds[i].physicalDev)
			}
			if key == "" {
				key = diskStatsKey(ds[i].Device)
			}
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
	// md* devices: /dev/md1p1 -> "md1", /dev/md126p2 -> "md126"
	// Unraid mounts array disks as /dev/mdNp1 — the partition suffix
	// (p1, p2…) has no diskstats entry; we need the whole-disk mdN key.
	if strings.HasPrefix(d, "md") {
		// Strip partition suffix: "md1p1" -> "md1", "md126" -> "md126"
		if idx := strings.IndexByte(d, 'p'); idx > 2 {
			// Only strip if the part after 'p' looks like a number
			// (avoid stripping "p" from a device literally named "mdp...")
			rest := d[idx+1:]
			if rest != "" && rest[0] >= '0' && rest[0] <= '9' {
				return d[:idx]
			}
		}
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
func enrichWithSmart(cli *ssh.Client, disks []disk, serverID string) {
	for i := range disks {
		base := baseDevName(disks[i].Device)
		if base == "" {
			// For md devices, try to find the underlying physical disk
			base = resolveMdBase(cli, disks[i].Device)
			if base == "" {
				continue
			}
		}
		info := fetchSmart(cli, base, serverID)
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

// ---------------------------------------------------------------------------
// Storage via official Unraid GraphQL API
// ---------------------------------------------------------------------------

// storageGraphQL uses the Unraid GraphQL API (GetArrayStatus) for disk data.
// When SSH is also available, it enriches with RW rates and SMART data.
func (h *Handler) storageGraphQL(c *gin.Context, sid string, cli *ssh.Client, hasSSH bool) {
	data, err := h.ur.GraphQLQuery(sid, unraid.QueryGetArrayStatus, nil)
	if err != nil {
		logger.Warnf("storage graphql query failed for %s: %v", sid, err)
		// Fall back to SSH if available
		if hasSSH && cli != nil {
			h.storageSSH(c, cli, sid)
			return
		}
		errOut(c, http.StatusServiceUnavailable, "存储信息获取失败: "+err.Error())
		return
	}

	arr, err := unraid.ParseArrayQuery(data)
	if err != nil {
		errOut(c, http.StatusInternalServerError, "解析存储数据失败: "+err.Error())
		return
	}

	// Convert GraphQL disk data to our disk struct
	var arrayDisks, cacheDisks []disk
	if arr.Boot != nil {
		// Boot device (USB flash)
	}
	for _, d := range arr.Parities {
		arrayDisks = append(arrayDisks, gqlDiskToDisk(d))
	}
	for _, d := range arr.Disks {
		arrayDisks = append(arrayDisks, gqlDiskToDisk(d))
	}
	for _, d := range arr.Caches {
		cacheDisks = append(cacheDisks, gqlDiskToDisk(d))
	}

	// Enrich with RW rates and SMART if SSH is available
	if hasSSH && cli != nil {
		enrichWithRW(cli, arrayDisks, cacheDisks)
		enrichWithSmart(cli, arrayDisks, sid)
		enrichWithSmart(cli, cacheDisks, sid)
	}

	c.JSON(http.StatusOK, arrayStatus{
		State:      strings.ToLower(arr.State),
		Disks:      arrayDisks,
		CacheDisks: cacheDisks,
	})
}

// gqlDiskToDisk converts a GraphQL disk response to our disk struct.
func gqlDiskToDisk(d unraid.GQLDisk) disk {
	dk := disk{
		Name:       d.Name,
		Device:     d.Device,
		Status:     strings.ToLower(d.Status),
		Color:      d.Color,
		Rotational: d.Rotational,
		Transport:  d.Transport,
	}
	if d.Size != "" {
		dk.SizeBytes = parseGQLDiskSize(d.Size)
	}
	if d.FsSize != "" {
		dk.SizeBytes = parseGQLDiskSize(d.FsSize)
	}
	if d.FsUsed != "" {
		dk.UsedBytes = parseGQLDiskSize(d.FsUsed)
	}
	if d.Temp != "" {
		tempC := atoiSafe(d.Temp, 0)
		if tempC > 0 {
			dk.TempC = &tempC
		}
	}
	if d.NumErrors != "" {
		dk.Errors = atoiSafe(d.NumErrors, 0)
	}
	if d.FsType != "" {
		dk.FsType = d.FsType
	}
	if dk.Status == "" && d.Color != "" {
		// Derive status from Unraid color indicator
		switch {
		case strings.Contains(d.Color, "green"):
			dk.Status = "ok"
		case strings.Contains(d.Color, "yellow"):
			dk.Status = "warning"
		case strings.Contains(d.Color, "red"):
			dk.Status = "critical"
		default:
			dk.Status = "unknown"
		}
	}
	return dk
}

// parseGQLDiskSize parses a disk size string from the GraphQL API.
// The API may return sizes like "3.62 TB" or raw numbers.
func parseGQLDiskSize(s string) int64 {
	if s == "" {
		return 0
	}
	// Try as a plain number (bytes)
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}
	// Try as a human-readable size
	s = strings.TrimSpace(s)
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return 0
	}
	numStr := parts[0]
	var unit string
	if len(parts) > 1 {
		unit = parts[1]
	} else {
		// Extract unit from the string (e.g. "3.62TB")
		re := regexp.MustCompile(`^([0-9.]+)\s*(TB|GB|MB|KB|TiB|GiB|MiB|KiB|T|G|M|K)`)
		if m := re.FindStringSubmatch(s); len(m) > 2 {
			numStr = m[1]
			unit = m[2]
		}
	}
	v := atofSafe(numStr)
	switch {
	case strings.HasPrefix(unit, "Ti"), strings.HasPrefix(unit, "T"):
		return int64(v * float64(1<<40))
	case strings.HasPrefix(unit, "Gi"), strings.HasPrefix(unit, "G"):
		return int64(v * float64(1<<30))
	case strings.HasPrefix(unit, "Mi"), strings.HasPrefix(unit, "M"):
		return int64(v * float64(1<<20))
	case strings.HasPrefix(unit, "Ki"), strings.HasPrefix(unit, "K"):
		return int64(v * float64(1<<10))
	default:
		return int64(v)
	}
}



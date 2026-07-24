package handler

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/crazyqin/unraid-plus/server/internal/ssh"
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
	Degraded   bool   `json:"degraded,omitempty"`         // true when data from HTML scraping
	DegradedReason string `json:"degradedReason,omitempty"` // "ssh_unavailable" etc.
}

// Storage returns the array + cache disk view. v0.7+ reads Unraid's own
// structured state files (/usr/local/emhttp/state/disks.ini, var.ini) via
// SSH — the same data source the Unraid WebUI uses. This replaces the old
// fragile approach of parsing `df` output and `mdcmd status` format.
//
// v0.7+: When SSH is unavailable but WebGUI API is available, falls back
// to scraping the Unraid Main page HTML for basic disk info.
func (h *Handler) Storage(c *gin.Context) {
	cli, sid, hasSSH, hasAPI := h.resolveServer(c)
	if sid == "" {
		return
	}

	if hasSSH {
		h.storageSSH(c, cli, sid)
		return
	}

	// API-only fallback: scrape the Unraid Main page HTML
	if hasAPI {
		h.storageAPI(c, sid)
		return
	}

	errOut(c, http.StatusServiceUnavailable, "存储信息不可用")
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
// Storage API fallback (HTML scraping from Unraid WebGUI Main page)
// ---------------------------------------------------------------------------

// storageAPI scrapes the Unraid Main page for basic disk info when SSH
// is unavailable. This is a degraded view — no per-disk RW rates, no SMART
// data, and disk info depends on the HTML structure of the Unraid version.
func (h *Handler) storageAPI(c *gin.Context, sid string) {
	body, status, err := h.ur.FetchPage(sid, "/Main")
	if err != nil || status != 200 {
		logger.Debugf("storage API fallback: failed to fetch /Main: %v (status=%d)", err, status)
		c.JSON(http.StatusOK, arrayStatus{
			State:          "unknown",
			Degraded:       true,
			DegradedReason: "ssh_unavailable",
		})
		return
	}

	html := string(body)
	state, disks, cache := parseStorageHTML(html)

	c.JSON(http.StatusOK, arrayStatus{
		State:           state,
		Disks:           disks,
		CacheDisks:      cache,
		Degraded:        true,
		DegradedReason:  "ssh_unavailable",
	})
}

// Regex patterns for scraping Unraid Main page disk table.
var (
	// Disk table rows — Unraid renders each disk as a <tr> with specific classes
	reDiskRow = regexp.MustCompile(`(?s)<tr[^>]*class=['"][^'"]*dash-disk[^'"]*['"][^>]*>(.*?)</tr>`)
	// Disk name (e.g. "disk1", "parity", "cache")
	reDiskSlotName = regexp.MustCompile(`<a[^>]*class=['"][^'"]*info-popup[^'"]*['"][^>]*>([^<]+)</a>`)
	// Device path (e.g. "sdb", "sdc")
	reDiskDevice = regexp.MustCompile(`<td[^>]*class=['"][^'"]*dev[^'"]*['"][^>]*>([^<]+)</td>`)
	// Temperature (e.g. "33°C" or "33°" or just "33")
	reDiskTemp = regexp.MustCompile(`(\d+)\s*°`)
	// Size values in table cells (e.g. "3.62 TB", "500 GB")
	reSizeCell = regexp.MustCompile(`(\d+\.?\d*)\s*(TB|GB|MB|KB|TiB|GiB|MiB|KiB)`)
	// Color/status indicator (Unraid LED class like "green-on", "yellow-on", "red-blink")
	reColorIndicator = regexp.MustCompile(`class=['"][^'"]*(green-on|yellow-on|red-blink|red-on|grey-off|blue-on)[^'"]*['"]`)
	// Array state from the status header
	reArrayStateLabel = regexp.MustCompile(`(?i)class=['"][^'"]*status[^'"]*['"][^>]*>([^<]+)<`)
)

// parseStorageHTML extracts disk information from the Unraid Main page HTML.
// Returns (arrayState, arrayDisks, cacheDisks).
func parseStorageHTML(html string) (string, []disk, []disk) {
	// Try to extract array state
	arrayState := "unknown"
	if m := reArrayStateLabel.FindStringSubmatch(html); len(m) > 1 {
		stateStr := strings.TrimSpace(strings.ToLower(m[1]))
		switch {
		case strings.Contains(stateStr, "started"):
			arrayState = "started"
		case strings.Contains(stateStr, "stopped"):
			arrayState = "stopped"
		}
	}

	var arrayDisks, cacheDisks []disk

	// Find disk table rows
	rows := reDiskRow.FindAllStringSubmatch(html, -1)
	for _, rowMatch := range rows {
		rowHTML := rowMatch[1]
		name := ""
		device := ""
		var sizeBytes, usedBytes int64
		tempC := 0
		color := ""

		// Extract disk name
		if m := reDiskSlotName.FindStringSubmatch(rowHTML); len(m) > 1 {
			name = strings.TrimSpace(m[1])
		}

		// Extract device
		if m := reDiskDevice.FindStringSubmatch(rowHTML); len(m) > 1 {
			device = strings.TrimSpace(m[1])
		}

		// Extract temperature
		if m := reDiskTemp.FindStringSubmatch(rowHTML); len(m) > 1 {
			tempC = atoiSafe(m[1], 0)
		}

		// Extract color indicator
		if m := reColorIndicator.FindStringSubmatch(rowHTML); len(m) > 1 {
			color = m[1]
		}

		// Extract size values (first = total, try to find used/free from context)
		sizeMatches := reSizeCell.FindAllStringSubmatch(rowHTML, -1)
		if len(sizeMatches) > 0 {
			// First size is typically total
			sizeBytes = parseSizeStr(sizeMatches[0][0])
		}
		if len(sizeMatches) > 1 {
			// Second size might be used or free depending on Unraid version
			secondVal := parseSizeStr(sizeMatches[1][0])
			if secondVal > 0 && secondVal < sizeBytes {
				usedBytes = sizeBytes - secondVal // likely free
			}
		}

		// Determine if this is a cache or array disk
		d := disk{
			Name:      name,
			Device:    device,
			SizeBytes: sizeBytes,
			UsedBytes: usedBytes,
			Color:     color,
			Status:    diskStatus(usedBytes, sizeBytes),
		}
		if tempC > 0 {
			d.TempC = &tempC
		}

		nameLower := strings.ToLower(name)
		switch {
		case strings.HasPrefix(nameLower, "cache"):
			cacheDisks = append(cacheDisks, d)
		case nameLower != "" && nameLower != "parity":
			arrayDisks = append(arrayDisks, d)
		case nameLower == "parity":
			// Parity disk goes into array list with special status
			d.Status = "ok"
			arrayDisks = append(arrayDisks, d)
		}
	}

	return arrayState, arrayDisks, cacheDisks
}

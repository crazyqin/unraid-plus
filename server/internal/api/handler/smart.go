package handler

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/crazyqin/unraid-plus/server/internal/ssh"
)

// smartInfo holds the subset of SMART attributes we surface to the UI.
// We never omit zero-value fields once Available=true, so the frontend can
// distinguish "we ran SMART and got 0" from "we didn't run SMART".
type smartInfo struct {
	// Available is false when smartctl is not installed, the device does
	// not support SMART (md software raid, USB bridges without SAT, loop,
	// zfs vdevs, etc.), or the JSON output failed to parse. In all those
	// cases Status is "unknown" and the counters are zero.
	Available bool `json:"available"`

	// Passed mirrors smartctl's smart_status.passed bit (SATA self-test
	// result / NVMe critical warning register). false = drive thinks it
	// is already failing.
	Passed bool `json:"passed"`

	// Status is the curated summary consumed by badges:
	//   "ok"      — self-test passes and no reliability counter is non-zero
	//   "warning" — reallocated / pending / offline-uncorrectable / NVMe
	//                media_errors non-zero but self-test still passes
	//   "failing" — smart_status.passed == false
	//   "unknown" — smartctl missing / device unsupported / JSON parse error
	Status string `json:"status"`

	// Temperature (SATA attr 194 / NVMe composite temperature). nil when
	// not reported. More reliable than /sys/class/thermal for individual
	// disks because it is the on-disk sensor, not the CPU-zone sensor.
	Temperature *int `json:"temperature,omitempty"`

	// SATA reliability counters (raw values).
	Reallocated   int `json:"reallocated"`   // attr 5
	Pending       int `json:"pending"`       // attr 197
	Uncorrectable int `json:"uncorrectable"` // attr 198

	// NVMe cumulative media & data integrity errors. Surfaced separately
	// because semantics differ from SATA reallocated sectors.
	MediaErrors int `json:"mediaErrors"`

	// Model / serial for display + identification.
	ModelName    string `json:"modelName,omitempty"`
	SerialNumber string `json:"serialNumber,omitempty"`

	// FetchedAt is the unix timestamp of the cache entry. The frontend
	// can use it to show "刷新于 N 秒前" alongside the data.
	FetchedAt int64 `json:"fetchedAt"`
}

// smartCache is a process-wide TTL cache so we don't fork smartctl on every
// 5s storage-page poll — that would drown the SSH connection on multi-disk
// arrays (smartctl is ~1-2s per disk under load). The cache is keyed by
// base device name (e.g. "sda", "nvme0n1").
//
// Stale-on-write trade-off: a probe failure is also cached for the TTL,
// otherwise the frontend would re-trigger an expensive failing probe on
// every poll. The TTL is short enough (30s) that a replaced drive / newly
// installed smartctl shows up within a minute.
var smartCache = struct {
	sync.RWMutex
	m map[string]smartInfo
}{m: map[string]smartInfo{}}

const smartCacheTTL = 30 * time.Second

// fetchSmart returns the cached entry if fresh, otherwise invokes smartctl
// on the given base device name (e.g. "sda", "nvme0n1") and parses the JSON.
func fetchSmart(cli *ssh.Client, base string) smartInfo {
	smartCache.RLock()
	if e, ok := smartCache.m[base]; ok && time.Since(time.Unix(e.FetchedAt, 0)) < smartCacheTTL {
		smartCache.RUnlock()
		return e
	}
	smartCache.RUnlock()

	info := probeSmart(cli, base)
	info.FetchedAt = time.Now().Unix()

	smartCache.Lock()
	smartCache.m[base] = info
	smartCache.Unlock()
	return info
}

// probeSmart runs `smartctl -H -A -j /dev/<base>` and parses the JSON.
// -H  health (populates smart_status.passed)
// -A  attributes (SATA attribute table / NVMe health log)
// -j  JSON output (smartctl 7+)
//
// smartctl exit code is a bitmask: bit 0 = cmdline error, bit 3 = "pre-failure
// attribute trip", etc. So non-zero exit doesn't mean the command failed —
// we treat whatever stdout we got as parseable JSON. Empty stdout (smartctl
// missing or device absent) falls back to "unknown".
//
// All output parsing happens in parseSmartJSON, which handles SATA vs NVMe
// shapes. smartctl not installed → empty output → status "unknown".
func probeSmart(cli *ssh.Client, base string) smartInfo {
	if base == "" {
		return smartInfo{Status: "unknown"}
	}
	out, _ := cli.Run("smartctl -H -A -j " + shellQuote("/dev/"+base) + " 2>/dev/null || true")
	if strings.TrimSpace(out) == "" {
		return smartInfo{Status: "unknown"}
	}
	return parseSmartJSON([]byte(out))
}

// parseSmartJSON extracts the bits we care about from smartctl -j output.
// Handles both SATA (ata_smart_attributes.table[]) and NVMe
// (nvme_smart_health_information_log.*) formats. We use a generic
// map[string]any first because smartctl's JSON tree shape varies a lot by
// drive type and version; a fixed struct tree ends up either losing data
// or sprawling.
func parseSmartJSON(buf []byte) smartInfo {
	var top map[string]any
	if err := json.Unmarshal(buf, &top); err != nil {
		return smartInfo{Status: "unknown"}
	}

	info := smartInfo{
		Available: true,
		Status:    "ok",
	}

	if v, ok := top["model_name"].(string); ok {
		info.ModelName = v
	}
	if v, ok := top["serial_number"].(string); ok {
		info.SerialNumber = v
	}

	// smart_status.passed is present in smartctl 7+ on both SATA and NVMe.
	if ss, ok := top["smart_status"].(map[string]any); ok {
		if p, ok := ss["passed"].(bool); ok {
			info.Passed = p
			if !p {
				info.Status = "failing"
			}
		}
	}

	// Temperature: prefer the top-level temperature.current field
	// (smartctl 7+) which works for both SATA and NVMe. The SATA raw
	// value for attr 194 is a multi-byte encoded integer (not the
	// actual temperature), so we only fall back to it if the top-level
	// field is missing.
	if temp, ok := top["temperature"].(map[string]any); ok {
		if t, ok := temp["current"].(float64); ok {
			ti := int(t)
			info.Temperature = &ti
		}
	}

	// SATA path.
	if attrs, ok := top["ata_smart_attributes"].(map[string]any); ok {
		if tbl, ok := attrs["table"].([]any); ok {
			for _, row := range tbl {
				m, ok := row.(map[string]any)
				if !ok {
					continue
				}
				id, _ := m["id"].(float64)
				raw, _ := m["raw"].(map[string]any)
				if raw == nil {
					continue
				}
				val, _ := raw["value"].(float64)
				switch int(id) {
				case 5:
					info.Reallocated = int(val)
				case 197:
					info.Pending = int(val)
				case 198:
					info.Uncorrectable = int(val)
				case 194: // Temperature_Celsius — fallback only
					if info.Temperature == nil {
						// raw.value for temp is multi-byte encoded;
						// try raw.string first (e.g. "45 (Min/Max 18/80)")
						if s, ok := raw["string"].(string); ok {
							t := parseTempFromString(s)
							if t > 0 {
								info.Temperature = &t
							}
						}
					}
				}
			}
		}
	}

	// NVMe path — only if top-level temperature didn't provide it.
	if info.Temperature == nil {
		if nl, ok := top["nvme_smart_health_information_log"].(map[string]any); ok {
			if t, ok := nl["temperature"].(float64); ok {
				ti := int(t)
				info.Temperature = &ti
			}
			if me, ok := nl["media_errors"].(float64); ok {
				info.MediaErrors = int(me)
			}
		}
	} else {
		// Temperature already set, but still grab media_errors
		if nl, ok := top["nvme_smart_health_information_log"].(map[string]any); ok {
			if me, ok := nl["media_errors"].(float64); ok {
				info.MediaErrors = int(me)
			}
		}
	}

	// Promote status to "warning" if any counter is non-zero (unless the
	// self-test already flipped us to "failing").
	if info.Status == "ok" && (info.Reallocated > 0 ||
		info.Pending > 0 ||
		info.Uncorrectable > 0 ||
		info.MediaErrors > 0) {
		info.Status = "warning"
	}

	return info
}

// baseDevName extracts the block-device base name from a partition path:
//   /dev/sda3    -> sda
//   /dev/sda     -> sda
//   /dev/nvme0n1p2 -> nvme0n1
//   /dev/nvme0n1   -> nvme0n1
//   /dev/mmcblk0p1 -> mmcblk0
//   /dev/md126     -> ""   (software raid, smartctl won't help)
//   /dev/loop0     -> ""
//   /dev/zvol/...  -> ""
//
// Callers should skip SMART probing when "" is returned.
var (
	nvmeWhole = regexp.MustCompile(`^nvme\d+n\d+$`)
	mmcWhole  = regexp.MustCompile(`^mmcblk\d+$`)
	sdWhole   = regexp.MustCompile(`^(sd[a-z]+|vd[a-z])$`)
	nvmePart  = regexp.MustCompile(`^(nvme\d+n\d+)p\d+$`)
	mmcPart   = regexp.MustCompile(`^(mmcblk\d+)p\d+$`)
	sdPart    = regexp.MustCompile(`^(sd[a-z]+|vd[a-z])\d+$`)
)

func baseDevName(devPath string) string {
	d := strings.TrimPrefix(devPath, "/dev/")
	if d == devPath {
		return "" // no /dev/ prefix at all
	}
	if nvmeWhole.MatchString(d) || mmcWhole.MatchString(d) || sdWhole.MatchString(d) {
		return d
	}
	if m := nvmePart.FindStringSubmatch(d); m != nil {
		return m[1]
	}
	if m := mmcPart.FindStringSubmatch(d); m != nil {
		return m[1]
	}
	if m := sdPart.FindStringSubmatch(d); m != nil {
		return m[1]
	}
	return ""
}

// mdPhysicalCache caches md→physical device mappings (e.g. "md1p1" -> "sdb").
// The mapping is stable for the process lifetime — it only changes if the
// array is reconfigured (disk replaced in same slot keeps same mapping).
var mdPhysicalCache sync.Map // map[string]string

// resolveMdBase finds the underlying physical device for an md device path
// (e.g. /dev/md1p1 -> "sdb"). This is needed because smartctl can't probe
// md devices directly — we need to probe the physical disk underneath.
//
// On Unraid, each array slot (md1, md2, ...) is backed by exactly one
// physical disk. The mapping is discovered by parsing `mdcmd status` output,
// which contains paired lines like:
//   diskName.1=md1p1
//   rdevName.1=sdb
//
// Returns "" if the path is not an md device or no backing device is found.
func resolveMdBase(cli *ssh.Client, devPath string) string {
	d := strings.TrimPrefix(devPath, "/dev/")
	if d == devPath || !strings.HasPrefix(d, "md") {
		return ""
	}

	// Check cache first
	if v, ok := mdPhysicalCache.Load(d); ok {
		return v.(string)
	}

	// Parse mdcmd status to build the full md→physical mapping.
	// This populates the cache for all md devices in one SSH call.
	out, _ := cli.Run("mdcmd status 2>/dev/null")
	mapping := parseMdcmdPhysicalMap(out)
	for mdName, physName := range mapping {
		mdPhysicalCache.Store(mdName, physName)
	}

	if v, ok := mdPhysicalCache.Load(d); ok {
		return v.(string)
	}

	// Cache negative result to avoid re-querying
	mdPhysicalCache.Store(d, "")
	return ""
}

// parseMdcmdPhysicalMap parses `mdcmd status` output and returns a map
// from md device name (e.g. "md1p1") to physical device base name (e.g. "sdb").
// Only entries with non-empty diskName and rdevName are included.
func parseMdcmdPhysicalMap(out string) map[string]string {
	diskNames := map[string]string{} // slot number -> md device name
	rdevNames := map[string]string{} // slot number -> physical device name

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "diskName.") {
			kv := strings.SplitN(line, "=", 2)
			if len(kv) != 2 {
				continue
			}
			num := strings.TrimPrefix(kv[0], "diskName.")
			diskNames[num] = kv[1]
		} else if strings.HasPrefix(line, "rdevName.") {
			kv := strings.SplitN(line, "=", 2)
			if len(kv) != 2 {
				continue
			}
			num := strings.TrimPrefix(kv[0], "rdevName.")
			rdevNames[num] = kv[1]
		}
	}

	mapping := map[string]string{}
	for num, mdName := range diskNames {
		if mdName == "" {
			continue
		}
		physName, ok := rdevNames[num]
		if !ok || physName == "" {
			continue
		}
		// Verify it's a real disk device (sd*, nvme*, vd*, mmcblk*)
		if baseDevName("/dev/" + physName) != "" {
			mapping[mdName] = physName
		}
	}
	return mapping
}

// invalidateSmartCache drops cache entries, returning the list of device keys
// that were actually removed. If `devices` is empty the whole cache is
// cleared. Keys that aren't present are silently skipped (not erroring on
// "refresh sda when sda was never probed" keeps the UI button idempotent).
//
// This is the backing store for POST /api/smart/refresh (see smart_refresh.go).
// It only invalidates; the next GET /api/storage call will trigger fresh
// smartctl probes via fetchSmart's cache-miss path. We don't proactively
// re-probe here because that would require walking the disk list + an active
// SSH connection from inside this call, and the frontend already follows up
// the refresh with a re-fetch of /api/storage anyway.
func invalidateSmartCache(devices []string) []string {
	smartCache.Lock()
	defer smartCache.Unlock()

	// Empty filter → drop everything.
	if len(devices) == 0 {
		cleared := make([]string, 0, len(smartCache.m))
		for k := range smartCache.m {
			cleared = append(cleared, k)
		}
		smartCache.m = map[string]smartInfo{}
		return cleared
	}

	cleared := make([]string, 0, len(devices))
	for _, dev := range devices {
		if _, ok := smartCache.m[dev]; ok {
			delete(smartCache.m, dev)
			cleared = append(cleared, dev)
		}
	}
	return cleared
}

// parseTempFromString extracts the first integer from a smartctl raw.string
// value like "45 (Min/Max 18/80)" → 45. Returns 0 if no number is found.
// Used as a fallback when the top-level temperature.current field is missing.
func parseTempFromString(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// Extract the first sequence of digits
	start := -1
	for i, c := range s {
		if c >= '0' && c <= '9' {
			if start == -1 {
				start = i
			}
		} else if start != -1 {
			n, err := strconv.Atoi(s[start:i])
			if err == nil {
				return n
			}
			start = -1
		}
	}
	if start != -1 {
		n, err := strconv.Atoi(s[start:])
		if err == nil {
			return n
		}
	}
	return 0
}

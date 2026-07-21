package handler

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/your-org/unraidpp/server/internal/ssh"
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
				case 194: // Temperature_Celsius / Temperature_Internal
					t := int(val)
					info.Temperature = &t
				}
			}
		}
	}

	// NVMe path.
	if nl, ok := top["nvme_smart_health_information_log"].(map[string]any); ok {
		if t, ok := nl["temperature"].(float64); ok {
			ti := int(t)
			info.Temperature = &ti
		}
		if me, ok := nl["media_errors"].(float64); ok {
			info.MediaErrors = int(me)
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

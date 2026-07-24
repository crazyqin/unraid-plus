package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/crazyqin/unraid-plus/server/internal/ssh"
	"github.com/crazyqin/unraid-plus/server/internal/unraid"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

type dashboardResp struct {
	CPU       cpuInfo     `json:"cpu"`
	Memory    memInfo     `json:"memory"`
	Network   []netInfo   `json:"network"`
	ArrayRw   rwRate      `json:"arrayRwBytesPerSec"`
	Uptime    int64       `json:"uptime"`
	LoadAvg   [3]float64  `json:"loadAvg"`
	ServerMeta *serverMeta `json:"serverMeta,omitempty"` // Unraid server metadata
}

// serverMeta holds metadata extracted from Unraid's <unraid-user-profile server="..."> JSON.
// This is the richest structured data available in API-only mode.
type serverMeta struct {
	Name        string `json:"name"`
	OSVersion   string `json:"osVersion"`
	Description string `json:"description"`
	Model       string `json:"model"`
	RegType     string `json:"regType"`
	RegTo       string `json:"regTo"`
	DeviceCount string `json:"deviceCount"`
	CaseModel   string `json:"caseModel"`
}

type cpuInfo struct {
	ModelName        string    `json:"modelName"`
	Cores            int       `json:"cores"`
	UsagePct         float64   `json:"usagePct"`
	PerCoreUsagePct  []float64 `json:"perCoreUsagePct"`
	PerCoreTempC     []float64 `json:"perCoreTempC"`
}

type memInfo struct {
	TotalBytes int64   `json:"totalBytes"`
	UsedBytes  int64   `json:"usedBytes"`
	CacheBytes int64   `json:"cacheBytes"`
	UsagePct   float64 `json:"usagePct"`
}

type netInfo struct {
	Iface         string `json:"iface"`
	RxBytesPerSec int64  `json:"rxBytesPerSec"`
	TxBytesPerSec int64  `json:"txBytesPerSec"`
	RxTotalBytes  int64  `json:"rxTotalBytes"`
	TxTotalBytes  int64  `json:"txTotalBytes"`
}

type rwRate struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

// Dashboard returns a snapshot of CPU / memory / network / array throughput.
//
// v0.9+: Uses the official Unraid GraphQL API (/graphql) as the primary data
// source when available. Falls back to SSH for real-time stats (with delta
// computation) when GraphQL is not available. HTML scraping is removed.
//
// GraphQL provides: CPU model, core count, memory layout, OS info, uptime,
// CPU/memory usage metrics. SSH provides: per-core CPU usage with deltas,
// per-core temps, real-time network/disk throughput rates.
func (h *Handler) Dashboard(c *gin.Context) {
	cli, sid, hasSSH, hasAPI := h.prepareServer(c)
	if sid == "" {
		return
	}

	// GraphQL-first: when the official API is available, use it for all
	// metadata and metrics. SSH is only needed for real-time delta-based
	// stats (per-core usage, network/disk throughput) which require two
	// /proc snapshots ~1s apart.
	if hasAPI && h.ur.HasGraphQL(sid) {
		h.dashboardGraphQL(c, sid, cli, hasSSH)
		return
	}

	if hasSSH {
		h.dashboardSSH(c, cli, sid)
		return
	}

	errOut(c, http.StatusServiceUnavailable, "Dashboard unavailable: GraphQL API not available and SSH not connected")
}

// readCSRFFromSSH reads csrf_token from Unraid's var.ini via SSH.
func readCSRFFromSSH(cli *ssh.Client) string {
	if cli == nil {
		return ""
	}
	// Unraid writes live state under /var/local/emhttp/; flash mirror under /boot/config/ is less current.
	for _, path := range []string{"/var/local/emhttp/var.ini", "/usr/local/emhttp/state/var.ini"} {
		out, err := cli.Run("grep -m1 '^csrf_token=' " + shellQuote(path) + " 2>/dev/null")
		if err != nil || out == "" {
			continue
		}
		// csrf_token="abcd..."
		line := strings.TrimSpace(out)
		if i := strings.Index(line, "="); i >= 0 {
			v := strings.Trim(line[i+1:], "\"' \t\r\n")
			if v != "" && v != "0000000000000000" {
				return v
			}
		}
	}
	return ""
}

// dashboardSSH is the full SSH-based dashboard with real-time stats.
func (h *Handler) dashboardSSH(c *gin.Context, cli *ssh.Client, sid string) {

	readStateFiles(cli) // reads var.ini/disks.ini for metadata (best-effort)

	// First snapshot: fire all commands concurrently (CPU still needs dual sample).
	var (
		cpu1, memInfo                                   string
		uptimeStr, loadStr, modelName, coreCountStr, temps string
	)

	var wg1 sync.WaitGroup
	wg1.Add(7)
	go func() { cpu1, _ = cli.Run("cat /proc/stat"); wg1.Done() }()
	go func() { memInfo, _ = cli.Run("cat /proc/meminfo"); wg1.Done() }()
	go func() { uptimeStr, _ = cli.Run("cat /proc/uptime"); wg1.Done() }()
	go func() { loadStr, _ = cli.Run("cat /proc/loadavg"); wg1.Done() }()
	go func() { modelName, _ = cli.Run("grep -m1 'model name' /proc/cpuinfo | cut -d: -f2 | sed 's/^ //'"); wg1.Done() }()
	go func() { coreCountStr, _ = cli.Run("nproc"); wg1.Done() }()
	go func() { temps, _ = cli.Run(readCoreTempCmd()); wg1.Done() }()
	wg1.Wait()

	// Disk/network rates: prefer inter-request cache (accurate over poll interval).
	arrayRw, network := sampleIORates(cli, sid)

	time.Sleep(400 * time.Millisecond)
	cpu2, _ := cli.Run("cat /proc/stat")

	nCores := atoiSafe(coreCountStr, 1)
	resp := dashboardResp{
		CPU: cpuInfo{
			ModelName:    strings.TrimSpace(modelName),
			Cores:        nCores,
			PerCoreTempC: mapTempsToLogicalCores(parseThermalSnapshot(temps), nCores),
		},
		Network: network,
		ArrayRw: arrayRw,
	}
	resp.CPU.UsagePct, resp.CPU.PerCoreUsagePct = computeCPUUsage(cpu1, cpu2, resp.CPU.Cores)

	resp.Memory = parseMeminfo(memInfo)
	resp.Uptime = parseUptime(uptimeStr)
	resp.LoadAvg = parseLoadAvg(loadStr)

	c.JSON(http.StatusOK, resp)
}

// computeCPUUsage parses the full /proc/stat output (which includes the
// aggregate `cpu` line followed by one `cpuN` line per logical core) and
// returns (avg%, per-core%). Both snapshots must come from `cat /proc/stat`
// *without* `head -n 1` so that the per-core rows are present.
//
// The avg is derived from the aggregate `cpu` row, the per-core slice from
// the cpuN rows. If a particular cpuN row is missing in either snapshot
// (rare race during hotplug) the corresponding entry is left at 0.
func computeCPUUsage(stat1, stat2 string, cores int) (float64, []float64) {
	a := parseProcStat(stat1)
	b := parseProcStat(stat2)

	avg := 0.0
	if l1, ok1 := a["cpu"]; ok1 {
		if l2, ok2 := b["cpu"]; ok2 {
			avg = cpuLineBusyPct(l1, l2)
		}
	}

	perCore := make([]float64, cores)
	for i := 0; i < cores; i++ {
		key := "cpu" + strconv.Itoa(i)
		l1, ok1 := a[key]
		l2, ok2 := b[key]
		if !ok1 || !ok2 {
			continue
		}
		perCore[i] = cpuLineBusyPct(l1, l2)
	}
	return avg, perCore
}

// cpuLine holds the numeric fields of a single /proc/stat cpu row, in their
// original order (user, nice, system, idle, iowait, irq, softirq, steal,
// guest, guest_nice). Everything except the label.
type cpuLine struct {
	fields []float64
}

// parseProcStat parses /proc/stat output into a map keyed by the first
// column label. Only "cpu" and "cpuN" rows are kept — intr/ctxt/btime etc.
// are dropped silently.
func parseProcStat(s string) map[string]cpuLine {
	out := map[string]cpuLine{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		label := f[0]
		if !strings.HasPrefix(label, "cpu") {
			continue
		}
		nums := make([]float64, 0, len(f)-1)
		for _, x := range f[1:] {
			nums = append(nums, atofSafe(x))
		}
		out[label] = cpuLine{fields: nums}
	}
	return out
}

// cpuLineBusyPct computes the busy percentage between two snapshots of the
// same cpu row.  Idle = fields[3] (iowait at index 4 counts as busy here —
// matches `parseCPUAggDelta`'s pre-v0.3 behaviour so the aggregate line
// keeps the same readings).
func cpuLineBusyPct(a, b cpuLine) float64 {
	if len(a.fields) < 4 || len(b.fields) < 4 {
		return 0
	}
	var total1, total2, idle1, idle2 float64
	for _, v := range a.fields {
		total1 += v
	}
	for _, v := range b.fields {
		total2 += v
	}
	idle1 = a.fields[3]
	idle2 = b.fields[3]
	dt := total2 - total1
	di := idle2 - idle1
	if dt <= 0 {
		return 0
	}
	pct := (1 - di/dt) * 100
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

func parseCPUAggDelta(s1, s2 string) float64 {
	a := parseProcStat(s1)
	b := parseProcStat(s2)
	l1, ok1 := a["cpu"]
	l2, ok2 := b["cpu"]
	if !ok1 || !ok2 {
		return 0
	}
	return cpuLineBusyPct(l1, l2)
}

func parseMeminfo(s string) memInfo {
	m := memInfo{}
	for _, line := range strings.Split(s, "\n") {
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Fields(strings.TrimSpace(kv[1]))
		if len(val) == 0 {
			continue
		}
		v := atoi64Safe(val[0]) * 1024 // meminfo reports in kB
		switch key {
		case "MemTotal":
			m.TotalBytes = v
		case "MemAvailable":
			m.CacheBytes = m.TotalBytes - v // approximate
		}
	}
	if m.TotalBytes > 0 {
		m.UsedBytes = m.TotalBytes - m.CacheBytes
		if m.UsedBytes < 0 {
			m.UsedBytes = 0
		}
		m.UsagePct = float64(m.UsedBytes) / float64(m.TotalBytes) * 100
	}
	return m
}

func computeNet(s1, s2 string, dt float64) []netInfo {
	first := parseNetDev(s1)
	second := parseNetDev(s2)
	out := []netInfo{}
	for name, b := range second {
		a := first[name]
		rxRate := int64(float64(b.rxTotal-a.rxTotal) / dt)
		txRate := int64(float64(b.txTotal-a.txTotal) / dt)
		if rxRate < 0 {
			rxRate = 0
		}
		if txRate < 0 {
			txRate = 0
		}
		// skip loopback for the dashboard
		if name == "lo" {
			continue
		}
		out = append(out, netInfo{
			Iface:         name,
			RxBytesPerSec: rxRate,
			TxBytesPerSec: txRate,
			RxTotalBytes:  b.rxTotal,
			TxTotalBytes:  b.txTotal,
		})
	}
	if len(out) == 0 {
		return []netInfo{{Iface: "-"}}
	}
	return out
}

type netRow struct {
	rxTotal int64
	txTotal int64
}

func parseNetDev(s string) map[string]netRow {
	out := map[string]netRow{}
	for _, line := range strings.Split(s, "\n") {
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:idx])
		rest := strings.Fields(line[idx+1:])
		if len(rest) < 16 {
			continue
		}
		out[name] = netRow{
			rxTotal: atoi64Safe(rest[0]),
			txTotal: atoi64Safe(rest[8]),
		}
	}
	return out
}

// computeDiskRW sums whole-disk (not partition) read/write byte deltas.
// /proc/diskstats sector counts are always in 512-byte units.
func computeDiskRW(s1, s2 string, dt float64) rwRate {
	a := parseDiskstats(s1)
	b := parseDiskstats(s2)
	return diskRWFromMaps(a, b, dt)
}

func diskRWFromMaps(a, b map[string]diskStat, dt float64) rwRate {
	if dt <= 0 {
		dt = 1
	}
	var rd, wr int64
	for dev, v2 := range b {
		v1, ok := a[dev]
		if !ok {
			continue
		}
		rd += int64(float64(v2.sectorsRead-v1.sectorsRead) * 512 / dt)
		wr += int64(float64(v2.sectorsWritten-v1.sectorsWritten) * 512 / dt)
	}
	if rd < 0 {
		rd = 0
	}
	if wr < 0 {
		wr = 0
	}
	return rwRate{Read: rd, Write: wr}
}

type diskStat struct {
	sectorsRead    int64
	sectorsWritten int64
}

// ioSnap is a timestamped /proc snapshot used for cross-request rate calc.
type ioSnap struct {
	at    time.Time
	disks map[string]diskStat
	nets  map[string]netRow
}

// ioRateCache remembers the previous diskstats/netdev snapshot per server so
// rates can be computed over the UI poll interval (2–15s) instead of only a
// short ~0.9s window inside one request. Longer windows catch intermittent I/O
// that a sub-second sample often reports as zero.
var ioRateCache = struct {
	sync.Mutex
	m map[string]ioSnap
}{m: map[string]ioSnap{}}

// sampleIORates returns array R/W + network rates for the server.
// Prefers inter-request deltas (cache hit); falls back to dual sample on first hit.
func sampleIORates(cli *ssh.Client, serverID string) (rwRate, []netInfo) {
	diskRaw, _ := cli.Run("cat /proc/diskstats")
	netRaw, _ := cli.Run("cat /proc/net/dev")
	now := time.Now()
	disks := parseDiskstats(diskRaw)
	nets := parseNetDev(netRaw)

	ioRateCache.Lock()
	prev, ok := ioRateCache.m[serverID]
	ioRateCache.m[serverID] = ioSnap{at: now, disks: disks, nets: nets}
	ioRateCache.Unlock()

	if ok {
		dt := now.Sub(prev.at).Seconds()
		// Accept 0.4s–60s windows (covers 1s–15s UI refresh + some jitter)
		if dt >= 0.4 && dt <= 60 && len(prev.disks) > 0 {
			rw := diskRWFromMaps(prev.disks, disks, dt)
			net := netRatesFromMaps(prev.nets, nets, dt)
			return rw, net
		}
	}

	// Cold start / stale cache: dual sample ~0.8s
	time.Sleep(800 * time.Millisecond)
	diskRaw2, _ := cli.Run("cat /proc/diskstats")
	netRaw2, _ := cli.Run("cat /proc/net/dev")
	now2 := time.Now()
	disks2 := parseDiskstats(diskRaw2)
	nets2 := parseNetDev(netRaw2)

	ioRateCache.Lock()
	ioRateCache.m[serverID] = ioSnap{at: now2, disks: disks2, nets: nets2}
	ioRateCache.Unlock()

	dt := 0.8
	if ok {
		// still use dual-sample window
	}
	return diskRWFromMaps(disks, disks2, dt), netRatesFromMaps(nets, nets2, dt)
}

func netRatesFromMaps(a, b map[string]netRow, dt float64) []netInfo {
	if dt <= 0 {
		dt = 1
	}
	out := []netInfo{}
	for name, nb := range b {
		if name == "lo" {
			continue
		}
		na, ok := a[name]
		if !ok {
			continue
		}
		rxRate := int64(float64(nb.rxTotal-na.rxTotal) / dt)
		txRate := int64(float64(nb.txTotal-na.txTotal) / dt)
		if rxRate < 0 {
			rxRate = 0
		}
		if txRate < 0 {
			txRate = 0
		}
		out = append(out, netInfo{
			Iface:         name,
			RxBytesPerSec: rxRate,
			TxBytesPerSec: txRate,
			RxTotalBytes:  nb.rxTotal,
			TxTotalBytes:  nb.txTotal,
		})
	}
	if len(out) == 0 {
		return []netInfo{{Iface: "-"}}
	}
	return out
}

func parseDiskstats(s string) map[string]diskStat {
	out := map[string]diskStat{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 14 {
			continue
		}
		dev := f[2]
		// Whole disks only — summing partitions + parents double-counts I/O
		// and can make rates look wrong or noisy.
		if !isWholeDiskName(dev) {
			continue
		}
		out[dev] = diskStat{
			sectorsRead:    atoi64Safe(f[5]),
			sectorsWritten: atoi64Safe(f[9]),
		}
	}
	return out
}

// isWholeDiskName reports whether a /proc/diskstats device name is a whole
// disk (not a partition). Examples that pass: sda, sdb, nvme0n1, md1, vda.
// Examples that fail: sda1, nvme0n1p1, md1p1, loop0.
func isWholeDiskName(dev string) bool {
	if dev == "" {
		return false
	}
	// nvme0n1 yes; nvme0n1p1 / nvme0n1p2 no
	if strings.HasPrefix(dev, "nvme") {
		// whole: nvme\d+n\d+
		for i := 0; i < len(dev); i++ {
			// crude but reliable: presence of 'p' after nN
			if i > 0 && dev[i] == 'p' && i+1 < len(dev) && dev[i+1] >= '0' && dev[i+1] <= '9' {
				return false
			}
		}
		return strings.Contains(dev, "n")
	}
	// md1 yes; md1p1 no
	if strings.HasPrefix(dev, "md") {
		for i := 2; i < len(dev); i++ {
			if dev[i] == 'p' {
				return false
			}
		}
		// require at least one digit
		for i := 2; i < len(dev); i++ {
			if dev[i] >= '0' && dev[i] <= '9' {
				return true
			}
		}
		return false
	}
	// mmcblk0 yes; mmcblk0p1 no
	if strings.HasPrefix(dev, "mmcblk") {
		return !strings.Contains(dev, "p")
	}
	// sda / vda / hda / xvda — whole disk ends with a letter, partitions end with digits
	if strings.HasPrefix(dev, "sd") || strings.HasPrefix(dev, "vd") ||
		strings.HasPrefix(dev, "hd") || strings.HasPrefix(dev, "xvd") {
		if len(dev) < 3 {
			return false
		}
		last := dev[len(dev)-1]
		return last < '0' || last > '9'
	}
	return false
}

// thermalSnapshot holds labeled core temperatures + optional CPU topology.
// Format produced by readCoreTempCmd:
//
//	L <logicalCPU> <core_id>
//	T <core_id> <millidegrees>
//	P <millidegrees>          (package / k10temp / thermal_zone fallback)
//	Legacy plain millidegree lines are also accepted.
type thermalSnapshot struct {
	logicalToCore map[int]int     // logical CPU index → physical core id
	coreTemp      map[int]float64 // physical core id → °C
	packageTemp   float64         // package-level °C; 0 if unknown
	legacy        []float64       // unlabeled plain readings (°C)
}

func parseThermalSnapshot(s string) thermalSnapshot {
	snap := thermalSnapshot{
		logicalToCore: map[int]int{},
		coreTemp:      map[int]float64{},
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		switch {
		case len(fields) >= 3 && fields[0] == "L":
			logCPU := atoiSafe(fields[1], -1)
			coreID := atoiSafe(fields[2], -1)
			if logCPU >= 0 && coreID >= 0 {
				snap.logicalToCore[logCPU] = coreID
			}
		case len(fields) >= 3 && fields[0] == "T":
			coreID := atoiSafe(fields[1], -1)
			temp := milliToCelsius(fields[2])
			if coreID >= 0 && temp > 0 {
				snap.coreTemp[coreID] = temp
			}
		case len(fields) >= 2 && fields[0] == "P":
			temp := milliToCelsius(fields[1])
			if temp > 0 {
				snap.packageTemp = temp
			}
		default:
			// legacy: bare millidegree value
			temp := milliToCelsius(line)
			if temp > 0 {
				snap.legacy = append(snap.legacy, temp)
			} else if temp == 0 {
				snap.legacy = append(snap.legacy, -1)
			}
		}
	}
	return snap
}

func milliToCelsius(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	if v > 1000 {
		v /= 1000
	}
	if v <= 0 {
		return 0
	}
	// Round to 1 decimal to avoid noisy UI floats
	return float64(int(v*10+0.5)) / 10
}

// parseThermal keeps backward-compatible unlabeled readings (tests / callers).
func parseThermal(s string) []float64 {
	return parseThermalSnapshot(s).legacy
}

func parseUptime(s string) int64 {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return 0
	}
	return int64(atofSafe(parts[0]))
}

func parseLoadAvg(s string) [3]float64 {
	var l [3]float64
	parts := strings.Fields(s)
	for i := 0; i < 3 && i < len(parts); i++ {
		l[i] = atofSafe(parts[i])
	}
	return l
}

/* ---- parse helpers ---- */

func atoiSafe(s string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return v
}

func atoi64Safe(s string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func atofSafe(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

// readCoreTempCmd returns a shell command that reads CPU topology + temperatures.
//
// Output lines (parsed by parseThermalSnapshot):
//
//	L <logicalCPU> <core_id>     — from /sys/.../topology/core_id
//	T <core_id> <millidegrees>   — Intel coretemp "Core N"
//	P <millidegrees>             — package / k10temp / thermal_zone fallback
//
// Strategy:
//  1. Always emit topology (L lines) when available.
//  2. coretemp: T lines for Core*, P for Package*.
//  3. k10temp / thermal_zone: single P package reading.
func readCoreTempCmd() string {
	return `(
    # Topology: logical CPU → physical core id (for HT sibling mapping)
    for c in /sys/devices/system/cpu/cpu[0-9]*; do
      [ -f "$c/topology/core_id" ] || continue
      id=${c##*/cpu}
      core=$(cat "$c/topology/core_id" 2>/dev/null) || continue
      echo "L $id $core"
    done
    # Intel coretemp
    p=$(grep -xl coretemp /sys/class/hwmon/hwmon*/name 2>/dev/null | head -1)
    if [ -n "$p" ]; then
      d=${p%/*}
      for f in $(ls $d/temp*_input 2>/dev/null | sort -V); do
        lbl=${f%_input}_label
        val=$(cat "$f" 2>/dev/null) || continue
        if [ -f "$lbl" ]; then
          label=$(cat "$lbl" 2>/dev/null)
          case "$label" in
            Core*)
              num=$(echo "$label" | tr -dc '0-9')
              [ -n "$num" ] && echo "T $num $val"
              ;;
            Package*)
              echo "P $val"
              ;;
          esac
        else
          echo "P $val"
        fi
      done
      exit 0
    fi
    # AMD k10temp (package)
    p=$(grep -xl k10temp /sys/class/hwmon/hwmon*/name 2>/dev/null | head -1)
    if [ -n "$p" ]; then
      d=${p%/*}
      for f in $(ls $d/temp*_input 2>/dev/null | sort -V); do
        echo "P $(cat "$f" 2>/dev/null)"
        break
      done
      exit 0
    fi
    # thermal_zone fallback
    for z in /sys/class/thermal/thermal_zone*; do
      t=$(cat "$z/temp" 2>/dev/null) || continue
      [ -n "$t" ] && echo "P $t" && break
    done
  )`
}

// mapTempsToLogicalCores builds a per-logical-CPU temperature slice (°C).
// -1 means no reading. Uses topology (L lines) + labeled core temps when present.
//
// Intel HT topology (typical):
//
//	logical 0..3 → core_id 0..3, logical 4..7 → core_id 0..3
//	temps T0..T3 → out [t0,t1,t2,t3,t0,t1,t2,t3]
//
// The old expandCoreTemps interleaved as [t0,t0,t1,t1,...] which misaligned
// sibling threads and made the per-core panel look wrong.
func mapTempsToLogicalCores(snap thermalSnapshot, nCores int) []float64 {
	if nCores <= 0 {
		return nil
	}
	out := make([]float64, nCores)
	for i := range out {
		out[i] = -1
	}

	// Preferred: topology + labeled core temps
	if len(snap.coreTemp) > 0 && len(snap.logicalToCore) > 0 {
		for logCPU := 0; logCPU < nCores; logCPU++ {
			coreID, ok := snap.logicalToCore[logCPU]
			if !ok {
				continue
			}
			if t, ok := snap.coreTemp[coreID]; ok && t > 0 {
				out[logCPU] = t
			} else if snap.packageTemp > 0 {
				out[logCPU] = snap.packageTemp
			}
		}
		return out
	}

	// Labeled core temps without topology — assume sorted core ids and HT second half
	if len(snap.coreTemp) > 0 {
		ids := make([]int, 0, len(snap.coreTemp))
		for id := range snap.coreTemp {
			ids = append(ids, id)
		}
		// sort ascending
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				if ids[j] < ids[i] {
					ids[i], ids[j] = ids[j], ids[i]
				}
			}
		}
		phy := make([]float64, len(ids))
		for i, id := range ids {
			phy[i] = snap.coreTemp[id]
		}
		return expandCoreTempsHT(phy, nCores)
	}

	// Legacy plain list
	if len(snap.legacy) > 0 {
		return expandCoreTempsHT(snap.legacy, nCores)
	}

	// Package-only: same temp on every logical core
	if snap.packageTemp > 0 {
		for i := range out {
			out[i] = snap.packageTemp
		}
	}
	return out
}

// expandCoreTempsHT maps N physical-core temps onto M logical CPUs.
// When M == 2N (classic HT), siblings share the second half:
//
//	[t0,t1,t2,t3] + 8 → [t0,t1,t2,t3, t0,t1,t2,t3]
//
// Not the incorrect adjacent-pair layout [t0,t0,t1,t1,...].
func expandCoreTempsHT(phyTemps []float64, nCores int) []float64 {
	nPhys := len(phyTemps)
	if nPhys == 0 || nCores == 0 {
		return phyTemps
	}
	if nPhys >= nCores {
		return phyTemps[:nCores]
	}
	out := make([]float64, nCores)
	if nCores%nPhys == 0 {
		// e.g. 2× HT: fill sequential blocks of physical order
		groups := nCores / nPhys
		for g := 0; g < groups; g++ {
			for i := 0; i < nPhys; i++ {
				out[g*nPhys+i] = phyTemps[i]
			}
		}
		return out
	}
	// Uneven: cycle physical temps
	for i := 0; i < nCores; i++ {
		out[i] = phyTemps[i%nPhys]
	}
	return out
}

// expandCoreTemps is kept for any external callers; uses HT-aware expansion.
func expandCoreTemps(phyTemps []float64, nCores int) []float64 {
	return expandCoreTempsHT(phyTemps, nCores)
}

// ---------------------------------------------------------------------------
// Dashboard via official Unraid GraphQL API
// ---------------------------------------------------------------------------

// dashboardGraphQL uses the Unraid GraphQL API for system info and metrics.
// When SSH is also available, it supplements with real-time delta stats
// (per-core temps, array disk throughput) that GraphQL does not expose.
func (h *Handler) dashboardGraphQL(c *gin.Context, sid string, cli *ssh.Client, hasSSH bool) {
	resp := dashboardResp{
		CPU:     cpuInfo{PerCoreUsagePct: []float64{}, PerCoreTempC: []float64{}},
		Network: []netInfo{{Iface: "-"}},
	}

	// Fetch system info + metrics concurrently
	var infoData json.RawMessage
	var metricsData json.RawMessage
	var infoErr, metricsErr error
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		infoData, infoErr = h.ur.GraphQLQuery(sid, unraid.QueryGetSystemInfo, nil)
		wg.Done()
	}()
	go func() {
		metricsData, metricsErr = h.ur.GraphQLQuery(sid, unraid.QueryGetMetrics, nil)
		wg.Done()
	}()
	wg.Wait()

	// If both queries failed (e.g. CSRF/auth error), fall back to SSH
	if infoErr != nil && metricsErr != nil {
		logger.Warnf("dashboard graphql both queries failed for %s (info: %v, metrics: %v), falling back to SSH", sid, infoErr, metricsErr)
		if hasSSH {
			h.dashboardSSH(c, cli, sid)
			return
		}
		// No SSH either -- return empty shell so UI stays up
		c.JSON(http.StatusOK, resp)
		return
	}
	if infoErr != nil {
		logger.Warnf("dashboard graphql info query failed for %s: %v", sid, infoErr)
	}
	if metricsErr != nil {
		logger.Warnf("dashboard graphql metrics query failed for %s: %v", sid, metricsErr)
	}

	gotInfo, gotMetrics := false, false

	// Parse system info
	if infoErr == nil && infoData != nil {
		info, err := unraid.ParseInfoQuery(infoData)
		if err != nil {
			logger.Warnf("dashboard graphql info parse failed for %s: %v", sid, err)
		} else if info != nil {
			gotInfo = true
			if info.CPU != nil {
				resp.CPU.ModelName = info.CPU.Brand
				if info.CPU.Brand == "" {
					resp.CPU.ModelName = info.CPU.Manufacturer
				}
				// Prefer threads (logical cores) for per-core charts; fall back to physical cores.
				resp.CPU.Cores = info.CPU.Threads
				if resp.CPU.Cores == 0 {
					resp.CPU.Cores = info.CPU.Cores
				}
			}
			if info.OS != nil {
				// Official API: os.uptime is boot-time ISO string → converted to seconds.
				resp.Uptime = info.OS.Uptime.Seconds
			}
			resp.ServerMeta = &serverMeta{
				Name: func() string {
					if info.OS != nil {
						return info.OS.Hostname
					}
					return ""
				}(),
				OSVersion: func() string {
					if info.Versions != nil && info.Versions.Core != nil {
						return info.Versions.Core.Unraid
					}
					return ""
				}(),
				Model: func() string {
					if info.System != nil {
						return info.System.Model
					}
					return ""
				}(),
			}
		}
	}

	// Parse metrics
	if metricsErr == nil && metricsData != nil {
		metrics, err := unraid.ParseMetricsQuery(metricsData)
		if err != nil {
			logger.Warnf("dashboard graphql metrics parse failed for %s: %v", sid, err)
		} else if metrics != nil {
			gotMetrics = true
			if metrics.CPU != nil {
				resp.CPU.UsagePct = metrics.CPU.PercentTotal
				if len(metrics.CPU.Cpus) > 0 {
					per := make([]float64, len(metrics.CPU.Cpus))
					for i, core := range metrics.CPU.Cpus {
						per[i] = core.PercentTotal
					}
					resp.CPU.PerCoreUsagePct = per
					if resp.CPU.Cores == 0 {
						resp.CPU.Cores = len(per)
					}
				}
			}
			if metrics.Memory != nil {
				resp.Memory = memInfo{
					TotalBytes: metrics.Memory.Total.Int64(),
					UsedBytes:  metrics.Memory.Used.Int64(),
					CacheBytes: metrics.Memory.BuffCache.Int64(),
					UsagePct:   metrics.Memory.PercentTotal,
				}
				// If used is 0 but we have total+available, derive used.
				if resp.Memory.UsedBytes == 0 && resp.Memory.TotalBytes > 0 && metrics.Memory.Available.Int64() > 0 {
					resp.Memory.UsedBytes = resp.Memory.TotalBytes - metrics.Memory.Available.Int64()
					if resp.Memory.UsedBytes < 0 {
						resp.Memory.UsedBytes = 0
					}
					if resp.Memory.UsagePct == 0 {
						resp.Memory.UsagePct = float64(resp.Memory.UsedBytes) / float64(resp.Memory.TotalBytes) * 100
					}
				}
			}
			// Network throughput from GraphQL (rxSec/txSec already computed server-side)
			if len(metrics.Network) > 0 {
				nets := make([]netInfo, 0, len(metrics.Network))
				for _, n := range metrics.Network {
					if n.Name == "" || n.Name == "lo" {
						continue
					}
					// Prefer interfaces that are up / have traffic
					nets = append(nets, netInfo{
						Iface:         n.Name,
						RxBytesPerSec: int64(n.RxSec),
						TxBytesPerSec: int64(n.TxSec),
						RxTotalBytes:  n.BytesReceived.Int64(),
						TxTotalBytes:  n.BytesSent.Int64(),
					})
				}
				if len(nets) > 0 {
					resp.Network = nets
				}
			}
		}
	}

	// If metrics failed entirely but SSH is available, fill CPU/mem from SSH.
	// (Info may have succeeded — still want live usage numbers.)
	if !gotMetrics && hasSSH && cli != nil {
		logger.Infof("dashboard graphql metrics missing for %s, filling usage from SSH", sid)
		h.fillUsageFromSSH(cli, &resp)
	}

	// If SSH is available, always enrich with temps + disk R/W (GraphQL lacks these).
	// Also overwrites per-core / network with higher-fidelity /proc deltas when present.
	if hasSSH && cli != nil {
		h.enrichWithSSHDeltas(cli, sid, &resp)
	}

	// If we got neither info nor metrics and no SSH enrichment, try full SSH fallback.
	if !gotInfo && !gotMetrics && hasSSH && cli != nil && resp.CPU.UsagePct == 0 && resp.Memory.TotalBytes == 0 {
		h.dashboardSSH(c, cli, sid)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// fillUsageFromSSH populates CPU usage and memory from a single SSH snapshot
// (no 1s delta sleep). Used when GraphQL metrics fail but SSH works.
func (h *Handler) fillUsageFromSSH(cli *ssh.Client, resp *dashboardResp) {
	var cpu1, memStr, loadStr string
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { cpu1, _ = cli.Run("cat /proc/stat"); wg.Done() }()
	go func() { memStr, _ = cli.Run("cat /proc/meminfo"); wg.Done() }()
	go func() { loadStr, _ = cli.Run("cat /proc/loadavg"); wg.Done() }()
	wg.Wait()

	time.Sleep(400 * time.Millisecond)
	cpu2, _ := cli.Run("cat /proc/stat")

	cores := resp.CPU.Cores
	if cores == 0 {
		if nproc, err := cli.Run("nproc"); err == nil {
			cores = atoiSafe(nproc, 1)
			resp.CPU.Cores = cores
		} else {
			cores = 1
		}
	}
	resp.CPU.UsagePct, resp.CPU.PerCoreUsagePct = computeCPUUsage(cpu1, cpu2, cores)
	if memStr != "" {
		resp.Memory = parseMeminfo(memStr)
	}
	if loadStr != "" {
		resp.LoadAvg = parseLoadAvg(loadStr)
	}
	if resp.CPU.ModelName == "" {
		if model, err := cli.Run("grep -m1 'model name' /proc/cpuinfo | cut -d: -f2 | sed 's/^ //'"); err == nil {
			resp.CPU.ModelName = strings.TrimSpace(model)
		}
	}
	if resp.Uptime == 0 {
		if up, err := cli.Run("cat /proc/uptime"); err == nil {
			resp.Uptime = parseUptime(up)
		}
	}
}

// enrichWithSSHDeltas supplements a GraphQL-based dashboard response with
// real-time stats from SSH: per-core CPU usage, per-core temps, network
// throughput, and array disk throughput.
func (h *Handler) enrichWithSSHDeltas(cli *ssh.Client, sid string, resp *dashboardResp) {
	var cpu1, temps string
	var coreCountStr string
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { cpu1, _ = cli.Run("cat /proc/stat"); wg.Done() }()
	go func() { temps, _ = cli.Run(readCoreTempCmd()); wg.Done() }()
	go func() { coreCountStr, _ = cli.Run("nproc"); wg.Done() }()
	wg.Wait()

	// Array R/W + network from cross-request cache (preferred) or dual sample.
	arrayRw, network := sampleIORates(cli, sid)
	resp.ArrayRw = arrayRw
	// Prefer GraphQL network if it already has real rates; otherwise SSH.
	if len(resp.Network) == 0 || (len(resp.Network) == 1 && resp.Network[0].Iface == "-") ||
		(resp.Network[0].RxBytesPerSec == 0 && resp.Network[0].TxBytesPerSec == 0) {
		resp.Network = network
	}

	time.Sleep(400 * time.Millisecond)
	cpu2, _ := cli.Run("cat /proc/stat")

	// Overwrite CPU usage with per-core data from SSH deltas
	cores := resp.CPU.Cores
	if cores == 0 {
		cores = atoiSafe(coreCountStr, 1)
		resp.CPU.Cores = cores
	}
	resp.CPU.UsagePct, resp.CPU.PerCoreUsagePct = computeCPUUsage(cpu1, cpu2, cores)
	resp.CPU.PerCoreTempC = mapTempsToLogicalCores(parseThermalSnapshot(temps), cores)

	// Load average (SSH-only)
	if loadStr, err := cli.Run("cat /proc/loadavg"); err == nil {
		resp.LoadAvg = parseLoadAvg(loadStr)
	}
}

// Regex patterns for scraping Unraid Dashboard HTML.

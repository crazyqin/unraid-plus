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
	cli, sid, hasSSH, hasAPI := h.resolveServer(c)
	if sid == "" {
		return
	}

	// Lazy GraphQL probe: connect/reconnect fire ProbeGraphQL async, so the first
	// dashboard request may race. EnsureGraphQL runs a one-shot probe if needed.
	if hasAPI && !h.ur.HasGraphQL(sid) {
		h.ur.EnsureGraphQL(sid)
	}

	// Optionally refresh CSRF from SSH var.ini (most reliable source).
	if hasAPI && hasSSH && cli != nil && h.ur.CsrfToken(sid) == "" {
		if tok := readCSRFFromSSH(cli); tok != "" {
			h.ur.SetCSRFToken(sid, tok)
		}
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
		h.dashboardSSH(c, cli)
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
func (h *Handler) dashboardSSH(c *gin.Context, cli *ssh.Client) {

	readStateFiles(cli) // reads var.ini/disks.ini for metadata (best-effort)

	// First snapshot: fire all commands concurrently.
	var (
		cpu1, memInfo, net1, disk1                          string
		uptimeStr, loadStr, modelName, coreCountStr, temps string
	)

	var wg1 sync.WaitGroup
	wg1.Add(9)
	go func() { cpu1, _ = cli.Run("cat /proc/stat"); wg1.Done() }()
	go func() { memInfo, _ = cli.Run("cat /proc/meminfo"); wg1.Done() }()
	go func() { net1, _ = cli.Run("cat /proc/net/dev"); wg1.Done() }()
	go func() { disk1, _ = cli.Run("cat /proc/diskstats"); wg1.Done() }()
	go func() { uptimeStr, _ = cli.Run("cat /proc/uptime"); wg1.Done() }()
	go func() { loadStr, _ = cli.Run("cat /proc/loadavg"); wg1.Done() }()
	go func() { modelName, _ = cli.Run("grep -m1 'model name' /proc/cpuinfo | cut -d: -f2 | sed 's/^ //'"); wg1.Done() }()
	go func() { coreCountStr, _ = cli.Run("nproc"); wg1.Done() }()
	go func() { temps, _ = cli.Run(readCoreTempCmd()); wg1.Done() }()
	wg1.Wait()

	time.Sleep(900 * time.Millisecond)

	// Second snapshot: only the delta-dependent commands.
	var cpu2, net2, disk2 string
	var wg2 sync.WaitGroup
	wg2.Add(3)
	go func() { cpu2, _ = cli.Run("cat /proc/stat"); wg2.Done() }()
	go func() { net2, _ = cli.Run("cat /proc/net/dev"); wg2.Done() }()
	go func() { disk2, _ = cli.Run("cat /proc/diskstats"); wg2.Done() }()
	wg2.Wait()

	resp := dashboardResp{
		CPU: cpuInfo{
			ModelName:    strings.TrimSpace(modelName),
			Cores:        atoiSafe(coreCountStr, 1),
			PerCoreTempC: expandCoreTemps(parseThermal(temps), atoiSafe(coreCountStr, 1)),
		},
	}
	resp.CPU.UsagePct, resp.CPU.PerCoreUsagePct = computeCPUUsage(cpu1, cpu2, resp.CPU.Cores)

	resp.Memory = parseMeminfo(memInfo)
	resp.Network = computeNet(net1, net2, 0.9)
	resp.ArrayRw = computeDiskRW(disk1, disk2, 0.9)
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

// computeDiskRW sums all sd/nvme/vd devices' read/write byte deltas.
func computeDiskRW(s1, s2 string, dt float64) rwRate {
	a := parseDiskstats(s1)
	b := parseDiskstats(s2)
	var rd, wr int64
	for dev, v2 := range b {
		v1 := a[dev]
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

func parseDiskstats(s string) map[string]diskStat {
	out := map[string]diskStat{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 14 {
			continue
		}
		dev := f[2]
		// filter to physical-looking devices only
		if !(strings.HasPrefix(dev, "sd") || strings.HasPrefix(dev, "nvme") || strings.HasPrefix(dev, "vd") || strings.HasPrefix(dev, "md")) {
			continue
		}
		out[dev] = diskStat{
			sectorsRead:    atoi64Safe(f[5]),
			sectorsWritten: atoi64Safe(f[9]),
		}
	}
	return out
}

func parseThermal(s string) []float64 {
	out := []float64{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		v, err := strconv.ParseFloat(line, 64)
		if err != nil {
			continue
		}
		// /sys/class/thermal and hwmon report in millidegrees
		if v > 1000 {
			v /= 1000
		}
		// A reading of 0°C is nonsensical for a running CPU — it typically
		// means the core is in a deep C-state and the sensor returned 0.
		// Treat it as unavailable by skipping the entry entirely so the
		// frontend shows "—" instead of "0°C".
		if v <= 0 {
			out = append(out, -1) // -1 = no reading
			continue
		}
		out = append(out, v)
	}
	return out
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

// readCoreTempCmd returns a shell command that reads per-core CPU temperatures.
//
// Strategy (in order of preference):
//  1. hwmon "coretemp" (Intel): reads temp*_input where temp*_label starts with
//     "Core" or "Package" to distinguish per-core vs package temps. Each Core
//     label maps to one logical core.
//  2. hwmon "k10temp" (AMD): usually provides Tctl/Tdie (package temps).
//  3. Fallback: thermal_zone (package-level temp, often just 1-2 zones).
//
// The command outputs one temperature value per line in millidegrees, matching
// the format expected by parseThermal.
func readCoreTempCmd() string {
	return `( 
    p=$(grep -xl coretemp /sys/class/hwmon/hwmon*/name 2>/dev/null | head -1)
    if [ -n "$p" ]; then
      d=${p%/*}
      # Intel coretemp: temp1=Package, temp2=Core0, temp3=Core1, ...
      # Only read entries labeled "Core N" — skip "Package id N"
      for f in $(ls $d/temp*_input 2>/dev/null | sort -t'p' -k2 -n); do
        lbl=${f%_input}_label
        if [ -f "$lbl" ]; then
          label=$(cat "$lbl" 2>/dev/null)
          case "$label" in
            Core*) cat "$f" 2>/dev/null ;;
          esac
        else
          # No label file: include it (some virtual zones lack labels)
          cat "$f" 2>/dev/null
        fi
      done
      exit 0
    fi
    p=$(grep -xl k10temp /sys/class/hwmon/hwmon*/name 2>/dev/null | head -1)
    if [ -n "$p" ]; then
      d=${p%/*}
      for f in $(ls $d/temp*_input 2>/dev/null | sort -t'p' -k2 -n); do cat "$f" 2>/dev/null; done
      exit 0
    fi
    for z in /sys/class/thermal/thermal_zone*; do cat $z/temp 2>/dev/null || true; done
  )`
}

// expandCoreTemps maps physical core temperatures to logical cores.
//
// Intel CPUs with Hyper-Threading have N physical cores but 2N logical cores.
// The coretemp driver exposes one temperature per physical core, so the parsed
// array may be shorter than nproc. For example, an i5-8279U (4C/8T) reports
// 4 temps for 8 logical cores.
//
// This function expands the shorter physical-core array to match nCores by
// duplicating each physical core's temperature for its sibling threads:
//
//	phyTemps=[t0,t1,t2,t3] + nCores=8 → [t0,t0,t1,t1,t2,t2,t3,t3]
//
// If the temperature count already matches nCores (AMD or non-HT Intel),
// the array is returned unchanged. If there are more temps than cores, the
// excess is trimmed. If there are zero temps, an empty array is returned.
func expandCoreTemps(phyTemps []float64, nCores int) []float64 {
	nPhys := len(phyTemps)
	if nPhys == 0 || nCores == 0 {
		return phyTemps
	}
	// Already matches or more temps than cores — return as-is
	if nPhys >= nCores {
		return phyTemps[:nCores]
	}
	// Expand: each physical core's temp maps to (nCores/nPhys) logical cores
	ratio := nCores / nPhys
	out := make([]float64, 0, nCores)
	for _, t := range phyTemps {
		for j := 0; j < ratio; j++ {
			out = append(out, t)
		}
	}
	// Pad remaining if nCores is not evenly divisible
	for len(out) < nCores {
		out = append(out, phyTemps[len(phyTemps)-1])
	}
	return out
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
			h.dashboardSSH(c, cli)
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
		h.enrichWithSSHDeltas(cli, &resp)
	}

	// If we got neither info nor metrics and no SSH enrichment, try full SSH fallback.
	if !gotInfo && !gotMetrics && hasSSH && cli != nil && resp.CPU.UsagePct == 0 && resp.Memory.TotalBytes == 0 {
		h.dashboardSSH(c, cli)
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
// real-time delta stats from SSH: per-core CPU usage, per-core temps,
// network throughput, and array disk throughput. These require two snapshots
// ~1s apart and can only be computed from /proc/* files via SSH.
func (h *Handler) enrichWithSSHDeltas(cli *ssh.Client, resp *dashboardResp) {
	var cpu1, net1, disk1, temps string
	var coreCountStr string
	var wg sync.WaitGroup
	wg.Add(5)
	go func() { cpu1, _ = cli.Run("cat /proc/stat"); wg.Done() }()
	go func() { net1, _ = cli.Run("cat /proc/net/dev"); wg.Done() }()
	go func() { disk1, _ = cli.Run("cat /proc/diskstats"); wg.Done() }()
	go func() { temps, _ = cli.Run(readCoreTempCmd()); wg.Done() }()
	go func() { coreCountStr, _ = cli.Run("nproc"); wg.Done() }()
	wg.Wait()

	time.Sleep(900 * time.Millisecond)

	var cpu2, net2, disk2 string
	var wg2 sync.WaitGroup
	wg2.Add(3)
	go func() { cpu2, _ = cli.Run("cat /proc/stat"); wg2.Done() }()
	go func() { net2, _ = cli.Run("cat /proc/net/dev"); wg2.Done() }()
	go func() { disk2, _ = cli.Run("cat /proc/diskstats"); wg2.Done() }()
	wg2.Wait()

	// Overwrite CPU usage with per-core data from SSH deltas
	cores := resp.CPU.Cores
	if cores == 0 {
		cores = atoiSafe(coreCountStr, 1)
		resp.CPU.Cores = cores
	}
	resp.CPU.UsagePct, resp.CPU.PerCoreUsagePct = computeCPUUsage(cpu1, cpu2, cores)
	resp.CPU.PerCoreTempC = expandCoreTemps(parseThermal(temps), cores)
	resp.Network = computeNet(net1, net2, 0.9)
	resp.ArrayRw = computeDiskRW(disk1, disk2, 0.9)

	// Load average (SSH-only)
	if loadStr, err := cli.Run("cat /proc/loadavg"); err == nil {
		resp.LoadAvg = parseLoadAvg(loadStr)
	}
}

// Regex patterns for scraping Unraid Dashboard HTML.

package handler

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/crazyqin/unraid-plus/server/internal/ssh"
)

type dashboardResp struct {
	CPU       cpuInfo     `json:"cpu"`
	Memory    memInfo     `json:"memory"`
	Network   []netInfo   `json:"network"`
	ArrayRw   rwRate      `json:"arrayRwBytesPerSec"`
	Uptime    int64       `json:"uptime"`
	LoadAvg   [3]float64  `json:"loadAvg"`
	Degraded  bool        `json:"degraded,omitempty"`  // true when data from HTML scraping (API-only mode)
	DegradedReason string `json:"degradedReason,omitempty"` // "ssh_unavailable" etc.
	ServerMeta *serverMeta `json:"serverMeta,omitempty"` // Unraid server metadata from user-profile JSON
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
// v0.7+ uses Unraid state files (/usr/local/emhttp/state/var.ini) for
// server metadata (version, name) when available. Live CPU/mem/net/disk
// stats still come from /proc/* since those need real-time deltas.
//
// v0.7+: When SSH is unavailable but WebGUI API is available, falls back
// to scraping the Unraid Dashboard/Main page HTML for basic system info.
// The response includes degraded=true and the frontend can show a "limited
// data" indicator.
func (h *Handler) Dashboard(c *gin.Context) {
	cli, sid, hasSSH, hasAPI := h.resolveServer(c)
	if sid == "" {
		return
	}

	if hasSSH {
		h.dashboardSSH(c, cli)
		return
	}

	// API-only fallback: scrape HTML from Unraid WebGUI
	if hasAPI {
		h.dashboardAPI(c, sid)
		return
	}

	// Should not reach here (resolveServer returns error if both unavailable)
	errOut(c, http.StatusServiceUnavailable, "仪表盘不可用")
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
// Dashboard API fallback (HTML scraping from Unraid WebGUI)
// ---------------------------------------------------------------------------

// dashboardAPI fetches system info from the Unraid WebGUI when SSH is unavailable.
// Unraid 7.x uses Nchan WebSocket for real-time updates, but the initial page
// load still contains key data in embedded JavaScript variables and in the
// <unraid-user-profile> custom element's server attribute (JSON).
func (h *Handler) dashboardAPI(c *gin.Context, sid string) {
	resp := dashboardResp{
		Network:         []netInfo{{Iface: "-"}},
		CPU:             cpuInfo{PerCoreUsagePct: []float64{}, PerCoreTempC: []float64{}},
		Degraded:        true,
		DegradedReason:  "ssh_unavailable",
	}

	// Fetch /Main page for JS variables, array state, and user-profile JSON
	if body, status, err := h.ur.FetchPage(sid, "/Main"); err == nil && status == 200 {
		html := string(body)
		parseMainPageJS(html, &resp)
		// Extract server metadata from <unraid-user-profile server="...">
		resp.ServerMeta = parseUserProfileJSON(html)
	}

	// Try SystemInformation.php for CPU/memory data
	if body, status, err := h.ur.FetchEndpoint(sid, "/plugins/dynamix/include/SystemInformation.php"); err == nil && status == 200 {
		parseSystemInfoHTML(string(body), &resp)
	}

	// Try Temperature.php for CPU temp
	if body, status, err := h.ur.FetchEndpoint(sid, "/plugins/dynamix/include/Temperature.php"); err == nil && status == 200 {
		parseTemperatureHTML(string(body), &resp)
	}

	// Try /Dashboard page which may contain more system info in the
	// user-profile component or dashboard-specific panels
	if body, status, err := h.ur.FetchPage(sid, "/Dashboard"); err == nil && status == 200 {
		html := string(body)
		// Extract server metadata if /Main didn't provide it
		if resp.ServerMeta == nil {
			resp.ServerMeta = parseUserProfileJSON(html)
		}
		// Dashboard page may have system info in the statusbar or gauge elements
		if resp.CPU.ModelName == "" {
			parseDashboardHTMLSysInfo(html, &resp)
		}
	}

	c.JSON(http.StatusOK, resp)
}

// Regex patterns for scraping Unraid Dashboard HTML.
// The Unraid dashboard page embeds system info in various HTML elements.
// These patterns are best-effort and may vary across Unraid versions.
var (
	// CPU model from "CPU:" or "Processor:" label
	reCPUModel = regexp.MustCompile(`(?i)(?:CPU|Processor)\s*[:：]\s*<[^>]*>([^<]+)<`)
	// Memory from "Memory:" label (e.g. "17.3 GB / 31.2 GB")
	reMemoryLabel = regexp.MustCompile(`(?i)Memory\s*[:：]\s*<[^>]*>([^<]+)<`)
	// Uptime from "Uptime:" label (e.g. "4 days, 16 hours, 20 minutes")
	reUptimeLabel = regexp.MustCompile(`(?i)Uptime\s*[:：]\s*<[^>]*>([^<]+)<`)
	// Array state from mdState or similar (e.g. "Started", "Stopped")
	reArrayState = regexp.MustCompile(`(?i)class=['"][^'"]*array[^'"]*['"][^>]*>([^<]+)<`)
	// CPU usage from dashboard gauge/percentage (e.g. "1.9%")
	reCPUUsage = regexp.MustCompile(`(?i)(?:CPU\s+Usage|CPU\s+Load)\s*[:：]\s*<[^>]*>([^<]*%?)<`)
	// System info block — broader pattern for the info panel
	reSysInfoBlock = regexp.MustCompile(`(?s)<div[^>]*class=['"][^'"]*sys-info[^'"]*['"][^>]*>(.*?)</div>`)
)

// parseDashboardHTML extracts system info from the Unraid dashboard HTML.
// Returns a dashboardResp with whatever data we could extract; fields we
// couldn't find are left at their zero values.
func parseDashboardHTML(html string) dashboardResp {
	resp := dashboardResp{
		Network:         []netInfo{{Iface: "-"}},
		CPU: cpuInfo{
			PerCoreUsagePct: []float64{},
			PerCoreTempC:    []float64{},
		},
	}

	// Extract CPU model
	if m := reCPUModel.FindStringSubmatch(html); len(m) > 1 {
		resp.CPU.ModelName = strings.TrimSpace(m[1])
	}

	// Extract CPU usage percentage (if available in initial HTML)
	if m := reCPUUsage.FindStringSubmatch(html); len(m) > 1 {
		usageStr := strings.TrimSpace(m[1])
		usageStr = strings.TrimSuffix(usageStr, "%")
		resp.CPU.UsagePct = atofSafe(usageStr)
	}

	// Extract memory info (format: "used / total" or "X.X GB / Y.Y GB")
	if m := reMemoryLabel.FindStringSubmatch(html); len(m) > 1 {
		memStr := strings.TrimSpace(m[1])
		resp.Memory = parseMemoryHTMLStr(memStr)
	}

	// Extract uptime
	if m := reUptimeLabel.FindStringSubmatch(html); len(m) > 1 {
		uptimeStr := strings.TrimSpace(m[1])
		resp.Uptime = parseUptimeHTMLStr(uptimeStr)
	}

	// Try to extract CPU core count from a "Cores:" label or nproc-like value
	reCores := regexp.MustCompile(`(?i)(?:Cores|CPU\s*\(s\))\s*[:：]\s*<[^>]*>(\d+)`)
	if m := reCores.FindStringSubmatch(html); len(m) > 1 {
		resp.CPU.Cores = atoiSafe(m[1], 1)
	}

	return resp
}

// parseSystemInfoHTML extracts CPU model, memory, and core count from
// Unraid's SystemInformation.php output.
func parseSystemInfoHTML(html string, resp *dashboardResp) {
	reCPU := regexp.MustCompile(`(?i)(?:CPU|Processor|model name)\s*[:：]\s*(?:<[^>]*>)?\s*([^<\n]+)`)
	if m := reCPU.FindStringSubmatch(html); len(m) > 1 {
		resp.CPU.ModelName = strings.TrimSpace(m[1])
	}
	reCPUUsage := regexp.MustCompile(`(?i)(?:CPU\s+Usage|CPU\s+Load)\s*[:：]\s*(?:<[^>]*>)?\s*([^<]*%?)`)
	if m := reCPUUsage.FindStringSubmatch(html); len(m) > 1 {
		usageStr := strings.TrimSuffix(strings.TrimSpace(m[1]), "%")
		resp.CPU.UsagePct = atofSafe(usageStr)
	}
	reMem := regexp.MustCompile(`(?i)Memory\s*[:：]\s*(?:<[^>]*>)?\s*([^<\n]+)`)
	if m := reMem.FindStringSubmatch(html); len(m) > 1 {
		resp.Memory = parseMemoryHTMLStr(strings.TrimSpace(m[1]))
	}
	reCores := regexp.MustCompile(`(?i)(?:Cores|CPU\(s\))\s*[:：]\s*(?:<[^>]*>)?\s*(\d+)`)
	if m := reCores.FindStringSubmatch(html); len(m) > 1 {
		resp.CPU.Cores = atoiSafe(m[1], 1)
	}
}

// parseTemperatureHTML extracts CPU temperature from Unraid's Temperature.php output.
func parseTemperatureHTML(html string, resp *dashboardResp) {
	reTemp := regexp.MustCompile("(\\d+)\\s*\u00b0C")
	if m := reTemp.FindStringSubmatch(html); len(m) > 1 {
		temp := atofSafe(m[1])
		resp.CPU.PerCoreTempC = []float64{temp}
	}
}

// parseMainPageJS extracts system info from JS variables embedded in the /Main page.
func parseMainPageJS(html string, resp *dashboardResp) {
	// Parse uptime: var uptime = 422410.33;
	if m := regexp.MustCompile(`var\s+uptime\s*=\s*([0-9.]+)`).FindStringSubmatch(html); len(m) > 1 {
		resp.Uptime = int64(atofSafe(m[1]))
	}

	// Array state from status text in HTML
	if strings.Contains(html, "\u9635\u5217\u5df2\u542f\u52a8") || strings.Contains(html, "Array Started") {
		// array started
	} else if strings.Contains(html, "\u9635\u5217\u5df2\u505c\u6b62") || strings.Contains(html, "Array Stopped") {
		// array stopped
	}

	// Memory from footer: "MemUsed: 8.3 GB / 31.2 GB"
	reMemFooter := regexp.MustCompile(`(?i)(?:MemUsed|Memory)\s*[:：]\s*(\d+\.?\d*\s*[KMGT]i?B?)\s*/\s*(\d+\.?\d*\s*[KMGT]i?B?)`)
	if m := reMemFooter.FindStringSubmatch(html); len(m) > 2 {
		resp.Memory = parseMemoryHTMLStr(strings.TrimSpace(m[1]) + " / " + strings.TrimSpace(m[2]))
	}
}

// parseUserProfileJSON extracts server metadata from the Unraid 7.x
// <unraid-user-profile server="..."> custom element attribute.
// This JSON contains name, osVersion, model, regType, etc.
func parseUserProfileJSON(html string) *serverMeta {
	// Match: <unraid-user-profile server="{&quot;name&quot;:&quot;Tower&quot;,...}">
	re := regexp.MustCompile(`<unraid-user-profile\s+server="([^"]*)"`)
	m := re.FindStringSubmatch(html)
	if len(m) < 2 {
		return nil
	}
	// HTML-decode the &quot; entities
	encoded := m[1]
	encoded = strings.ReplaceAll(encoded, "&quot;", `"`)
	encoded = strings.ReplaceAll(encoded, "&#39;", "'")
	encoded = strings.ReplaceAll(encoded, "&amp;", "&")

	// Parse the JSON
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(encoded), &raw); err != nil {
		return nil
	}
	meta := &serverMeta{}
	if v, ok := raw["name"].(string); ok {
		meta.Name = v
	}
	if v, ok := raw["osVersion"].(string); ok {
		meta.OSVersion = v
	}
	if v, ok := raw["description"].(string); ok {
		meta.Description = v
	}
	if v, ok := raw["model"].(string); ok {
		meta.Model = v
	}
	if v, ok := raw["regTy"].(string); ok {
		meta.RegType = v
	}
	if v, ok := raw["regTo"].(string); ok {
		meta.RegTo = v
	}
	if v, ok := raw["deviceCount"].(string); ok {
		meta.DeviceCount = v
	}
	if v, ok := raw["caseModel"].(string); ok {
		meta.CaseModel = v
	}
	return meta
}

// parseDashboardHTMLSysInfo extracts CPU model and memory from the Unraid
// Dashboard page HTML. The Dashboard may embed system info in gauge panels
// or info blocks that are not available on the /Main page.
func parseDashboardHTMLSysInfo(html string, resp *dashboardResp) {
	// Try to find CPU model from various patterns in the dashboard
	reCPU := regexp.MustCompile(`(?i)(?:CPU|Processor|model\s+name)\s*[:：]\s*(?:<[^>]*>)?\s*([^<\n]+)`)
	if m := reCPU.FindStringSubmatch(html); len(m) > 1 {
		resp.CPU.ModelName = strings.TrimSpace(m[1])
	}
	// Try to find memory from dashboard info
	reMem := regexp.MustCompile(`(?i)Memory\s*[:：]\s*(?:<[^>]*>)?\s*([^<\n]+)`)
	if m := reMem.FindStringSubmatch(html); len(m) > 1 {
		resp.Memory = parseMemoryHTMLStr(strings.TrimSpace(m[1]))
	}
}

// parseMemoryHTMLStr parses memory strings like "17.3 GB / 31.2 GB" or
// "17.3G / 31.2G" into a memInfo struct.
func parseMemoryHTMLStr(s string) memInfo {
	m := memInfo{}
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return m
	}
	used := parseSizeStr(strings.TrimSpace(parts[0]))
	total := parseSizeStr(strings.TrimSpace(parts[1]))
	m.TotalBytes = total
	m.UsedBytes = used
	if m.UsedBytes > m.TotalBytes {
		// The "used" might actually be "available"
		m.CacheBytes = m.TotalBytes - m.UsedBytes
		m.UsedBytes = m.TotalBytes - m.CacheBytes
	}
	if m.TotalBytes > 0 {
		m.UsagePct = float64(m.UsedBytes) / float64(m.TotalBytes) * 100
	}
	return m
}

// parseSizeStr converts human-readable sizes like "31.2 GB", "4 GiB",
// "512 MB" to bytes. Returns 0 on parse error.
func parseSizeStr(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var mult int64 = 1
	switch {
	case strings.HasSuffix(s, "TiB"), strings.HasSuffix(s, "TB"):
		if strings.HasSuffix(s, "TiB") {
			mult = 1 << 40
			s = strings.TrimSuffix(s, "TiB")
		} else {
			mult = 1e12
			s = strings.TrimSuffix(s, "TB")
		}
	case strings.HasSuffix(s, "GiB"), strings.HasSuffix(s, "GB"), strings.HasSuffix(s, "G"):
		if strings.HasSuffix(s, "GiB") {
			mult = 1 << 30
			s = strings.TrimSuffix(s, "GiB")
		} else if strings.HasSuffix(s, "GB") {
			mult = 1e9
			s = strings.TrimSuffix(s, "GB")
		} else {
			mult = 1e9
			s = strings.TrimSuffix(s, "G")
		}
	case strings.HasSuffix(s, "MiB"), strings.HasSuffix(s, "MB"), strings.HasSuffix(s, "M"):
		if strings.HasSuffix(s, "MiB") {
			mult = 1 << 20
			s = strings.TrimSuffix(s, "MiB")
		} else if strings.HasSuffix(s, "MB") {
			mult = 1e6
			s = strings.TrimSuffix(s, "MB")
		} else {
			mult = 1e6
			s = strings.TrimSuffix(s, "M")
		}
	case strings.HasSuffix(s, "KiB"), strings.HasSuffix(s, "KB"), strings.HasSuffix(s, "K"):
		if strings.HasSuffix(s, "KiB") {
			mult = 1 << 10
			s = strings.TrimSuffix(s, "KiB")
		} else if strings.HasSuffix(s, "KB") {
			mult = 1e3
			s = strings.TrimSuffix(s, "KB")
		} else {
			mult = 1e3
			s = strings.TrimSuffix(s, "K")
		}
	}
	return int64(atofSafe(strings.TrimSpace(s)) * float64(mult))
}

// parseUptimeHTMLStr parses uptime strings like "4 days, 16 hours, 20 minutes"
// or "112h 20m" into seconds.
func parseUptimeHTMLStr(s string) int64 {
	var total int64
	// "X days, Y hours, Z minutes" pattern
	days := regexp.MustCompile(`(\d+)\s*day`).FindStringSubmatch(s)
	hours := regexp.MustCompile(`(\d+)\s*hour`).FindStringSubmatch(s)
	mins := regexp.MustCompile(`(\d+)\s*min`).FindStringSubmatch(s)
	if len(days) > 1 {
		total += int64(atoiSafe(days[1], 0)) * 86400
	}
	if len(hours) > 1 {
		total += int64(atoiSafe(hours[1], 0)) * 3600
	}
	if len(mins) > 1 {
		total += int64(atoiSafe(mins[1], 0)) * 60
	}
	// Also try "112h 20m" compact format
	if total == 0 {
		hmRe := regexp.MustCompile(`(\d+)h\s*(\d+)m`)
		if m := hmRe.FindStringSubmatch(s); len(m) > 2 {
			total = int64(atoiSafe(m[1], 0))*3600 + int64(atoiSafe(m[2], 0))*60
		}
	}
	return total
}

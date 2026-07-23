package handler

import (
	"strings"

	"github.com/crazyqin/unraid-plus/server/internal/ssh"
)

// This file implements the "Unraid API channel" by reading Unraid's own
// structured state files via SSH. These INI files at /usr/local/emhttp/state/
// are the same data source that Unraid's PHP WebUI reads from, giving us
// reliable structured data without parsing shell command output.
//
// Key advantage over the old approach (df + mdcmd + smartctl):
//   - disks.ini has all disk info in structured format (device, fsType, mount,
//     size, used, free, temp, errors, status) — no AWK/df parsing needed
//   - var.ini has array state (mdState=STARTED) — no guessing mdcmd format
//   - monitor.ini has temps and SMART data
//   - All maintained by Unraid's own daemons, format is stable across versions

// unraidState holds parsed data from all Unraid state files.
type unraidState struct {
	Var     varState
	Disks   []diskState
	Monitor monitorState
}

// varState holds selected fields from /usr/local/emhttp/state/var.ini.
type varState struct {
	Version  string // e.g. "7.3.2"
	Name     string // server name, e.g. "Tower"
	MdState  string // "STARTED", "STOPPED"
	FsState  string // "Started", "Stopped"
	TimeZone string
}

// diskState holds parsed fields from one disk section in disks.ini.
// Field names mirror the INI keys for easy cross-referencing with Unraid docs.
type diskState struct {
	Name         string // "disk1", "parity", "cache1", "flash"
	Device       string // "sdb", "nvme0n1" (physical device)
	ID           string // model_serial, e.g. "SanDisk_SDSSDH3_500G_..."
	Type         string // "Data", "Parity", "Cache", "Flash"
	Status       string // "DISK_OK", "DISK_NP_DSBL", etc.
	FsType       string // "xfs", "btrfs", "vfat"
	FsStatus     string // "Mounted", "Not mounted"
	FsMountpoint string // "/mnt/disk1"
	Temp         string // "47" or "*"
	NumReads     string
	NumWrites    string
	NumErrors    string
	FsSize       string // in 1K blocks
	FsFree       string // in 1K blocks
	FsUsed       string // in 1K blocks
	Rotational   string // "0" = SSD, "1" = HDD
	Transport    string // "ata", "nvme", "usb"
	Color        string // "green-on", "yellow-on", "red-on", "grey-off"
	DeviceSb     string // md device, e.g. "md1p1"
}

// monitorState holds parsed data from /usr/local/emhttp/state/monitor.ini.
type monitorState struct {
	Temps map[string]string // dev2="68" (key = device name without /dev/)
	Smart map[string]string // disk2.199="305" (key = diskName.attributeId)
}

// readStateFiles reads all Unraid state files in a single SSH batch.
// This is 3 SSH commands instead of the old approach's 5+ commands (df,
// mdcmd status, smartctl per disk, thermal, diskstats), and the data is
// far more structured and reliable.
func readStateFiles(cli *ssh.Client) (*unraidState, error) {
	// Use echo with a safe delimiter (no shell special chars) to separate
	// the output of each file. The delimiter must not appear in INI files.
	const delim = "XX_STATE_SPLIT_XX"

	cmd := "cat /usr/local/emhttp/state/var.ini 2>/dev/null" +
		`; echo "` + delim + `"` +
		`; cat /usr/local/emhttp/state/disks.ini 2>/dev/null` +
		`; echo "` + delim + `"` +
		`; cat /usr/local/emhttp/state/monitor.ini 2>/dev/null`

	out, err := cli.Run(cmd)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}

	parts := strings.Split(out, delim)
	state := &unraidState{}

	if len(parts) > 0 {
		state.Var = parseVarIni(parts[0])
	}
	if len(parts) > 1 {
		state.Disks = parseDisksIni(parts[1])
	}
	if len(parts) > 2 {
		state.Monitor = parseMonitorIni(parts[2])
	}

	return state, nil
}

// parseVarIni extracts selected fields from var.ini content.
func parseVarIni(s string) varState {
	v := varState{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		switch key {
		case "version":
			v.Version = val
		case "NAME":
			v.Name = val
		case "mdState":
			v.MdState = val
		case "fsState":
			v.FsState = val
		case "timeZone":
			v.TimeZone = val
		}
	}
	return v
}

// parseDisksIni parses Unraid's disks.ini which uses a section-based INI
// format with quoted values:
//
//	["disk1"]
//	idx="1"
//	name="disk1"
//	device="sdb"
//	...
//	["disk2"]
//	...
//
// We extract only the fields we need for the Storage and Dashboard APIs.
func parseDisksIni(s string) []diskState {
	var disks []diskState
	var current *diskState

	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}

		// Section header: ["disk1"]
		if strings.HasPrefix(line, "[") {
			if current != nil {
				disks = append(disks, *current)
			}
			sectionName := strings.Trim(line, "[]\"")
			current = &diskState{Name: sectionName}
			continue
		}

		if current == nil {
			continue
		}

		// Key="value" pair
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		switch key {
		case "device":
			current.Device = val
		case "id":
			current.ID = val
		case "type":
			current.Type = val
		case "status":
			current.Status = val
		case "fsType":
			current.FsType = val
		case "fsStatus":
			current.FsStatus = val
		case "fsMountpoint":
			current.FsMountpoint = val
		case "temp":
			current.Temp = val
		case "numReads":
			current.NumReads = val
		case "numWrites":
			current.NumWrites = val
		case "numErrors":
			current.NumErrors = val
		case "fsSize":
			current.FsSize = val
		case "fsFree":
			current.FsFree = val
		case "fsUsed":
			current.FsUsed = val
		case "rotational":
			current.Rotational = val
		case "transport":
			current.Transport = val
		case "color":
			current.Color = val
		case "deviceSb":
			current.DeviceSb = val
		}
	}
	if current != nil {
		disks = append(disks, *current)
	}
	return disks
}

// parseMonitorIni parses monitor.ini which has sections like:
//
//	[temp]
//	dev2="68"
//	[smart]
//	disk2.199="305"
func parseMonitorIni(s string) monitorState {
	m := monitorState{
		Temps: map[string]string{},
		Smart: map[string]string{},
	}
	section := ""
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[]")
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		switch section {
		case "temp":
			m.Temps[key] = val
		case "smart":
			m.Smart[key] = val
		}
	}
	return m
}

// stateArrayDisks filters state disks to only array data disks (type="Data").
func stateArrayDisks(disks []diskState) []diskState {
	var out []diskState
	for _, d := range disks {
		if d.Type == "Data" && d.FsMountpoint != "" {
			out = append(out, d)
		}
	}
	return out
}

// stateCacheDisks filters state disks to only cache disks (type="Cache").
func stateCacheDisks(disks []diskState) []diskState {
	var out []diskState
	for _, d := range disks {
		if d.Type == "Cache" && d.FsMountpoint != "" {
			out = append(out, d)
		}
	}
	return out
}

// stateToDisk converts a diskState (from INI) to the API disk struct.
// Sizes in disks.ini are in 1K blocks; convert to bytes.
//
// Device path: array disks use deviceSb (md device like "md1p1") since
// that's what's actually mounted. Cache/flash disks have no md layer,
// so deviceSb is empty — fall back to the physical device name.
//
// Temperature: disks.ini temp field provides a baseline; enrichWithSmart
// will override it with smartctl's more precise reading when available.
// When smartctl is absent, the INI temp remains as the only source.
func stateToDisk(ds diskState) disk {
	used := atoi64Safe(ds.FsUsed) * 1024
	size := atoi64Safe(ds.FsSize) * 1024
	devName := ds.DeviceSb
	if devName == "" {
		devName = ds.Device
	}
	d := disk{
		Device:    "/dev/" + devName,
		Name:      ds.FsMountpoint,
		FsType:    ds.FsType,
		SizeBytes: size,
		UsedBytes: used,
		Errors:    atoiSafe(ds.NumErrors, 0),
		Status:    diskStatus(used, size),
		// Unraid-specific enrichment from state files
		DiskName:   ds.Name,
		Color:      ds.Color,
		Rotational: ds.Rotational,
		Transport:  ds.Transport,
	}

	// Physical device from disks.ini — used for /proc/diskstats RW rate
	// lookup. Array disks (md devices) often don't appear in diskstats,
	// but their underlying physical device (sd*, nvme*) always does.
	if ds.Device != "" {
		d.physicalDev = ds.Device // e.g. "sdb", "nvme0n1"
	}

	// Temperature: disks.ini temp field is in Celsius, "*" means N/A
	if ds.Temp != "" && ds.Temp != "*" {
		if t := atoiSafe(ds.Temp, 0); t > 0 {
			d.TempC = &t
		}
	}

	return d
}

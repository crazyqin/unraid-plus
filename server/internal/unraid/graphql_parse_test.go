package unraid

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFlexInt64_NumberAndString(t *testing.T) {
	var n FlexInt64
	if err := json.Unmarshal([]byte(`34359738368`), &n); err != nil {
		t.Fatalf("number: %v", err)
	}
	if n.Int64() != 34359738368 {
		t.Fatalf("got %d", n.Int64())
	}
	if err := json.Unmarshal([]byte(`"17179869184"`), &n); err != nil {
		t.Fatalf("string: %v", err)
	}
	if n.Int64() != 17179869184 {
		t.Fatalf("got %d", n.Int64())
	}
}

func TestFlexUptime_ISOAndSeconds(t *testing.T) {
	boot := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	raw, _ := json.Marshal(boot)
	var u FlexUptime
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatalf("iso: %v", err)
	}
	// Allow 5s skew
	if u.Seconds < 2*3600-5 || u.Seconds > 2*3600+5 {
		t.Fatalf("expected ~7200s, got %d (boot=%s)", u.Seconds, u.BootTime)
	}

	u = FlexUptime{}
	if err := json.Unmarshal([]byte(`86400.5`), &u); err != nil {
		t.Fatalf("seconds: %v", err)
	}
	if u.Seconds != 86400 {
		t.Fatalf("got %d", u.Seconds)
	}
}

func TestParseInfoQuery_OfficialSchema(t *testing.T) {
	// Mirrors official Unraid API: uptime is ISO string, clockSpeed is int,
	// cache (if present) is object — we no longer request cache in the query.
	raw := []byte(`{
		"info": {
			"os": {
				"hostname": "Tower",
				"uptime": "2026-07-20T08:30:00.000Z",
				"platform": "linux"
			},
			"cpu": {
				"manufacturer": "Intel",
				"brand": "Intel(R) Core(TM) i7-8700 CPU @ 3.20GHz",
				"cores": 6,
				"threads": 12
			},
			"memory": {
				"layout": [
					{"bank": "BANK 0", "type": "DDR4", "clockSpeed": 2666, "manufacturer": "Samsung"}
				]
			},
			"versions": {"core": {"unraid": "7.2.0"}},
			"system": {"model": "Custom"}
		}
	}`)

	info, err := ParseInfoQuery(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.OS == nil || info.OS.Hostname != "Tower" {
		t.Fatalf("hostname: %+v", info.OS)
	}
	if info.OS.Uptime.Seconds <= 0 {
		t.Fatalf("uptime seconds should be > 0, got %d", info.OS.Uptime.Seconds)
	}
	if info.CPU == nil || info.CPU.Brand == "" || info.CPU.Threads != 12 {
		t.Fatalf("cpu: %+v", info.CPU)
	}
	if len(info.Memory.Layout) != 1 || info.Memory.Layout[0].ClockSpeed.Float64() != 2666 {
		t.Fatalf("memory layout: %+v", info.Memory)
	}
}

func TestParseInfoQuery_OldBrokenTypesWouldFail(t *testing.T) {
	// Document the regression: if uptime were float64, this would fail.
	raw := []byte(`{"info":{"os":{"hostname":"X","uptime":"2026-01-01T00:00:00Z"},"cpu":{"brand":"Y","cores":4}}}`)
	info, err := ParseInfoQuery(raw)
	if err != nil {
		t.Fatalf("must parse official uptime string: %v", err)
	}
	if info.CPU.Brand != "Y" {
		t.Fatalf("lost cpu due to uptime parse: %+v", info.CPU)
	}
}

func TestParseMetricsQuery_BigIntAndPerCore(t *testing.T) {
	raw := []byte(`{
		"metrics": {
			"cpu": {
				"percentTotal": 17.5,
				"cpus": [
					{"percentTotal": 10.0},
					{"percentTotal": 25.0}
				]
			},
			"memory": {
				"total": "34359738368",
				"used": "8589934592",
				"free": "1000",
				"available": "20000000000",
				"buffcache": "5000000000",
				"percentTotal": 25.0
			},
			"network": [
				{
					"name": "eth0",
					"operstate": "up",
					"bytesReceived": "1000000",
					"bytesSent": 2000000,
					"rxSec": 1024.5,
					"txSec": 512.25
				},
				{"name": "lo", "rxSec": 0, "txSec": 0}
			]
		}
	}`)

	m, err := ParseMetricsQuery(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.CPU == nil || m.CPU.PercentTotal != 17.5 {
		t.Fatalf("cpu: %+v", m.CPU)
	}
	if len(m.CPU.Cpus) != 2 || m.CPU.Cpus[1].PercentTotal != 25.0 {
		t.Fatalf("per-core: %+v", m.CPU.Cpus)
	}
	if m.Memory.Total.Int64() != 34359738368 {
		t.Fatalf("mem total: %d", m.Memory.Total.Int64())
	}
	if m.Memory.Used.Int64() != 8589934592 {
		t.Fatalf("mem used: %d", m.Memory.Used.Int64())
	}
	if len(m.Network) != 2 || m.Network[0].RxSec != 1024.5 {
		t.Fatalf("network: %+v", m.Network)
	}
	// Numeric BigInt (no quotes) also works
	raw2 := []byte(`{"metrics":{"memory":{"total":8589934592,"used":100,"percentTotal":1.1}}}`)
	m2, err := ParseMetricsQuery(raw2)
	if err != nil {
		t.Fatalf("parse number bigint: %v", err)
	}
	if m2.Memory.Total.Int64() != 8589934592 {
		t.Fatalf("got %d", m2.Memory.Total.Int64())
	}
}

func TestParseMetricsQuery_NumberOnlyCPU(t *testing.T) {
	raw := []byte(`{"metrics":{"cpu":{"percentTotal":3.14},"memory":{"total":1024,"used":512,"buffcache":128,"percentTotal":50}}}`)
	m, err := ParseMetricsQuery(raw)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if m.CPU.PercentTotal != 3.14 || m.Memory.PercentTotal != 50 {
		t.Fatalf("%+v", m)
	}
}

func TestParseArrayQuery_OfficialNumericDiskFields(t *testing.T) {
	// Official schema: size/fsUsed are GraphQLBigInt, rotational is Boolean,
	// capacity free/used/total are String.
	raw := []byte(`{
		"array": {
			"state": "STARTED",
			"capacity": {
				"kilobytes": {"free": "1000", "used": "2000", "total": "3000"},
				"disks": {"free": "1", "used": "2", "total": "3"}
			},
			"disks": [
				{
					"id": "disk1",
					"idx": 1,
					"name": "disk1",
					"device": "sdb",
					"size": 3907018584,
					"status": "DISK_OK",
					"rotational": true,
					"temp": 35,
					"numReads": 100,
					"numWrites": 200,
					"numErrors": 0,
					"fsSize": 3900000000,
					"fsFree": 1000000000,
					"fsUsed": 2900000000,
					"exportable": false,
					"type": "DATA",
					"warning": 70,
					"critical": 90,
					"fsType": "xfs",
					"color": "green-on"
				}
			],
			"caches": [],
			"parities": []
		}
	}`)
	arr, err := ParseArrayQuery(raw)
	if err != nil {
		t.Fatalf("parse array: %v", err)
	}
	if arr.State != "STARTED" {
		t.Fatalf("state %s", arr.State)
	}
	if arr.Capacity == nil || arr.Capacity.Kilobytes.Total.String() != "3000" {
		t.Fatalf("capacity: %+v", arr.Capacity)
	}
	if len(arr.Disks) != 1 {
		t.Fatalf("disks: %d", len(arr.Disks))
	}
	d := arr.Disks[0]
	if d.Size.String() != "3907018584" {
		t.Fatalf("size: %q", d.Size)
	}
	if !d.Rotational.Bool() {
		t.Fatalf("rotational should be true")
	}
	if d.Temp.String() != "35" {
		t.Fatalf("temp: %q", d.Temp)
	}
	if d.FsUsed.String() != "2900000000" {
		t.Fatalf("fsUsed: %q", d.FsUsed)
	}
}

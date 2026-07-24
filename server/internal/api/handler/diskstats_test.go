package handler

import (
	"testing"
)

func TestIsWholeDiskName(t *testing.T) {
	yes := []string{"sda", "sdb", "nvme0n1", "nvme1n1", "md1", "md126", "vda", "mmcblk0"}
	no := []string{"sda1", "sdb2", "nvme0n1p1", "nvme0n1p2", "md1p1", "loop0", "ram0", "mmcblk0p1", ""}
	for _, d := range yes {
		if !isWholeDiskName(d) {
			t.Errorf("%s should be whole disk", d)
		}
	}
	for _, d := range no {
		if isWholeDiskName(d) {
			t.Errorf("%s should NOT be whole disk", d)
		}
	}
}

func TestParseDiskstats_SkipsPartitions(t *testing.T) {
	// major minor name rio rmerge rsect ... wio wmerge wsect ...
	raw := "" +
		"   8       0 sda 100 0 1000 0 50 0 500 0 0 0 0 0 0\n" +
		"   8       1 sda1 100 0 1000 0 50 0 500 0 0 0 0 0 0\n" +
		" 259       0 nvme0n1 200 0 2000 0 10 0 100 0 0 0 0 0 0\n" +
		" 259       1 nvme0n1p1 200 0 2000 0 10 0 100 0 0 0 0 0 0\n" +
		"   9       1 md1 10 0 100 0 5 0 50 0 0 0 0 0 0\n" +
		"   9       2 md1p1 10 0 100 0 5 0 50 0 0 0 0 0 0\n"
	m := parseDiskstats(raw)
	if _, ok := m["sda"]; !ok {
		t.Fatal("want sda")
	}
	if _, ok := m["sda1"]; ok {
		t.Fatal("must skip sda1")
	}
	if _, ok := m["nvme0n1"]; !ok {
		t.Fatal("want nvme0n1")
	}
	if _, ok := m["nvme0n1p1"]; ok {
		t.Fatal("must skip nvme0n1p1")
	}
	if _, ok := m["md1"]; !ok {
		t.Fatal("want md1")
	}
	if _, ok := m["md1p1"]; ok {
		t.Fatal("must skip md1p1")
	}
}

func TestDiskRWFromMaps(t *testing.T) {
	a := map[string]diskStat{"sda": {sectorsRead: 100, sectorsWritten: 50}}
	b := map[string]diskStat{"sda": {sectorsRead: 100 + 2000, sectorsWritten: 50 + 1000}}
	// 2000 sectors * 512 / 1s = 1_024_000 B/s
	rw := diskRWFromMaps(a, b, 1.0)
	if rw.Read != 2000*512 {
		t.Fatalf("read=%d", rw.Read)
	}
	if rw.Write != 1000*512 {
		t.Fatalf("write=%d", rw.Write)
	}
}

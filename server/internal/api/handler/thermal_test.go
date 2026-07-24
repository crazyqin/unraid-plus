package handler

import (
	"reflect"
	"testing"
)

func TestMapTempsToLogicalCores_TopologyHT(t *testing.T) {
	// i5-8279U style: 4 physical cores, 8 logical (HT second half)
	// L 0→0,1→1,2→2,3→3,4→0,5→1,6→2,7→3
	// T 0=40, 1=50, 2=60, 3=70
	snap := thermalSnapshot{
		logicalToCore: map[int]int{0: 0, 1: 1, 2: 2, 3: 3, 4: 0, 5: 1, 6: 2, 7: 3},
		coreTemp:      map[int]float64{0: 40, 1: 50, 2: 60, 3: 70},
	}
	got := mapTempsToLogicalCores(snap, 8)
	want := []float64{40, 50, 60, 70, 40, 50, 60, 70}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestExpandCoreTempsHT_NotAdjacentPairs(t *testing.T) {
	// Old bug: [t0,t0,t1,t1,...] — correct HT second-half: [t0,t1,t2,t3,t0,t1,t2,t3]
	phy := []float64{40, 50, 60, 70}
	got := expandCoreTempsHT(phy, 8)
	want := []float64{40, 50, 60, 70, 40, 50, 60, 70}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseThermalSnapshot_Labeled(t *testing.T) {
	raw := "" +
		"L 0 0\nL 1 1\nL 2 0\nL 3 1\n" +
		"T 0 45000\nT 1 52000\n" +
		"P 48000\n"
	snap := parseThermalSnapshot(raw)
	if snap.coreTemp[0] != 45 || snap.coreTemp[1] != 52 {
		t.Fatalf("coreTemp: %+v", snap.coreTemp)
	}
	if snap.packageTemp != 48 {
		t.Fatalf("package: %v", snap.packageTemp)
	}
	got := mapTempsToLogicalCores(snap, 4)
	want := []float64{45, 52, 45, 52}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestMapTempsToLogicalCores_PackageOnly(t *testing.T) {
	snap := thermalSnapshot{packageTemp: 55}
	got := mapTempsToLogicalCores(snap, 4)
	want := []float64{55, 55, 55, 55}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

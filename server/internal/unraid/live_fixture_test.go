package unraid

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadData(t *testing.T, name string) json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("/tmp/unraid-fixtures", name+".json"))
	if err != nil {
		t.Skip("no fixture:", err)
	}
	var wrap struct {
		Data   json.RawMessage `json:"data"`
		Errors []GraphQLError  `json:"errors"`
	}
	if err := json.Unmarshal(b, &wrap); err != nil {
		t.Fatal(err)
	}
	if len(wrap.Errors) > 0 {
		t.Fatalf("errors: %+v", wrap.Errors)
	}
	return wrap.Data
}

func TestLiveFixtureArray(t *testing.T) {
	data := loadData(t, "array")
	arr, err := ParseArrayQuery(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("state=%s disks=%d", arr.State, len(arr.Disks))
	if len(arr.Disks) == 0 {
		t.Fatal("no disks")
	}
	d := arr.Disks[0]
	t.Logf("disk0 name=%s size=%q temp=%q rot=%v fsUsed=%q status=%s", d.Name, d.Size, d.Temp, d.Rotational, d.FsUsed, d.Status)
}

func TestLiveFixtureDocker(t *testing.T) {
	data := loadData(t, "docker")
	d, err := ParseDockerQuery(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("containers=%d", len(d.Containers))
	for i, c := range d.Containers {
		t.Logf("[%d] names=%v state=%s status=%s ports=%+v", i, c.Names, c.State, c.Status, c.Ports)
		if i > 2 { break }
	}
}

func TestLiveFixtureVMs(t *testing.T) {
	data := loadData(t, "vms")
	vmsArr, err := ParseVMsQuery(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(vmsArr) == 0 || len(vmsArr[0].Domains) == 0 {
		t.Fatalf("expected domains, got %+v", vmsArr)
	}
	t.Logf("domains=%d first=%+v", len(vmsArr[0].Domains), vmsArr[0].Domains[0])
}

func TestParseVMsQuery_ObjectAndArray(t *testing.T) {
	obj := []byte(`{"vms":{"id":"x:vms","domains":[{"id":"x:1","name":"A","state":"RUNNING","uuid":"1"}]}}`)
	got, err := ParseVMsQuery(obj)
	if err != nil || len(got) != 1 || len(got[0].Domains) != 1 || got[0].Domains[0].Name != "A" {
		t.Fatalf("object: err=%v got=%+v", err, got)
	}
	arr := []byte(`{"vms":[{"id":"g1","domains":[{"id":"1","name":"B","state":"shut off","uuid":"2"}]}]}`)
	got, err = ParseVMsQuery(arr)
	if err != nil || len(got) != 1 || got[0].Domains[0].Name != "B" {
		t.Fatalf("array: err=%v got=%+v", err, got)
	}
}

func TestStripPrefixedID(t *testing.T) {
	long := "2c4c1972107985ee3b6f9d068129ef585806100ec9f4f6d40fd71c5df689b6b5:601778a4a58dcb23"
	if StripPrefixedID(long) != "601778a4a58dcb23" {
		t.Fatal(StripPrefixedID(long))
	}
	if StripPrefixedID("plain-name") != "plain-name" {
		t.Fatal("plain")
	}
}

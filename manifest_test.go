package analyzer

import (
	"reflect"
	"strings"
	"testing"
)

const v1JSON = `{
  "chunks": [
    {"id": "a", "file": "dist/a.js", "size": 100, "entries": ["src/a.js"]},
    {"id": "b", "file": "dist/b.js", "size": 80, "entries": ["src/b.js"]}
  ],
  "modules": [
    {"id": "src/a.js", "size": 40, "deps": ["src/shared.js"]},
    {"id": "src/b.js", "size": 30, "deps": ["src/shared.js"]},
    {"id": "src/shared.js", "size": 50}
  ]
}`

// v2：显式 version、新增 kind/dir 字段，以及未来的未知字段。
const v2JSON = `{
  "version": 2,
  "futureTopLevel": {"anything": true},
  "chunks": [
    {"id": "a", "file": "dist/a.js", "size": 100, "kind": "js", "entries": ["src/a.js"], "hash": "x"},
    {"id": "b", "file": "dist/b.js", "size": 80, "kind": "js", "entries": ["src/b.js"], "hash": "y"}
  ],
  "modules": [
    {"id": "src/a.js", "size": 40, "deps": ["src/shared.js"], "dir": "src", "sideEffects": false},
    {"id": "src/b.js", "size": 30, "deps": ["src/shared.js"], "dir": "src"},
    {"id": "src/shared.js", "size": 50, "dir": "src"}
  ]
}`

func TestParseV1Defaults(t *testing.T) {
	m, err := ParseManifest(strings.NewReader(v1JSON))
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != 1 {
		t.Fatalf("version = %d, want 1", m.Version)
	}
	if len(m.Chunks) != 2 || len(m.Modules) != 3 {
		t.Fatalf("chunks=%d modules=%d", len(m.Chunks), len(m.Modules))
	}
}

func TestNewFieldsDoNotChangeAttribution(t *testing.T) {
	m1, err := ParseManifest(strings.NewReader(v1JSON))
	if err != nil {
		t.Fatal(err)
	}
	m2, err := ParseManifest(strings.NewReader(v2JSON))
	if err != nil {
		t.Fatal(err)
	}
	a1 := ComputeAttribution(m1)
	a2 := ComputeAttribution(m2)
	if !reflect.DeepEqual(a1.Chunks, a2.Chunks) {
		t.Fatalf("attribution changed by new fields:\nv1 %+v\nv2 %+v", a1.Chunks, a2.Chunks)
	}
	if a1.TotalOccupied != a2.TotalOccupied || a1.TotalResidual != a2.TotalResidual {
		t.Fatalf("totals differ: %+v vs %+v", a1, a2)
	}
	// v2 的 kind 应被解析用于预算等场景。
	if m2.Chunks[0].Kind != "js" {
		t.Fatalf("kind not parsed: %+v", m2.Chunks[0])
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	if _, err := ParseManifest(strings.NewReader(`{not json`)); err == nil {
		t.Fatal("want error for malformed json")
	}
	if _, err := ParseManifest(strings.NewReader(`{"version": 99}`)); err == nil {
		t.Fatal("want error for unsupported version")
	}
	if _, err := ParseManifest(strings.NewReader(`{"chunks": [{"id": ""}]}`)); err == nil {
		t.Fatal("want error for empty chunk id")
	}
}

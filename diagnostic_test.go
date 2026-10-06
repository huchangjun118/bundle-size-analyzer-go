package analyzer

import (
	"reflect"
	"testing"
)

func diagKinds(ds Diagnostics) map[DiagKind]int {
	out := map[DiagKind]int{}
	for _, d := range ds {
		out[d.Kind]++
	}
	return out
}

func TestMissingModuleLocated(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Chunks:  []Chunk{{ID: "a", File: "a.js", Size: 10, Entries: []string{"m1"}}},
		Modules: []Module{
			{ID: "m1", Size: 10, Deps: []string{"ghost"}},
		},
	}
	a := ComputeAttribution(m)
	kinds := diagKinds(a.Diagnostics)
	if kinds[DiagMissingModule] != 1 {
		t.Fatalf("diags = %+v", a.Diagnostics)
	}
	// 缺失模块不计入体积，但已有模块的归因保持正确。
	if a.Chunks[0].Bytes != 10 {
		t.Fatalf("bytes = %d, want 10", a.Chunks[0].Bytes)
	}
}

func TestMissingEntryLocated(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Chunks:  []Chunk{{ID: "a", File: "a.js", Size: 10, Entries: []string{"nope"}}},
	}
	a := ComputeAttribution(m)
	kinds := diagKinds(a.Diagnostics)
	if kinds[DiagMissingEntry] != 1 {
		t.Fatalf("diags = %+v", a.Diagnostics)
	}
	var found bool
	for _, d := range a.Diagnostics {
		if d.Kind == DiagMissingEntry && d.Chunk == "a" && d.Module == "nope" {
			found = true
		}
	}
	if !found {
		t.Fatal("diagnostic should locate chunk and module")
	}
}

func TestCycleDetectedAndAttributed(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Chunks:  []Chunk{{ID: "a", File: "a.js", Size: 30, Entries: []string{"m1"}}},
		Modules: []Module{
			{ID: "m1", Size: 10, Deps: []string{"m2"}},
			{ID: "m2", Size: 10, Deps: []string{"m3"}},
			{ID: "m3", Size: 10, Deps: []string{"m1"}}, // 成环
		},
	}
	a := ComputeAttribution(m)
	kinds := diagKinds(a.Diagnostics)
	if kinds[DiagCycle] != 1 {
		t.Fatalf("diags = %+v", a.Diagnostics)
	}
	for _, d := range a.Diagnostics {
		if d.Kind == DiagCycle && len(d.Path) < 4 {
			t.Fatalf("cycle path should be recorded, got %v", d.Path)
		}
	}
	// 环上模块仍然只归因一次，总量正确。
	if a.Chunks[0].Bytes != 30 {
		t.Fatalf("bytes = %d, want 30", a.Chunks[0].Bytes)
	}
}

func TestDuplicateIDsDiagnosed(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Chunks: []Chunk{
			{ID: "a", File: "a.js", Size: 10, Entries: []string{"m1"}},
			{ID: "a", File: "a2.js", Size: 99, Entries: []string{"m1"}},
		},
		Modules: []Module{
			{ID: "m1", Size: 5},
			{ID: "m1", Size: 50},
		},
	}
	a := ComputeAttribution(m)
	kinds := diagKinds(a.Diagnostics)
	if kinds[DiagDuplicateChunk] != 1 || kinds[DiagDuplicateMod] != 1 {
		t.Fatalf("diags = %+v", a.Diagnostics)
	}
	// 先出现者为准，重复定义不重复计费。
	if a.TotalOccupied != 10 || a.Chunks[0].Bytes != 5 {
		t.Fatalf("occupied=%d bytes=%d", a.TotalOccupied, a.Chunks[0].Bytes)
	}
}

func TestDiagnosticsDeterministic(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Chunks:  []Chunk{{ID: "a", File: "a.js", Size: 10, Entries: []string{"m1", "ghost-entry"}}},
		Modules: []Module{
			{ID: "m1", Size: 1, Deps: []string{"g1", "g2"}},
			{ID: "m2", Size: 1, Deps: []string{"m3"}},
			{ID: "m3", Size: 1, Deps: []string{"m2"}},
		},
	}
	want := ComputeAttribution(m).Diagnostics
	for i := 0; i < 20; i++ {
		got := ComputeAttribution(m).Diagnostics
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("run %d: diagnostics order changed", i)
		}
	}
}

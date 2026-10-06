package analyzer

import (
	"reflect"
	"testing"
)

// 共享模块 shared 被 a、b 两个资源块同时引用，只能计入一个属主。
func sharedManifest() *Manifest {
	return &Manifest{
		Version: 1,
		Chunks: []Chunk{
			{ID: "a", File: "dist/a.js", Size: 100, Entries: []string{"src/a.js"}},
			{ID: "b", File: "dist/b.js", Size: 80, Entries: []string{"src/b.js"}},
		},
		Modules: []Module{
			{ID: "src/a.js", Size: 40, Deps: []string{"src/shared.js"}},
			{ID: "src/b.js", Size: 30, Deps: []string{"src/shared.js"}},
			{ID: "src/shared.js", Size: 50},
		},
	}
}

func TestSharedModuleCountedOnce(t *testing.T) {
	a := ComputeAttribution(sharedManifest())

	var totalAttributed int64
	owners := map[string]int{}
	for _, c := range a.Chunks {
		totalAttributed += c.Bytes
		for _, m := range c.ModuleIDs {
			owners[m]++
		}
	}
	if totalAttributed != 120 {
		t.Fatalf("total attributed = %d, want 120", totalAttributed)
	}
	for m, n := range owners {
		if n != 1 {
			t.Fatalf("module %s attributed %d times, want exactly 1", m, n)
		}
	}
	// 共享模块归属字典序最小的资源块 a。
	if got := a.Chunks[0]; got.ChunkID != "a" || got.Bytes != 90 {
		t.Fatalf("chunk a = %+v, want bytes 90", got)
	}
	if got := a.Chunks[1]; got.ChunkID != "b" || got.Bytes != 30 {
		t.Fatalf("chunk b = %+v, want bytes 30", got)
	}
}

func TestResidualAlignsWithOccupied(t *testing.T) {
	a := ComputeAttribution(sharedManifest())
	var sum int64
	for _, c := range a.Chunks {
		sum += c.Bytes + c.Residual
	}
	if sum != a.TotalOccupied {
		t.Fatalf("bytes+residual = %d, TotalOccupied = %d", sum, a.TotalOccupied)
	}
	// chunk a: 100 占用 - 90 归因 = 10 残差。
	if a.Chunks[0].Residual != 10 {
		t.Fatalf("chunk a residual = %d, want 10", a.Chunks[0].Residual)
	}
}

func TestAttributionDeterministic(t *testing.T) {
	want := ComputeAttribution(sharedManifest())
	for i := 0; i < 20; i++ {
		got := ComputeAttribution(sharedManifest())
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("run %d differs:\nwant %+v\ngot  %+v", i, want, got)
		}
	}
}

func TestByDirectoryAndSubtree(t *testing.T) {
	m := sharedManifest()
	a := ComputeAttribution(m)

	dirs := a.ByDirectory(m)
	if len(dirs) != 1 || dirs[0].Dir != "src" || dirs[0].Bytes != 120 {
		t.Fatalf("ByDirectory = %+v", dirs)
	}

	total, mods, diags := a.Subtree(m, "src/a.js")
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %+v", diags)
	}
	if total != 90 || !reflect.DeepEqual(mods, []string{"src/a.js", "src/shared.js"}) {
		t.Fatalf("subtree = %d %v", total, mods)
	}

	// 子树根不存在：给出诊断而非静默零值。
	_, _, diags = a.Subtree(m, "nope.js")
	if len(diags) == 0 || diags[len(diags)-1].Kind != DiagMissingModule {
		t.Fatalf("want missing module diagnostic, got %+v", diags)
	}
}

func TestUnreachableModuleIsUnattributed(t *testing.T) {
	m := sharedManifest()
	m.Modules = append(m.Modules, Module{ID: "src/orphan.js", Size: 7})
	a := ComputeAttribution(m)
	if a.UnattributedBytes != 7 || !reflect.DeepEqual(a.UnattributedModules, []string{"src/orphan.js"}) {
		t.Fatalf("unattributed = %d %v", a.UnattributedBytes, a.UnattributedModules)
	}
}

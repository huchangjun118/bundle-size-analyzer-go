package diff

import (
	"testing"

	"github.com/huchangjun118/bundle-size-analyzer-go/internal/manifest"
)

func baseManifest() *manifest.Manifest {
	return &manifest.Manifest{
		Version: 1,
		Chunks: []manifest.Chunk{
			{ID: "app", Size: 500},
			{ID: "old", Size: 100},
		},
		Modules: []manifest.Module{
			{ID: "keep", Size: 100, Chunks: []string{"app"}},
			{ID: "grow", Size: 100, Chunks: []string{"app"}},
			{ID: "shrink", Size: 100, Chunks: []string{"app"}},
			{ID: "drop", Size: 50, Chunks: []string{"old"}},
		},
	}
}

func nextManifest() *manifest.Manifest {
	return &manifest.Manifest{
		Version: 1,
		Chunks: []manifest.Chunk{
			{ID: "app", Size: 600},
			{ID: "fresh", Size: 200},
		},
		Modules: []manifest.Module{
			{ID: "keep", Size: 100, Chunks: []string{"app"}},
			{ID: "grow", Size: 180, Chunks: []string{"app"}},
			{ID: "shrink", Size: 60, Chunks: []string{"app"}},
			{ID: "new", Size: 200, Chunks: []string{"fresh"}},
		},
	}
}

func TestCompareCategories(t *testing.T) {
	rep, err := Compare(baseManifest(), nextManifest())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	got := map[string]Category{}
	for _, e := range rep.Modules {
		got[e.ID] = e.Category
	}
	want := map[string]Category{
		"grow":   Grown,
		"shrink": Shrunk,
		"new":    Added,
		"drop":   Removed,
	}
	for id, c := range want {
		if got[id] != c {
			t.Fatalf("module %s category = %s, want %s", id, got[id], c)
		}
	}
	if _, ok := got["keep"]; ok {
		t.Fatal("unchanged module should not appear")
	}
	// 数值校验
	for _, e := range rep.Modules {
		switch e.ID {
		case "grow":
			if e.Delta != 80 || e.BaseSize != 100 || e.NextSize != 180 {
				t.Fatalf("grow entry = %+v", e)
			}
		case "drop":
			if e.Delta != -50 || e.NextSize != 0 {
				t.Fatalf("drop entry = %+v", e)
			}
		}
	}
	// 资源块粒度
	gotChunk := map[string]Category{}
	for _, e := range rep.Chunks {
		gotChunk[e.ID] = e.Category
	}
	if gotChunk["app"] != Grown || gotChunk["old"] != Removed || gotChunk["fresh"] != Added {
		t.Fatalf("chunk categories = %v", gotChunk)
	}
}

func TestCompareTotals(t *testing.T) {
	rep, _ := Compare(baseManifest(), nextManifest())
	if rep.BaseModuleSum != 350 || rep.NextModuleSum != 540 {
		t.Fatalf("module sums = %d/%d", rep.BaseModuleSum, rep.NextModuleSum)
	}
	if rep.BaseChunkSum != 600 || rep.NextChunkSum != 800 {
		t.Fatalf("chunk sums = %d/%d", rep.BaseChunkSum, rep.NextChunkSum)
	}
}

func TestCompareDeterministicAndSorted(t *testing.T) {
	r1, _ := Compare(baseManifest(), nextManifest())
	r2, _ := Compare(baseManifest(), nextManifest())
	if len(r1.Modules) != len(r2.Modules) {
		t.Fatal("non-deterministic")
	}
	for i := range r1.Modules {
		if r1.Modules[i] != r2.Modules[i] {
			t.Fatalf("non-deterministic at %d", i)
		}
	}
	// 按 ID 排序
	for i := 1; i < len(r1.Modules); i++ {
		if r1.Modules[i-1].ID >= r1.Modules[i].ID {
			t.Fatalf("modules not sorted: %+v", r1.Modules)
		}
	}
}

func TestByCategory(t *testing.T) {
	rep, _ := Compare(baseManifest(), nextManifest())
	mods, _ := rep.ByCategory(Added)
	if len(mods) != 1 || mods[0].ID != "new" {
		t.Fatalf("added modules = %+v", mods)
	}
	_, chunks := rep.ByCategory(Removed)
	if len(chunks) != 1 || chunks[0].ID != "old" {
		t.Fatalf("removed chunks = %+v", chunks)
	}
}

func TestCompareEmpty(t *testing.T) {
	rep, err := Compare(&manifest.Manifest{}, &manifest.Manifest{})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(rep.Modules) != 0 || len(rep.Chunks) != 0 {
		t.Fatal("empty manifests should produce empty diff")
	}
}

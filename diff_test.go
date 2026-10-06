package analyzer

import (
	"reflect"
	"testing"
)

func baseline() (*Snapshot, *Manifest, *Attribution) {
	m := &Manifest{
		Version: 1,
		Chunks: []Chunk{
			{ID: "app", File: "dist/app.js", Size: 100, Entries: []string{"src/app.js"}},
			{ID: "old", File: "dist/old.js", Size: 20, Entries: []string{"src/old.js"}},
		},
		Modules: []Module{
			{ID: "src/app.js", Size: 40, Deps: []string{"src/grow.js", "src/shrink.js"}},
			{ID: "src/grow.js", Size: 10},
			{ID: "src/shrink.js", Size: 30},
			{ID: "src/old.js", Size: 20},
		},
	}
	a := ComputeAttribution(m)
	return NewSnapshot(m, a), m, a
}

func newManifest() *Manifest {
	return &Manifest{
		Version: 1,
		Chunks: []Chunk{
			{ID: "app", File: "dist/app.js", Size: 130, Entries: []string{"src/app.js"}},
			{ID: "extra", File: "dist/extra.js", Size: 5, Entries: []string{"src/new.js"}},
		},
		Modules: []Module{
			{ID: "src/app.js", Size: 40, Deps: []string{"src/grow.js", "src/shrink.js"}},
			{ID: "src/grow.js", Size: 25},   // 增大
			{ID: "src/shrink.js", Size: 12}, // 减小
			{ID: "src/new.js", Size: 5},     // 新增
			// src/old.js 删除
		},
	}
}

func TestDiffCategories(t *testing.T) {
	base, _, _ := baseline()
	m2 := newManifest()
	a2 := ComputeAttribution(m2)
	d := ComputeDiff(base, m2, a2)

	byKind := map[DiffKind][]string{}
	for _, md := range d.Modules {
		byKind[md.Kind] = append(byKind[md.Kind], md.ModuleID)
	}
	want := map[DiffKind][]string{
		DiffAdded:     {"src/new.js"},
		DiffRemoved:   {"src/old.js"},
		DiffIncreased: {"src/grow.js"},
		DiffDecreased: {"src/shrink.js"},
	}
	if !reflect.DeepEqual(byKind, want) {
		t.Fatalf("module diff = %v, want %v", byKind, want)
	}

	// 模块差异能定位到资源块。
	for _, md := range d.Modules {
		if md.ModuleID == "src/grow.js" && !reflect.DeepEqual(md.Chunks, []string{"app"}) {
			t.Fatalf("grow.js chunks = %v", md.Chunks)
		}
	}

	// 资源块维度：app 归因 80 -> 77 减小、old 删除、extra 新增。
	var appDiff, oldDiff, extraDiff *ChunkDiff
	for i := range d.Chunks {
		switch d.Chunks[i].ChunkID {
		case "app":
			appDiff = &d.Chunks[i]
		case "old":
			oldDiff = &d.Chunks[i]
		case "extra":
			extraDiff = &d.Chunks[i]
		}
	}
	if appDiff == nil || appDiff.Kind != DiffDecreased || appDiff.OldBytes != 80 || appDiff.NewBytes != 77 {
		t.Fatalf("app diff = %+v", appDiff)
	}
	if oldDiff == nil || oldDiff.Kind != DiffRemoved {
		t.Fatalf("old diff = %+v", oldDiff)
	}
	if extraDiff == nil || extraDiff.Kind != DiffAdded {
		t.Fatalf("extra diff = %+v", extraDiff)
	}
}

func TestDiffReproducible(t *testing.T) {
	base, _, _ := baseline()
	m2 := newManifest()
	want := ComputeDiff(base, m2, ComputeAttribution(m2))
	for i := 0; i < 20; i++ {
		got := ComputeDiff(base, m2, ComputeAttribution(m2))
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("run %d differs", i)
		}
	}
}

func TestDiffEmptyWhenIdentical(t *testing.T) {
	base, m, _ := baseline()
	d := ComputeDiff(base, m, ComputeAttribution(m))
	if len(d.Modules) != 0 || len(d.Chunks) != 0 {
		t.Fatalf("want empty diff, got %+v", d)
	}
}

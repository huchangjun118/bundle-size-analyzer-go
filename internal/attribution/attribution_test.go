package attribution

import (
	"testing"

	"github.com/huchangjun118/bundle-size-analyzer-go/internal/manifest"
)

// 共享模块场景：shared 同时属于 app 与 vendor，体积只能计一次。
func fixture() *manifest.Manifest {
	return &manifest.Manifest{
		Version: 2,
		Chunks: []manifest.Chunk{
			{ID: "app", Size: 500, Kind: "js"},
			{ID: "vendor", Size: 400, Kind: "js"},
			{ID: "style", Size: 100, Kind: "css"},
		},
		Modules: []manifest.Module{
			{ID: "shared", Size: 200, Chunks: []string{"app", "vendor"}, Dir: "src/lib"},
			{ID: "app-only", Size: 250, Chunks: []string{"app"}, Dir: "src"},
			{ID: "vendor-only", Size: 150, Chunks: []string{"vendor"}, Dir: "node_modules"},
			{ID: "css-mod", Size: 80, Chunks: []string{"style"}, Dir: "src"},
			{ID: "orphan", Size: 60, Dir: "src"}, // 不属于任何资源块
		},
	}
}

func findChunk(rep *Report, id string) *ChunkAttribution {
	for i := range rep.Chunks {
		if rep.Chunks[i].ChunkID == id {
			return &rep.Chunks[i]
		}
	}
	return nil
}

func TestSharedModuleCountedOnce(t *testing.T) {
	rep, err := Analyze(fixture())
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	// shared 归属字典序最小的 app
	app := findChunk(rep, "app")
	if app.ModuleBytes != 200+250 {
		t.Fatalf("app module bytes = %d, want 450", app.ModuleBytes)
	}
	vendor := findChunk(rep, "vendor")
	if vendor.ModuleBytes != 150 {
		t.Fatalf("vendor module bytes = %d, want 150", vendor.ModuleBytes)
	}
	// vendor 引用但未归属的共享体积应作为信息展示
	if vendor.SharedBytes != 200 {
		t.Fatalf("vendor shared bytes = %d, want 200", vendor.SharedBytes)
	}
	// 各块归因之和 == 已归属模块体积
	var sum int64
	for _, c := range rep.Chunks {
		sum += c.ModuleBytes
	}
	if sum != rep.AttributedBytes {
		t.Fatalf("chunk attribution sum %d != attributed %d", sum, rep.AttributedBytes)
	}
	// 归因 + 残差 == 占用总量
	if rep.AttributedBytes+rep.Residual != rep.TotalChunkBytes {
		t.Fatalf("attribution %d + residual %d != total %d",
			rep.AttributedBytes, rep.Residual, rep.TotalChunkBytes)
	}
	if rep.TotalChunkBytes != 1000 || rep.Residual != 1000-680 {
		t.Fatalf("total=%d residual=%d, want 1000/320", rep.TotalChunkBytes, rep.Residual)
	}
	// orphan 不计入任何块
	if rep.UnassignedBytes != 60 || len(rep.Unassigned) != 1 || rep.Unassigned[0] != "orphan" {
		t.Fatalf("unassigned = %+v (%d)", rep.Unassigned, rep.UnassignedBytes)
	}
}

func TestResidualPerChunk(t *testing.T) {
	rep, _ := Analyze(fixture())
	app := findChunk(rep, "app")
	if app.Residual != 500-450 {
		t.Fatalf("app residual = %d, want 50", app.Residual)
	}
	style := findChunk(rep, "style")
	if style.Residual != 20 {
		t.Fatalf("style residual = %d, want 20", style.Residual)
	}
}

func TestDirDimension(t *testing.T) {
	rep, _ := Analyze(fixture())
	want := map[string]int64{"src": 390, "src/lib": 200, "node_modules": 150}
	if len(rep.Dirs) != len(want) {
		t.Fatalf("dirs = %+v", rep.Dirs)
	}
	for _, d := range rep.Dirs {
		if d.Bytes != want[d.Dir] {
			t.Fatalf("dir %s bytes = %d, want %d", d.Dir, d.Bytes, want[d.Dir])
		}
	}
}

func TestSubtreeDimension(t *testing.T) {
	m := &manifest.Manifest{
		Version: 1,
		Modules: []manifest.Module{
			{ID: "a", Size: 10, Deps: []string{"b", "c"}},
			{ID: "b", Size: 20, Deps: []string{"c"}},
			{ID: "c", Size: 40},
		},
	}
	rep, err := Analyze(m)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	byRoot := map[string]int64{}
	for _, s := range rep.Subtrees {
		byRoot[s.Root] = s.Bytes
	}
	// c 被 a、b 共享，但每个子树内只计一次
	if byRoot["a"] != 70 || byRoot["b"] != 60 || byRoot["c"] != 40 {
		t.Fatalf("subtrees = %v", byRoot)
	}
}

func TestSubtreeWithCycle(t *testing.T) {
	m := &manifest.Manifest{
		Version: 1,
		Modules: []manifest.Module{
			{ID: "a", Size: 10, Deps: []string{"b"}},
			{ID: "b", Size: 20, Deps: []string{"a"}},
		},
	}
	rep, err := Analyze(m)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	for _, s := range rep.Subtrees {
		if s.Bytes != 30 {
			t.Fatalf("subtree %s = %d, want 30 (cycle dedup)", s.Root, s.Bytes)
		}
	}
	// 环应被报告而非静默丢弃
	found := false
	for _, is := range rep.Issues {
		if is.Kind == manifest.IssueCycle {
			found = true
		}
	}
	if !found {
		t.Fatal("cycle not reported in issues")
	}
}

func TestMissingModuleReference(t *testing.T) {
	m := &manifest.Manifest{
		Version: 1,
		Chunks:  []manifest.Chunk{{ID: "main", Size: 100}},
		Modules: []manifest.Module{
			{ID: "a", Size: 10, Chunks: []string{"main"}, Deps: []string{"ghost"}},
		},
	}
	rep, err := Analyze(m)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	found := false
	for _, is := range rep.Issues {
		if is.Kind == manifest.IssueMissingModule && is.Target == "ghost" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing module not reported: %+v", rep.Issues)
	}
	// 缺失引用不得影响已有归因
	if findChunk(rep, "main").ModuleBytes != 10 {
		t.Fatal("attribution corrupted by missing reference")
	}
}

func TestDeterministic(t *testing.T) {
	r1, _ := Analyze(fixture())
	r2, _ := Analyze(fixture())
	if len(r1.Chunks) != len(r2.Chunks) || len(r1.Subtrees) != len(r2.Subtrees) {
		t.Fatal("non-deterministic output length")
	}
	for i := range r1.Chunks {
		if r1.Chunks[i].ChunkID != r2.Chunks[i].ChunkID || r1.Chunks[i].ModuleBytes != r2.Chunks[i].ModuleBytes {
			t.Fatal("non-deterministic chunk output")
		}
	}
	// 顺序应为字典序
	ids := []string{r1.Chunks[0].ChunkID, r1.Chunks[1].ChunkID, r1.Chunks[2].ChunkID}
	if ids[0] != "app" || ids[1] != "style" || ids[2] != "vendor" {
		t.Fatalf("chunks not sorted: %v", ids)
	}
}

// 格式演进兼容性：v2 新增字段不得改变已有字段的归因结果。
func TestNewFieldsDoNotChangeAttribution(t *testing.T) {
	v1 := []byte(`{
	  "version": 1,
	  "chunks": [{"id": "app", "size": 500}, {"id": "vendor", "size": 400}],
	  "modules": [
	    {"id": "shared", "size": 200, "chunks": ["app", "vendor"]},
	    {"id": "app-only", "size": 250, "chunks": ["app"]}
	  ]
	}`)
	v2 := []byte(`{
	  "version": 2,
	  "chunks": [
	    {"id": "app", "size": 500, "kind": "js", "sourcemap": true},
	    {"id": "vendor", "size": 400, "kind": "js", "future": {"nested": [1]}}
	  ],
	  "modules": [
	    {"id": "shared", "size": 200, "chunks": ["app", "vendor"], "dir": "lib", "hash": "h1"},
	    {"id": "app-only", "size": 250, "chunks": ["app"], "dir": "src", "hash": "h2"}
	  ],
	  "metadata": {"builtAt": "2026-01-01"}
	}`)
	m1, err := manifest.Parse(v1)
	if err != nil {
		t.Fatalf("parse v1: %v", err)
	}
	m2, err := manifest.Parse(v2)
	if err != nil {
		t.Fatalf("parse v2: %v", err)
	}
	r1, err := Analyze(m1)
	if err != nil {
		t.Fatalf("analyze v1: %v", err)
	}
	r2, err := Analyze(m2)
	if err != nil {
		t.Fatalf("analyze v2: %v", err)
	}
	if r1.TotalChunkBytes != r2.TotalChunkBytes ||
		r1.AttributedBytes != r2.AttributedBytes ||
		r1.Residual != r2.Residual {
		t.Fatalf("totals changed: v1=%+v v2=%+v", r1, r2)
	}
	for i := range r1.Chunks {
		a, b := r1.Chunks[i], r2.Chunks[i]
		if a.ChunkID != b.ChunkID || a.ModuleBytes != b.ModuleBytes ||
			a.Residual != b.Residual || a.SharedBytes != b.SharedBytes {
			t.Fatalf("chunk %d changed: %+v vs %+v", i, a, b)
		}
	}
}

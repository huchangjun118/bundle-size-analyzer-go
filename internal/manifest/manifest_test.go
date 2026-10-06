package manifest

import (
	"testing"
)

func TestParseV1(t *testing.T) {
	data := []byte(`{
	  "version": 1,
	  "chunks": [{"id": "main", "size": 1000, "files": ["main.js"]}],
	  "modules": [
	    {"id": "b", "size": 300, "chunks": ["main"], "deps": ["a"]},
	    {"id": "a", "size": 200, "chunks": ["main"]}
	  ]
	}`)
	m, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Version != 1 || len(m.Chunks) != 1 || len(m.Modules) != 2 {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	// deps/chunks 应被排序以保证确定性
	if m.Modules[0].Deps[0] != "a" {
		t.Fatalf("deps not normalized: %+v", m.Modules[0])
	}
}

func TestParseV2IgnoresUnknownFields(t *testing.T) {
	v2 := []byte(`{
	  "version": 2,
	  "chunks": [{"id": "main", "size": 1000, "kind": "js", "futureField": {"x": 1}}],
	  "modules": [{"id": "a", "size": 200, "chunks": ["main"], "dir": "src", "hash": "abc"}],
	  "brandNewTopLevel": [1, 2, 3]
	}`)
	m, err := Parse(v2)
	if err != nil {
		t.Fatalf("Parse v2: %v", err)
	}
	if m.Chunks[0].Kind != "js" || m.Modules[0].Dir != "src" {
		t.Fatalf("v2 fields not parsed: %+v", m)
	}
	if m.Modules[0].Size != 200 || m.Chunks[0].Size != 1000 {
		t.Fatalf("existing fields altered by new fields: %+v", m)
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	if _, err := Parse([]byte(`not json`)); err == nil {
		t.Fatal("want error for invalid JSON")
	}
	if _, err := Parse([]byte(`{"version": 99}`)); err == nil {
		t.Fatal("want error for unsupported version")
	}
	if _, err := Parse([]byte(`{"version": 0}`)); err == nil {
		t.Fatal("want error for version 0")
	}
}

func TestValidateMissingAndCycle(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Chunks:  []Chunk{{ID: "main", Size: 100}},
		Modules: []Module{
			{ID: "a", Size: 10, Chunks: []string{"main", "ghost"}, Deps: []string{"b", "missing"}},
			{ID: "b", Size: 10, Chunks: []string{"main"}, Deps: []string{"a"}},
		},
	}
	issues := Validate(m)
	kinds := map[IssueKind]int{}
	for _, is := range issues {
		kinds[is.Kind]++
	}
	if kinds[IssueMissingModule] != 1 {
		t.Fatalf("want 1 missing module, got %d: %+v", kinds[IssueMissingModule], issues)
	}
	if kinds[IssueMissingChunk] != 1 {
		t.Fatalf("want 1 missing chunk, got %d: %+v", kinds[IssueMissingChunk], issues)
	}
	if kinds[IssueCycle] != 1 {
		t.Fatalf("want 1 cycle, got %d: %+v", kinds[IssueCycle], issues)
	}
	// 环的描述应规范化且确定
	for _, is := range issues {
		if is.Kind == IssueCycle && is.Detail != "a -> b -> a" {
			t.Fatalf("cycle not canonical: %q", is.Detail)
		}
	}
}

func TestValidateDeterministic(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Modules: []Module{
			{ID: "z", Size: 1, Deps: []string{"y"}},
			{ID: "y", Size: 1, Deps: []string{"x"}},
		},
	}
	a := Validate(m)
	b := Validate(m)
	if len(a) != len(b) {
		t.Fatal("non-deterministic issue count")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic issues: %+v vs %+v", a, b)
		}
	}
}

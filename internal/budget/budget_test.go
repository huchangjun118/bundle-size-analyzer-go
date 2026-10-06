package budget

import (
	"testing"

	"github.com/huchangjun118/bundle-size-analyzer-go/internal/attribution"
	"github.com/huchangjun118/bundle-size-analyzer-go/internal/manifest"
)

func report() *attribution.Report {
	m := &manifest.Manifest{
		Version: 2,
		Chunks: []manifest.Chunk{
			{ID: "app", Size: 500, Kind: "js"},
			{ID: "vendor", Size: 400, Kind: "js"},
			{ID: "style", Size: 100, Kind: "css"},
		},
		Modules: []manifest.Module{
			{ID: "a", Size: 450, Chunks: []string{"app"}},
			{ID: "v", Size: 380, Chunks: []string{"vendor"}},
		},
	}
	rep, err := attribution.Analyze(m)
	if err != nil {
		panic(err)
	}
	return rep
}

func TestEvaluateScopes(t *testing.T) {
	rules := []Rule{
		{Name: "total-cap", Scope: ScopeTotal, Limit: 1000},             // 1000 <= 1000 通过
		{Name: "app-cap", Scope: ScopeChunk, Target: "app", Limit: 400}, // 500 > 400 超限
		{Name: "js-cap", Scope: ScopeKind, Target: "js", Limit: 1000},   // 900 <= 1000 通过
		{Name: "css-cap", Scope: ScopeKind, Target: "css", Limit: 50},   // 100 > 50 超限
	}
	res, err := Evaluate(report(), rules)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.Passed {
		t.Fatal("expected overall failure")
	}
	if len(res.Evaluations) != 4 {
		t.Fatalf("evaluations = %d, want 4", len(res.Evaluations))
	}
	checks := []struct {
		name   string
		actual int64
		passed bool
		overBy int64
	}{
		{"total-cap", 1000, true, 0},
		{"app-cap", 500, false, 100},
		{"js-cap", 900, true, 0},
		{"css-cap", 100, false, 50},
	}
	for i, c := range checks {
		e := res.Evaluations[i]
		if e.Rule.Name != c.name || e.Actual != c.actual || e.Passed != c.passed || e.OverBy != c.overBy {
			t.Fatalf("evaluation %d = %+v, want %+v", i, e, c)
		}
	}
	violations := res.Violations()
	if len(violations) != 2 {
		t.Fatalf("violations = %d, want 2", len(violations))
	}
}

func TestEvaluateMissingTarget(t *testing.T) {
	rules := []Rule{{Name: "ghost", Scope: ScopeChunk, Target: "nope", Limit: 10}}
	res, err := Evaluate(report(), rules)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	e := res.Evaluations[0]
	if e.Found {
		t.Fatal("expected Found=false for missing chunk")
	}
	if !e.Passed {
		t.Fatal("missing target with actual 0 should pass")
	}
}

func TestEvaluateInvalidRules(t *testing.T) {
	rep := report()
	if _, err := Evaluate(rep, []Rule{{Name: "x", Scope: "weird", Limit: 1}}); err == nil {
		t.Fatal("want error for unknown scope")
	}
	if _, err := Evaluate(rep, []Rule{{Name: "x", Scope: ScopeChunk, Limit: 1}}); err == nil {
		t.Fatal("want error for missing target")
	}
	if _, err := Evaluate(rep, []Rule{{Name: "x", Scope: ScopeTotal, Limit: -1}}); err == nil {
		t.Fatal("want error for negative limit")
	}
}

func TestEvaluateDeterministic(t *testing.T) {
	rules := []Rule{
		{Name: "a", Scope: ScopeTotal, Limit: 10},
		{Name: "b", Scope: ScopeKind, Target: "js", Limit: 10},
	}
	r1, _ := Evaluate(report(), rules)
	r2, _ := Evaluate(report(), rules)
	for i := range r1.Evaluations {
		if r1.Evaluations[i] != r2.Evaluations[i] {
			t.Fatal("non-deterministic evaluation")
		}
	}
}

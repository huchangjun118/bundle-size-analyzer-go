package analyzer

import (
	"reflect"
	"testing"
)

func budgetFixture() (*Manifest, *Attribution) {
	m := &Manifest{
		Version: 2,
		Chunks: []Chunk{
			{ID: "app", File: "dist/app.js", Size: 100, Kind: "js", Entries: []string{"src/app.js"}},
			{ID: "style", File: "dist/style.css", Size: 40, Kind: "css", Entries: []string{"src/style.js"}},
		},
		Modules: []Module{
			{ID: "src/app.js", Size: 90},
			{ID: "src/style.js", Size: 30},
		},
	}
	return m, ComputeAttribution(m)
}

func TestBudgetPassAndFail(t *testing.T) {
	m, a := budgetFixture()
	rep := CheckBudget(m, a, []BudgetRule{
		{Name: "total-cap", Scope: ScopeTotal, Limit: 200},             // 140 <= 200 通过
		{Name: "app-cap", Scope: ScopeChunk, Target: "app", Limit: 80}, // 90 > 80 超限
		{Name: "css-cap", Scope: ScopeKind, Target: "css", Limit: 50},  // 30 <= 50 通过
		{Name: "ghost", Scope: ScopeChunk, Target: "ghost", Limit: 1},  // 目标不存在
	})
	if rep.Passed {
		t.Fatal("want Passed=false because app-cap fails")
	}
	got := map[string]BudgetResult{}
	for _, r := range rep.Results {
		got[r.Rule.Name] = r
	}
	if got["total-cap"].Status != StatusPass || got["total-cap"].Actual != 140 {
		t.Fatalf("total-cap = %+v", got["total-cap"])
	}
	if got["app-cap"].Status != StatusFail || got["app-cap"].OverBy != 10 {
		t.Fatalf("app-cap = %+v", got["app-cap"])
	}
	if got["css-cap"].Status != StatusPass {
		t.Fatalf("css-cap = %+v", got["css-cap"])
	}
	if got["ghost"].Status != StatusUnknown {
		t.Fatalf("ghost = %+v", got["ghost"])
	}
}

func TestBudgetDeterministicOrder(t *testing.T) {
	m, a := budgetFixture()
	rules := []BudgetRule{
		{Name: "z", Scope: ScopeTotal, Limit: 200},
		{Name: "a", Scope: ScopeChunk, Target: "app", Limit: 200},
		{Name: "m", Scope: ScopeKind, Target: "css", Limit: 200},
	}
	want := CheckBudget(m, a, rules)
	// 打乱顺序不影响结果。
	rev := []BudgetRule{rules[2], rules[1], rules[0]}
	got := CheckBudget(m, a, rev)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("order matters:\n%+v\n%+v", want, got)
	}
}

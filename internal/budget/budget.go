// Package budget 提供传输/体积预算规则与判定。
//
// 规则可作用于三个范围：总体(total)、单个资源块(chunk)、某一类资源(kind)。
// 判定完全确定：同一归因报告与同一规则列表必然得到同一结果。
package budget

import (
	"fmt"

	"github.com/huchangjun118/bundle-size-analyzer-go/internal/attribution"
)

// Scope 是规则作用的范围。
type Scope string

const (
	ScopeTotal Scope = "total" // 全部资源块字节总量
	ScopeChunk Scope = "chunk" // 单个资源块，Target 为资源块 ID
	ScopeKind  Scope = "kind"  // 某类资源，Target 为资源类别（如 js/css）
)

// Rule 是一条预算规则。Limit 为允许的最大字节数（含）。
type Rule struct {
	Name   string `json:"name"`
	Scope  Scope  `json:"scope"`
	Target string `json:"target,omitempty"` // chunk/kind 范围的目标
	Limit  int64  `json:"limit"`
}

// Evaluation 是一条规则的评估明细。
type Evaluation struct {
	Rule   Rule  `json:"rule"`
	Actual int64 `json:"actual"`  // 实际字节数
	Found  bool  `json:"found"`   // chunk/kind 目标是否存在
	Passed bool  `json:"passed"`  // Actual <= Limit
	OverBy int64 `json:"over_by"` // 超出字节数，未超为 0
}

// Result 是预算判定结果。
type Result struct {
	Passed      bool         `json:"passed"`
	Evaluations []Evaluation `json:"evaluations"` // 与输入规则一一对应，顺序一致
}

// Violations 返回所有未通过的评估。
func (r *Result) Violations() []Evaluation {
	var out []Evaluation
	for _, e := range r.Evaluations {
		if !e.Passed {
			out = append(out, e)
		}
	}
	return out
}

// Evaluate 依据归因报告评估预算规则。规则按输入顺序逐条评估。
func Evaluate(rep *attribution.Report, rules []Rule) (*Result, error) {
	for _, r := range rules {
		switch r.Scope {
		case ScopeTotal, ScopeChunk, ScopeKind:
		default:
			return nil, fmt.Errorf("budget: rule %q: unknown scope %q", r.Name, r.Scope)
		}
		if r.Scope != ScopeTotal && r.Target == "" {
			return nil, fmt.Errorf("budget: rule %q: scope %q requires a target", r.Name, r.Scope)
		}
		if r.Limit < 0 {
			return nil, fmt.Errorf("budget: rule %q: negative limit %d", r.Name, r.Limit)
		}
	}

	res := &Result{Passed: true}
	for _, r := range rules {
		actual, found := resolve(rep, r)
		e := Evaluation{Rule: r, Actual: actual, Found: found}
		if actual > r.Limit {
			e.OverBy = actual - r.Limit
		}
		e.Passed = e.OverBy == 0
		if !e.Passed {
			res.Passed = false
		}
		res.Evaluations = append(res.Evaluations, e)
	}
	return res, nil
}

// resolve 计算规则覆盖的实际字节数。
func resolve(rep *attribution.Report, r Rule) (int64, bool) {
	switch r.Scope {
	case ScopeTotal:
		return rep.TotalChunkBytes, true
	case ScopeChunk:
		for _, c := range rep.Chunks {
			if c.ChunkID == r.Target {
				return c.Total, true
			}
		}
		return 0, false
	case ScopeKind:
		var sum int64
		found := false
		for _, c := range rep.Chunks {
			if c.Kind == r.Target {
				sum += c.Total
				found = true
			}
		}
		return sum, found
	}
	return 0, false
}

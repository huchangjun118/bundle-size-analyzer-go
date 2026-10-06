package analyzer

import "sort"

// BudgetScope 预算规则的作用范围。
type BudgetScope string

const (
	ScopeTotal BudgetScope = "total" // 总体占用
	ScopeChunk BudgetScope = "chunk" // 单个资源块，Target 为资源块 ID
	ScopeKind  BudgetScope = "kind"  // 某一类资源，Target 为资源类别
)

// BudgetRule 一条预算规则。
type BudgetRule struct {
	Name   string
	Scope  BudgetScope
	Target string // ScopeChunk 时为资源块 ID，ScopeKind 时为资源类别
	Limit  int64  // 上限字节数（含）
}

// BudgetStatus 单条规则的判定结果。
type BudgetStatus string

const (
	StatusPass    BudgetStatus = "pass"
	StatusFail    BudgetStatus = "fail"
	StatusUnknown BudgetStatus = "unknown_target" // 规则指向不存在的目标
)

// BudgetResult 单条规则的判定明细。
type BudgetResult struct {
	Rule   BudgetRule
	Status BudgetStatus
	Actual int64
	OverBy int64 // 超出部分，未超限时为 0
}

// BudgetReport 全量规则的判定报告，Passed 为 false 时应阻断合并。
type BudgetReport struct {
	Passed  bool
	Results []BudgetResult
}

// CheckBudget 依据归因结果判定预算。
// 规则先按 (Scope, Target, Name) 排序再求值，命中与阈值判断完全确定，
// 与调用方传入的规则顺序无关。
func CheckBudget(m *Manifest, a *Attribution, rules []BudgetRule) *BudgetReport {
	sorted := append([]BudgetRule(nil), rules...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Scope != sorted[j].Scope {
			return sorted[i].Scope < sorted[j].Scope
		}
		if sorted[i].Target != sorted[j].Target {
			return sorted[i].Target < sorted[j].Target
		}
		return sorted[i].Name < sorted[j].Name
	})

	// 预聚合：资源块归因字节、类别归因字节。
	chunkBytes := make(map[string]int64, len(a.Chunks))
	for _, c := range a.Chunks {
		chunkBytes[c.ChunkID] = c.Bytes
	}
	kindBytes := make(map[string]int64)
	for _, c := range m.Chunks {
		kindBytes[chunkKind(c)] += chunkBytes[c.ID]
	}

	rep := &BudgetReport{Passed: true}
	for _, r := range sorted {
		res := BudgetResult{Rule: r, Status: StatusPass}
		var actual int64
		switch r.Scope {
		case ScopeTotal:
			actual = a.TotalOccupied
		case ScopeChunk:
			b, ok := chunkBytes[r.Target]
			if !ok {
				// 目标不存在：结果可区分，但不视为超限，不阻断合并。
				res.Status = StatusUnknown
				rep.Results = append(rep.Results, res)
				continue
			}
			actual = b
		case ScopeKind:
			b, ok := kindBytes[r.Target]
			if !ok {
				res.Status = StatusUnknown
				rep.Results = append(rep.Results, res)
				continue
			}
			actual = b
		default:
			res.Status = StatusUnknown
			rep.Results = append(rep.Results, res)
			continue
		}
		res.Actual = actual
		if actual > r.Limit {
			res.Status = StatusFail
			res.OverBy = actual - r.Limit
			rep.Passed = false
		}
		rep.Results = append(rep.Results, res)
	}
	return rep
}

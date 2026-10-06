// Package manifest 解析产物清单，兼容多版本格式。
//
// v1 格式字段：version/chunks(id,size,files)/modules(id,size,deps,chunks)。
// v2 在 v1 基础上新增 chunk.kind 与 module.dir 字段。
// 解析时忽略未知字段，因此新增字段不会改变已有字段的归因结果。
package manifest

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Module 表示一个构建模块。
type Module struct {
	ID     string   `json:"id"`
	Size   int64    `json:"size"`
	Deps   []string `json:"deps"`   // 依赖的模块 ID
	Chunks []string `json:"chunks"` // 所属资源块 ID
	Dir    string   `json:"dir"`    // v2: 目录维度
}

// Chunk 表示一个资源块（产物文件）。
type Chunk struct {
	ID    string   `json:"id"`
	Size  int64    `json:"size"` // 产物文件总字节数
	Files []string `json:"files"`
	Kind  string   `json:"kind"` // v2: 资源类别，如 js/css/asset
}

// Manifest 是一份产物清单。
type Manifest struct {
	Version int      `json:"version"`
	Chunks  []Chunk  `json:"chunks"`
	Modules []Module `json:"modules"`
}

// 支持的最高清单版本。
const MaxVersion = 2

// Parse 从字节数据解析清单，自动识别版本。
// 未知字段被忽略；版本高于 MaxVersion 时返回错误。
func Parse(data []byte) (*Manifest, error) {
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("manifest: invalid JSON: %w", err)
	}
	if head.Version < 1 || head.Version > MaxVersion {
		return nil, fmt.Errorf("manifest: unsupported version %d (supported: 1..%d)", head.Version, MaxVersion)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest: decode: %w", err)
	}
	for i := range m.Modules {
		mod := &m.Modules[i]
		if mod.Deps == nil {
			mod.Deps = []string{}
		}
		if mod.Chunks == nil {
			mod.Chunks = []string{}
		}
		sort.Strings(mod.Deps)
		sort.Strings(mod.Chunks)
	}
	for i := range m.Chunks {
		if m.Chunks[i].Files == nil {
			m.Chunks[i].Files = []string{}
		}
		sort.Strings(m.Chunks[i].Files)
	}
	return &m, nil
}

// IssueKind 区分校验发现的问题类别。
type IssueKind string

const (
	IssueDuplicateModuleID IssueKind = "duplicate_module_id"
	IssueDuplicateChunkID  IssueKind = "duplicate_chunk_id"
	IssueMissingModule     IssueKind = "missing_module"   // 依赖指向不存在的模块
	IssueMissingChunk      IssueKind = "missing_chunk"    // 模块指向不存在的资源块
	IssueCycle             IssueKind = "dependency_cycle" // 模块引用成环
	IssueNegativeSize      IssueKind = "negative_size"
)

// Issue 描述清单中的一处异常。
type Issue struct {
	Kind   IssueKind `json:"kind"`
	Module string    `json:"module,omitempty"` // 相关模块 ID
	Chunk  string    `json:"chunk,omitempty"`  // 相关资源块 ID
	Target string    `json:"target,omitempty"` // 指向的目标 ID
	Detail string    `json:"detail,omitempty"`
}

// Validate 校验清单，返回全部问题，按 (Kind, Module, Chunk, Target) 排序保证确定性。
// 校验不丢弃数据：问题以 Issue 形式显式上报。
func Validate(m *Manifest) []Issue {
	var issues []Issue
	modSeen := map[string]bool{}
	for _, mod := range m.Modules {
		if modSeen[mod.ID] {
			issues = append(issues, Issue{Kind: IssueDuplicateModuleID, Module: mod.ID})
		}
		modSeen[mod.ID] = true
		if mod.Size < 0 {
			issues = append(issues, Issue{Kind: IssueNegativeSize, Module: mod.ID, Detail: fmt.Sprintf("size=%d", mod.Size)})
		}
	}
	chunkSeen := map[string]bool{}
	for _, ch := range m.Chunks {
		if chunkSeen[ch.ID] {
			issues = append(issues, Issue{Kind: IssueDuplicateChunkID, Chunk: ch.ID})
		}
		chunkSeen[ch.ID] = true
		if ch.Size < 0 {
			issues = append(issues, Issue{Kind: IssueNegativeSize, Chunk: ch.ID, Detail: fmt.Sprintf("size=%d", ch.Size)})
		}
	}
	for _, mod := range m.Modules {
		for _, dep := range mod.Deps {
			if !modSeen[dep] {
				issues = append(issues, Issue{Kind: IssueMissingModule, Module: mod.ID, Target: dep})
			}
		}
		for _, c := range mod.Chunks {
			if !chunkSeen[c] {
				issues = append(issues, Issue{Kind: IssueMissingChunk, Module: mod.ID, Target: c})
			}
		}
	}
	issues = append(issues, detectCycles(m)...)
	sort.Slice(issues, func(i, j int) bool {
		a, b := issues[i], issues[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Module != b.Module {
			return a.Module < b.Module
		}
		if a.Chunk != b.Chunk {
			return a.Chunk < b.Chunk
		}
		return a.Target < b.Target
	})
	return issues
}

// detectCycles 用三色 DFS 找环，每条环只报告一次（以环上最小 ID 为起点规范化）。
func detectCycles(m *Manifest) []Issue {
	deps := map[string][]string{}
	for _, mod := range m.Modules {
		deps[mod.ID] = mod.Deps
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	reported := map[string]bool{} // 以规范化环 key 去重
	var issues []Issue
	var stack []string

	ids := make([]string, 0, len(deps))
	for id := range deps {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var visit func(id string)
	visit = func(id string) {
		color[id] = gray
		stack = append(stack, id)
		for _, dep := range deps[id] {
			if _, ok := deps[dep]; !ok {
				continue // 缺失模块由 MissingModule 报告
			}
			switch color[dep] {
			case gray:
				// 找到环：从 stack 中 dep 的位置到栈顶
				idx := 0
				for i, s := range stack {
					if s == dep {
						idx = i
						break
					}
				}
				cycle := append([]string{}, stack[idx:]...)
				key := canonicalCycle(cycle)
				if !reported[key] {
					reported[key] = true
					issues = append(issues, Issue{Kind: IssueCycle, Module: dep, Detail: key})
				}
			case white:
				visit(dep)
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = black
	}
	for _, id := range ids {
		if color[id] == white {
			visit(id)
		}
	}
	return issues
}

// canonicalCycle 将环旋转到最小元素开头，保证同一环的表示唯一。
func canonicalCycle(cycle []string) string {
	if len(cycle) == 0 {
		return ""
	}
	min := 0
	for i := range cycle {
		if cycle[i] < cycle[min] {
			min = i
		}
	}
	rot := append(append([]string{}, cycle[min:]...), cycle[:min]...)
	out := rot[0]
	for _, s := range rot[1:] {
		out += " -> " + s
	}
	return out + " -> " + rot[0]
}

package analyzer

import "sort"

// DiffKind 差异类别。
type DiffKind string

const (
	DiffAdded     DiffKind = "added"
	DiffRemoved   DiffKind = "removed"
	DiffIncreased DiffKind = "increased"
	DiffDecreased DiffKind = "decreased"
)

// ModuleDiff 定位到具体模块的差异。
type ModuleDiff struct {
	Kind     DiffKind
	ModuleID string
	OldBytes int64
	NewBytes int64
	Chunks   []string // 该模块归属的资源块（新产物侧；删除时为基线侧）
}

// ChunkDiff 定位到资源块的差异。
type ChunkDiff struct {
	Kind     DiffKind
	ChunkID  string
	OldBytes int64
	NewBytes int64
}

// Diff 是基线快照与新产物之间的完整差异，条目顺序确定。
type Diff struct {
	Modules []ModuleDiff
	Chunks  []ChunkDiff
}

// Snapshot 是一份可序列化的归因快照，用作增量对比的基线。
type Snapshot struct {
	Modules map[string]int64  // 模块 ID -> 字节数
	Chunks  map[string]int64  // 资源块 ID -> 归因字节数
	Owners  map[string]string // 模块 ID -> 属主资源块 ID
}

// NewSnapshot 由归因结果生成快照。
func NewSnapshot(m *Manifest, a *Attribution) *Snapshot {
	s := &Snapshot{
		Modules: make(map[string]int64, len(m.Modules)),
		Chunks:  make(map[string]int64, len(a.Chunks)),
		Owners:  make(map[string]string, len(a.owner)),
	}
	for _, mod := range m.Modules {
		if _, dup := s.Modules[mod.ID]; !dup {
			s.Modules[mod.ID] = mod.Size
		}
	}
	for _, c := range a.Chunks {
		s.Chunks[c.ChunkID] = c.Bytes
	}
	for mod, owner := range a.owner {
		s.Owners[mod] = owner
	}
	return s
}

// ComputeDiff 对比基线快照与新产物，相同输入下输出完全可复现：
// 模块与资源块差异分别按 ID 升序，大小不变的条目不出现。
func ComputeDiff(base *Snapshot, m *Manifest, a *Attribution) *Diff {
	cur := NewSnapshot(m, a)
	d := &Diff{}

	// 模块维度。
	for _, id := range unionKeys(base.Modules, cur.Modules) {
		oldB, inOld := base.Modules[id]
		newB, inNew := cur.Modules[id]
		switch {
		case !inOld:
			d.Modules = append(d.Modules, ModuleDiff{
				Kind: DiffAdded, ModuleID: id, NewBytes: newB,
				Chunks: ownerList(cur.Owners[id]),
			})
		case !inNew:
			d.Modules = append(d.Modules, ModuleDiff{
				Kind: DiffRemoved, ModuleID: id, OldBytes: oldB,
				Chunks: ownerList(base.Owners[id]),
			})
		case newB > oldB:
			d.Modules = append(d.Modules, ModuleDiff{
				Kind: DiffIncreased, ModuleID: id, OldBytes: oldB, NewBytes: newB,
				Chunks: ownerList(cur.Owners[id]),
			})
		case newB < oldB:
			d.Modules = append(d.Modules, ModuleDiff{
				Kind: DiffDecreased, ModuleID: id, OldBytes: oldB, NewBytes: newB,
				Chunks: ownerList(cur.Owners[id]),
			})
		}
	}

	// 资源块维度。
	for _, id := range unionKeys(base.Chunks, cur.Chunks) {
		oldB, inOld := base.Chunks[id]
		newB, inNew := cur.Chunks[id]
		switch {
		case !inOld:
			d.Chunks = append(d.Chunks, ChunkDiff{Kind: DiffAdded, ChunkID: id, NewBytes: newB})
		case !inNew:
			d.Chunks = append(d.Chunks, ChunkDiff{Kind: DiffRemoved, ChunkID: id, OldBytes: oldB})
		case newB > oldB:
			d.Chunks = append(d.Chunks, ChunkDiff{Kind: DiffIncreased, ChunkID: id, OldBytes: oldB, NewBytes: newB})
		case newB < oldB:
			d.Chunks = append(d.Chunks, ChunkDiff{Kind: DiffDecreased, ChunkID: id, OldBytes: oldB, NewBytes: newB})
		}
	}
	return d
}

func unionKeys(maps ...map[string]int64) []string {
	seen := make(map[string]bool)
	var out []string
	for _, m := range maps {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

func ownerList(owner string) []string {
	if owner == "" {
		return nil
	}
	return []string{owner}
}

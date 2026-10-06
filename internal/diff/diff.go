// Package diff 提供基线快照与新产物的增量对比。
//
// 差异分四类：新增(added)、删除(removed)、增大(grown)、减小(shrunk)，
// 分别在模块与资源块两个粒度上给出。体积不变的条目不出现在结果中。
// 全部条目按 ID 排序，相同输入下结果完全可复现。
package diff

import (
	"sort"

	"github.com/huchangjun118/bundle-size-analyzer-go/internal/manifest"
)

// Category 是差异类别。
type Category string

const (
	Added   Category = "added"
	Removed Category = "removed"
	Grown   Category = "grown"
	Shrunk  Category = "shrunk"
)

// Entry 是一条差异记录。
type Entry struct {
	ID       string   `json:"id"`
	Category Category `json:"category"`
	BaseSize int64    `json:"base_size"` // 基线字节数，added 时为 0
	NextSize int64    `json:"next_size"` // 新产物字节数，removed 时为 0
	Delta    int64    `json:"delta"`     // NextSize − BaseSize
}

// Report 是完整差异结果。
type Report struct {
	Modules       []Entry `json:"modules"` // 模块粒度差异，按 ID 排序
	Chunks        []Entry `json:"chunks"`  // 资源块粒度差异，按 ID 排序
	BaseModuleSum int64   `json:"base_module_sum"`
	NextModuleSum int64   `json:"next_module_sum"`
	BaseChunkSum  int64   `json:"base_chunk_sum"`
	NextChunkSum  int64   `json:"next_chunk_sum"`
}

// ByCategory 过滤出某一类别的条目，保持原有排序。
func (r *Report) ByCategory(c Category) (modules, chunks []Entry) {
	for _, e := range r.Modules {
		if e.Category == c {
			modules = append(modules, e)
		}
	}
	for _, e := range r.Chunks {
		if e.Category == c {
			chunks = append(chunks, e)
		}
	}
	return modules, chunks
}

// Compare 对比基线与新清单。两侧输入的异常（重复 ID 等）不阻断对比，
// 重复 ID 取首次出现的值，保证结果确定。
func Compare(base, next *manifest.Manifest) (*Report, error) {
	rep := &Report{}
	rep.Modules = diffSet(moduleSizes(base), moduleSizes(next))
	rep.Chunks = diffSet(chunkSizes(base), chunkSizes(next))
	for _, s := range moduleSizes(base) {
		rep.BaseModuleSum += s
	}
	for _, s := range moduleSizes(next) {
		rep.NextModuleSum += s
	}
	for _, s := range chunkSizes(base) {
		rep.BaseChunkSum += s
	}
	for _, s := range chunkSizes(next) {
		rep.NextChunkSum += s
	}
	return rep, nil
}

func moduleSizes(m *manifest.Manifest) map[string]int64 {
	out := map[string]int64{}
	for _, mod := range m.Modules {
		if _, ok := out[mod.ID]; !ok {
			out[mod.ID] = mod.Size
		}
	}
	return out
}

func chunkSizes(m *manifest.Manifest) map[string]int64 {
	out := map[string]int64{}
	for _, ch := range m.Chunks {
		if _, ok := out[ch.ID]; !ok {
			out[ch.ID] = ch.Size
		}
	}
	return out
}

// diffSet 计算两个 size 集合的差异，输出按 ID 排序。
func diffSet(base, next map[string]int64) []Entry {
	ids := map[string]bool{}
	for id := range base {
		ids[id] = true
	}
	for id := range next {
		ids[id] = true
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)

	out := []Entry{}
	for _, id := range sorted {
		b, inBase := base[id]
		n, inNext := next[id]
		var e Entry
		switch {
		case !inBase:
			e = Entry{ID: id, Category: Added, NextSize: n, Delta: n}
		case !inNext:
			e = Entry{ID: id, Category: Removed, BaseSize: b, Delta: -b}
		case n > b:
			e = Entry{ID: id, Category: Grown, BaseSize: b, NextSize: n, Delta: n - b}
		case n < b:
			e = Entry{ID: id, Category: Shrunk, BaseSize: b, NextSize: n, Delta: n - b}
		default:
			continue // 体积不变
		}
		out = append(out, e)
	}
	return out
}

// Package attribution 计算体积归因。
//
// 归因语义：
//   - 每个模块的体积全局只计一次。被多个资源块共享的模块，
//     归属于其资源块列表中 ID 字典序最小的那个（确定性归属规则），
//     因此各资源块归因之和恒等于已归属模块体积之和。
//   - 资源块的残差 = 资源块实际字节数 − 归属到该块的模块体积之和，
//     表示清单未能解释的部分（如运行时胶水代码），允许为负并如实上报。
//   - 不属于任何资源块的模块列为 Unassigned，不计入任何资源块。
//   - 全部输出切片按 ID 排序，同一份输入的输出完全确定。
package attribution

import (
	"sort"

	"github.com/huchangjun118/bundle-size-analyzer-go/internal/manifest"
)

// ChunkAttribution 是单个资源块的归因。
type ChunkAttribution struct {
	ChunkID      string   `json:"chunk_id"`
	Kind         string   `json:"kind,omitempty"`
	Total        int64    `json:"total"`         // 资源块实际字节数
	ModuleBytes  int64    `json:"module_bytes"`  // 归属到本块的模块体积之和
	Residual     int64    `json:"residual"`      // Total − ModuleBytes
	SharedBytes  int64    `json:"shared_bytes"`  // 本块引用但未归属本块的共享模块体积（仅信息展示，不计费）
	OwnedModules []string `json:"owned_modules"` // 归属到本块的模块，已排序
}

// DirAttribution 是目录维度的归因。
type DirAttribution struct {
	Dir     string   `json:"dir"`
	Bytes   int64    `json:"bytes"`
	Modules []string `json:"modules"`
}

// SubtreeAttribution 是依赖子树维度的归因：根模块及其全部传递依赖，
// 子树内部去重，每个模块只计一次。
type SubtreeAttribution struct {
	Root    string   `json:"root"`
	Bytes   int64    `json:"bytes"`
	Modules []string `json:"modules"`
}

// Report 是完整归因结果。
type Report struct {
	TotalChunkBytes  int64                `json:"total_chunk_bytes"`  // 占用总量：Σ chunk.Size
	TotalModuleBytes int64                `json:"total_module_bytes"` // Σ module.Size
	AttributedBytes  int64                `json:"attributed_bytes"`   // 归属到资源块的模块体积之和
	Residual         int64                `json:"residual"`           // TotalChunkBytes − AttributedBytes
	UnassignedBytes  int64                `json:"unassigned_bytes"`   // 不属于任何资源块的模块体积
	Unassigned       []string             `json:"unassigned"`         // 上述模块 ID，已排序
	Chunks           []ChunkAttribution   `json:"chunks"`             // 按 ChunkID 排序
	Dirs             []DirAttribution     `json:"dirs"`               // 按 Dir 排序
	Subtrees         []SubtreeAttribution `json:"subtrees"`           // 按 Root 排序
	Issues           []manifest.Issue     `json:"issues"`             // 输入异常，原样透传不静默丢弃
}

// ownerChunk 返回模块的归属资源块：其资源块列表中字典序最小者。
func ownerChunk(mod manifest.Module) (string, bool) {
	if len(mod.Chunks) == 0 {
		return "", false
	}
	min := mod.Chunks[0]
	for _, c := range mod.Chunks[1:] {
		if c < min {
			min = c
		}
	}
	return min, true
}

// Analyze 对清单做体积归因。清单中的异常（缺失模块、成环等）不阻断分析，
// 而是记录到 Report.Issues；指向不存在资源块的模块归属不计入任何块。
func Analyze(m *manifest.Manifest) (*Report, error) {
	issues := manifest.Validate(m)

	chunkByID := map[string]manifest.Chunk{}
	for _, ch := range m.Chunks {
		if _, ok := chunkByID[ch.ID]; !ok {
			chunkByID[ch.ID] = ch
		}
	}

	rep := &Report{Issues: issues}
	owned := map[string][]string{}   // chunkID -> module IDs
	sharedRefs := map[string]int64{} // chunkID -> 引用但未归属的共享模块体积
	sizeOf := map[string]int64{}
	unassigned := []string{}

	for _, mod := range m.Modules {
		sizeOf[mod.ID] = mod.Size
		rep.TotalModuleBytes += mod.Size
		owner, ok := ownerChunk(mod)
		if !ok {
			unassigned = append(unassigned, mod.ID)
			rep.UnassignedBytes += mod.Size
			continue
		}
		if _, exists := chunkByID[owner]; !exists {
			// 归属目标不存在：不计费，问题已在 Issues 中报告
			unassigned = append(unassigned, mod.ID)
			rep.UnassignedBytes += mod.Size
			continue
		}
		owned[owner] = append(owned[owner], mod.ID)
		// 记录本模块被其他块引用但未归属的体积（信息展示）
		for _, c := range mod.Chunks {
			if c != owner {
				if _, exists := chunkByID[c]; exists {
					sharedRefs[c] += mod.Size
				}
			}
		}
	}
	sort.Strings(unassigned)
	rep.Unassigned = unassigned

	chunkIDs := make([]string, 0, len(chunkByID))
	for id := range chunkByID {
		chunkIDs = append(chunkIDs, id)
	}
	sort.Strings(chunkIDs)
	for _, id := range chunkIDs {
		ch := chunkByID[id]
		mods := owned[id]
		sort.Strings(mods)
		var modBytes int64
		for _, mid := range mods {
			modBytes += sizeOf[mid]
		}
		ca := ChunkAttribution{
			ChunkID:      id,
			Kind:         ch.Kind,
			Total:        ch.Size,
			ModuleBytes:  modBytes,
			Residual:     ch.Size - modBytes,
			SharedBytes:  sharedRefs[id],
			OwnedModules: mods,
		}
		if ca.OwnedModules == nil {
			ca.OwnedModules = []string{}
		}
		rep.Chunks = append(rep.Chunks, ca)
		rep.TotalChunkBytes += ch.Size
		rep.AttributedBytes += modBytes
	}
	rep.Residual = rep.TotalChunkBytes - rep.AttributedBytes

	rep.Dirs = dirAttributions(m)
	rep.Subtrees = subtreeAttributions(m)
	return rep, nil
}

// dirAttributions 按目录聚合模块体积。Dir 为空时归入 "(root)"。
func dirAttributions(m *manifest.Manifest) []DirAttribution {
	byDir := map[string][]string{}
	sizeOf := map[string]int64{}
	for _, mod := range m.Modules {
		dir := mod.Dir
		if dir == "" {
			dir = "(root)"
		}
		byDir[dir] = append(byDir[dir], mod.ID)
		sizeOf[mod.ID] = mod.Size
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	out := make([]DirAttribution, 0, len(dirs))
	for _, d := range dirs {
		mods := byDir[d]
		sort.Strings(mods)
		var bytes int64
		for _, id := range mods {
			bytes += sizeOf[id]
		}
		out = append(out, DirAttribution{Dir: d, Bytes: bytes, Modules: mods})
	}
	return out
}

// subtreeAttributions 为每个模块计算依赖子树体积。
// 缺失的依赖目标被跳过（已由 Issues 报告）；环通过 visited 集合安全处理。
func subtreeAttributions(m *manifest.Manifest) []SubtreeAttribution {
	deps := map[string][]string{}
	sizeOf := map[string]int64{}
	ids := make([]string, 0, len(m.Modules))
	for _, mod := range m.Modules {
		deps[mod.ID] = mod.Deps
		sizeOf[mod.ID] = mod.Size
		ids = append(ids, mod.ID)
	}
	sort.Strings(ids)
	out := make([]SubtreeAttribution, 0, len(ids))
	for _, root := range ids {
		visited := map[string]bool{}
		stack := []string{root}
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if visited[id] {
				continue
			}
			visited[id] = true
			stack = append(stack, deps[id]...)
		}
		mods := make([]string, 0, len(visited))
		var bytes int64
		for id := range visited {
			mods = append(mods, id)
			bytes += sizeOf[id]
		}
		sort.Strings(mods)
		out = append(out, SubtreeAttribution{Root: root, Bytes: bytes, Modules: mods})
	}
	return out
}

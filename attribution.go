package analyzer

import "sort"

// ChunkAttribution 是单个资源块的归因结果。
type ChunkAttribution struct {
	ChunkID   string
	Bytes     int64 // 归属到该资源块的模块字节数（共享模块只计入一个属主）
	Residual  int64 // 资源块占用量中无法归因到任何模块的部分
	ModuleIDs []string
}

// Attribution 是一次完整的体积归因结果。
// 不变式：sum(Chunks[i].Bytes) + sum(Chunks[i].Residual) == TotalOccupied。
// 无法被任何资源块入口可达的模块计入 Unattributed*，不进入占用总量。
type Attribution struct {
	TotalOccupied       int64 // 所有资源块磁盘占用总量
	TotalModule         int64 // 全部模块字节总量（每个模块只计一次）
	TotalResidual       int64
	UnattributedBytes   int64    // 不可达模块的字节数
	UnattributedModules []string // 不可达模块 ID，升序
	Chunks              []ChunkAttribution
	Diagnostics         Diagnostics

	owner map[string]string // 模块 ID -> 属主资源块 ID（仅供内部视图复用）
}

// ComputeAttribution 计算体积归因。同一输入下输出完全确定：
// 所有列表按键升序，共享模块归属到达它的字典序最小资源块，
// 与清单中条目的书写顺序无关。
func ComputeAttribution(m *Manifest) *Attribution {
	g, diags := buildGraph(m)
	a := &Attribution{Diagnostics: diags, owner: make(map[string]string)}

	// 资源块按 ID 升序处理，保证属主选择确定。
	chunks := append([]Chunk(nil), m.Chunks...)
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].ID < chunks[j].ID })

	// 去重：重复资源块以先出现者为准，记录诊断，不重复归因。
	seenChunk := make(map[string]bool, len(chunks))
	var unique []Chunk
	for _, c := range chunks {
		if seenChunk[c.ID] {
			a.Diagnostics = append(a.Diagnostics, Diagnostic{
				Kind:    DiagDuplicateChunk,
				Chunk:   c.ID,
				Message: "duplicate chunk id, first occurrence kept",
			})
			continue
		}
		seenChunk[c.ID] = true
		unique = append(unique, c)
	}

	// 属主判定：按资源块 ID 升序，先到先得（即字典序最小者）。
	attributed := make(map[string]bool, len(g.sorted))
	for _, c := range unique {
		a.TotalOccupied += c.Size
		for _, e := range c.Entries {
			if _, ok := g.modules[e]; !ok {
				a.Diagnostics = append(a.Diagnostics, Diagnostic{
					Kind:    DiagMissingEntry,
					Chunk:   c.ID,
					Module:  e,
					Message: "chunk entry references missing module",
				})
			}
		}
		ca := ChunkAttribution{ChunkID: c.ID}
		for _, mid := range g.reachableFrom(c.Entries) {
			if attributed[mid] {
				continue // 共享模块只计入一次
			}
			attributed[mid] = true
			a.owner[mid] = c.ID
			ca.Bytes += g.modules[mid].Size
			ca.ModuleIDs = append(ca.ModuleIDs, mid)
		}
		ca.Residual = c.Size - ca.Bytes
		a.TotalResidual += ca.Residual
		a.Chunks = append(a.Chunks, ca)
	}

	// 全部模块字节（每个一次）与不可达模块。
	for _, id := range g.sorted {
		a.TotalModule += g.modules[id].Size
		if !attributed[id] {
			a.UnattributedBytes += g.modules[id].Size
			a.UnattributedModules = append(a.UnattributedModules, id)
		}
	}
	return a
}

// DirEntry 是按目录维度聚合的一条记录。
type DirEntry struct {
	Dir   string
	Bytes int64
}

// ByDirectory 按模块目录聚合归因字节数，每个模块只计一次，
// 结果按目录名升序。
func (a *Attribution) ByDirectory(m *Manifest) []DirEntry {
	totals := make(map[string]int64)
	for _, mod := range m.Modules {
		totals[moduleDir(mod)] += mod.Size
	}
	out := make([]DirEntry, 0, len(totals))
	for dir, b := range totals {
		out = append(out, DirEntry{Dir: dir, Bytes: b})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out
}

// Subtree 返回以 moduleID 为根的依赖子树去重后的总字节数与模块列表（升序）。
// 根模块不存在时返回诊断而非静默给出零值。
func (a *Attribution) Subtree(m *Manifest, moduleID string) (int64, []string, Diagnostics) {
	g, diags := buildGraph(m)
	if _, ok := g.modules[moduleID]; !ok {
		return 0, nil, append(diags, Diagnostic{
			Kind:    DiagMissingModule,
			Module:  moduleID,
			Message: "subtree root is not a known module",
		})
	}
	mods := g.reachableFrom([]string{moduleID})
	var total int64
	for _, id := range mods {
		total += g.modules[id].Size
	}
	return total, mods, diags
}

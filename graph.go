package analyzer

import "sort"

// graph 是模块引用图，提供确定顺序的遍历与环检测。
// 邻接表按键排序，所有遍历结果与输入顺序无关。
type graph struct {
	modules map[string]*Module  // 模块 ID -> 模块
	sorted  []string            // 升序排列的模块 ID
	edges   map[string][]string // 模块 ID -> 升序去重后的依赖（仅含存在的模块）
}

// buildGraph 由清单构建引用图，并收集重复定义与缺失引用等诊断。
// 缺失的依赖不会被静默丢弃：记录诊断后从邻接表中剔除，
// 保证剩余图的遍历结果确定。
func buildGraph(m *Manifest) (*graph, Diagnostics) {
	g := &graph{
		modules: make(map[string]*Module, len(m.Modules)),
		edges:   make(map[string][]string, len(m.Modules)),
	}
	var diags Diagnostics

	// 第一遍：登记模块，重复 ID 以先出现者为准并记录诊断。
	for i := range m.Modules {
		mod := &m.Modules[i]
		if _, dup := g.modules[mod.ID]; dup {
			diags = append(diags, Diagnostic{
				Kind:    DiagDuplicateMod,
				Module:  mod.ID,
				Message: "duplicate module id, first occurrence kept",
			})
			continue
		}
		g.modules[mod.ID] = mod
		g.sorted = append(g.sorted, mod.ID)
	}
	sort.Strings(g.sorted)

	// 第二遍：建边，缺失依赖记录诊断并剔除，邻接表去重排序。
	for _, id := range g.sorted {
		mod := g.modules[id]
		seen := make(map[string]bool, len(mod.Deps))
		var deps []string
		for _, dep := range mod.Deps {
			if _, ok := g.modules[dep]; !ok {
				diags = append(diags, Diagnostic{
					Kind:    DiagMissingModule,
					Module:  id,
					Message: "module references missing module " + dep,
				})
				continue
			}
			if seen[dep] {
				continue // 重复引用不得重复计费：邻接表去重
			}
			seen[dep] = true
			deps = append(deps, dep)
		}
		sort.Strings(deps)
		g.edges[id] = deps
	}

	// 第三遍：环检测（确定顺序的迭代式 DFS）。
	diags = append(diags, g.detectCycles()...)
	return g, diags
}

// detectCycles 以确定顺序找出所有环。每个环只报告一次，
// 以环上字典序最小的节点作为代表。
func (g *graph) detectCycles() Diagnostics {
	const (
		white = 0 // 未访问
		gray  = 1 // 在当前栈上
		black = 2 // 已完成
	)
	color := make(map[string]int, len(g.sorted))
	reported := make(map[string]bool) // 环代表节点 -> 已报告
	var diags Diagnostics

	for _, start := range g.sorted {
		if color[start] != white {
			continue
		}
		// 迭代式 DFS，栈中保存 (节点, 下一个子节点下标)。
		type frame struct {
			node string
			next int
		}
		stack := []frame{{node: start}}
		color[start] = gray
		var path []string // 当前栈上的节点，与 stack 同步
		path = append(path, start)

		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			deps := g.edges[top.node]
			if top.next < len(deps) {
				child := deps[top.next]
				top.next++
				switch color[child] {
				case white:
					color[child] = gray
					stack = append(stack, frame{node: child})
					path = append(path, child)
				case gray:
					// 找到环：path 中 child 到栈顶的部分。
					idx := 0
					for i, n := range path {
						if n == child {
							idx = i
							break
						}
					}
					cycle := append([]string(nil), path[idx:]...)
					rep := cycle[0]
					for _, n := range cycle[1:] {
						if n < rep {
							rep = n
						}
					}
					if !reported[rep] {
						reported[rep] = true
						diags = append(diags, Diagnostic{
							Kind:    DiagCycle,
							Module:  rep,
							Path:    append(cycle, child),
							Message: "dependency cycle detected",
						})
					}
				}
			} else {
				color[top.node] = black
				stack = stack[:len(stack)-1]
				path = path[:len(path)-1]
			}
		}
	}
	return diags
}

// reachableFrom 返回从 roots 可达的全部模块（含 roots 中存在的部分），
// 结果升序排列。环不会导致死循环。
func (g *graph) reachableFrom(roots []string) []string {
	visited := make(map[string]bool)
	var stack []string
	for _, r := range roots {
		if _, ok := g.modules[r]; ok && !visited[r] {
			visited[r] = true
			stack = append(stack, r)
		}
	}
	var out []string
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		out = append(out, n)
		// 逆序压栈保持弹出顺序确定（此处结果最终会排序，顺序仅影响性能）。
		deps := g.edges[n]
		for i := len(deps) - 1; i >= 0; i-- {
			if !visited[deps[i]] {
				visited[deps[i]] = true
				stack = append(stack, deps[i])
			}
		}
	}
	sort.Strings(out)
	return out
}

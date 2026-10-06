package analyzer

// DiagKind 区分异常输入的类别。
type DiagKind string

const (
	DiagMissingModule  DiagKind = "missing_module"  // 引用了不存在的模块
	DiagMissingEntry   DiagKind = "missing_entry"   // 资源块入口指向不存在的模块
	DiagCycle          DiagKind = "cycle"           // 模块引用成环
	DiagDuplicateChunk DiagKind = "duplicate_chunk" // 资源块 ID 重复
	DiagDuplicateMod   DiagKind = "duplicate_module"
)

// Diagnostic 定位一处异常输入，附带上文信息。
type Diagnostic struct {
	Kind    DiagKind
	Chunk   string   // 相关资源块（可空）
	Module  string   // 相关模块（可空）
	Path    []string // 成环时的引用路径（可空）
	Message string
}

// Diagnostics 是按确定顺序排列的诊断列表。
type Diagnostics []Diagnostic

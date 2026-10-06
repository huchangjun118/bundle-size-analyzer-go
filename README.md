# bundle-size-analyzer-go

构建产物的体积归因分析与传输预算门禁。整条链路只在本进程内运行，不依赖任何外部服务，同一输入下输出完全确定。

## 清单格式

`ParseManifest` 自动识别 v1 / v2，未知字段被忽略，**新增字段不改变已有字段的归因结果**。

```jsonc
{
  "version": 2,                 // 缺省视为 1
  "chunks": [
    {
      "id": "app",              // 资源块标识（必填）
      "file": "dist/app.js",    // 产物文件
      "size": 102400,           // 磁盘占用字节数
      "kind": "js",             // v2+：资源类别，缺省由扩展名推导
      "entries": ["src/main.js"]// 入口模块
    }
  ],
  "modules": [
    {
      "id": "src/main.js",
      "size": 4096,
      "deps": ["src/lib.js"],   // 模块引用关系
      "dir": "src"              // v2+：显式目录，缺省由 id 推导
    }
  ]
}
```

## 归因语义

`ComputeAttribution` 的不变式：

```
sum(chunk.Bytes) + sum(chunk.Residual) == TotalOccupied
```

- **共享模块只计一次**：被多个资源块可达的模块，归属到达它的**字典序最小**的资源块，重复引用（邻接表已去重）不重复计费。
- **残差（Residual）**：资源块磁盘占用中无法归因到任何模块的部分（运行时、包装代码等），逐块给出。
- **不可达模块**：不被任何入口可达的模块计入 `UnattributedBytes` / `UnattributedModules`，不进入占用总量。
- **确定性**：所有输出列表按键升序，与清单书写顺序、遍历顺序无关。

多维度视图：

- `Attribution.Chunks`：按资源块。
- `Attribution.ByDirectory(m)`：按模块目录聚合，每个模块只计一次。
- `Attribution.Subtree(m, id)`：以某模块为根的依赖子树去重总量。

## 差异类别

`ComputeDiff(base, manifest, attribution)` 对比基线快照与新产物，模块与资源块两个维度各给出四类差异，条目按 ID 升序，相同输入完全可复现：

| 类别 | 含义 |
|---|---|
| `added` | 基线不存在、新产物存在 |
| `removed` | 基线存在、新产物不存在 |
| `increased` | 两侧都存在且字节数增大 |
| `decreased` | 两侧都存在且字节数减小 |

模块差异附带其属主资源块（`Chunks` 字段），可定位到具体模块与资源块。

## 预算配置

`CheckBudget(m, attribution, rules)` 逐条判定，任一规则超限则 `Report.Passed == false`，应阻断合并。

```go
rules := []analyzer.BudgetRule{
    {Name: "total",  Scope: analyzer.ScopeTotal, Limit: 500_000},
    {Name: "app",    Scope: analyzer.ScopeChunk, Target: "app", Limit: 150_000},
    {Name: "styles", Scope: analyzer.ScopeKind,  Target: "css", Limit: 50_000},
}
```

- 作用范围：`total`（总体占用）、`chunk`（单个资源块归因字节）、`kind`（某类资源归因字节之和）。
- 判定结果：`pass` / `fail`（附 `Actual` 与 `OverBy` 明细）/ `unknown_target`（规则指向不存在的目标，可区分但不阻断）。
- 规则先按 `(Scope, Target, Name)` 排序再求值，命中与阈值判断与传入顺序无关。

## 传输估算

`EstimateTransfer(name, reader, config)` 分别给出压缩前（`RawBytes`）与压缩后（`TransferBytes`）体积。算法支持 `none` / `gzip` / `flate` / `zlib`，档位 `Level` 可配（0 为算法默认）。估算以固定缓冲流式进行，任何时刻内存占用与输入大小无关。

## 异常输入

异常不会被静默丢弃，而是以 `Diagnostic` 定位并区分原因：

| 类别 | 含义 |
|---|---|
| `missing_module` | 模块引用了不存在的模块 |
| `missing_entry` | 资源块入口指向不存在的模块 |
| `cycle` | 模块引用成环（`Path` 记录环路径，环上模块仍只归因一次） |
| `duplicate_chunk` / `duplicate_module` | ID 重复，先出现者为准 |

## 并发

`TaskManager` 以固定数量 worker 执行任务：每个任务维护独立进度（`Progress`），队列有界——慢任务占满队列后新提交收到 `ErrQueueFull` 而非无限堆积；`Cancel` 可取消单个任务；`Close` 等待所有任务完成并退出全部 worker，不留后台 goroutine。

## 测试

```sh
go test -race ./...
```

覆盖共享模块归因、增量差异、预算判定、压缩估算、异常输入与并发场景。

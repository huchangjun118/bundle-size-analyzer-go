# bundle-size-analyzer-go

构建产物的体积归因分析与传输预算门禁工具。整条链路只在本进程内运行，
不依赖任何外部服务；同一份输入下所有输出完全确定（条目顺序与数值不受遍历顺序影响）。

## 构建与测试

```sh
go build ./...
go test ./...        # 并发场景建议 go test -race ./internal/tasks/
```

## CLI

```sh
bsa analyze <manifest.json>                # 体积归因报告（JSON）
bsa estimate [-alg gzip] [-level 6] <file> # 传输体积估算（流式）
bsa diff <base.json> <next.json>           # 增量对比
bsa budget <manifest.json> <rules.json>    # 预算判定，超限退出码为 2
```

## 产物清单格式

当前支持 version 1 与 version 2。解析时忽略未知字段，
因此**新增字段不会改变已有字段的归因结果**（有测试锁定该行为）。

```jsonc
{
  "version": 2,
  "chunks": [
    // v1: id/size/files；v2 新增 kind（资源类别，如 js/css/asset）
    {"id": "app", "size": 500, "files": ["app.js"], "kind": "js"}
  ],
  "modules": [
    // v1: id/size/deps/chunks；v2 新增 dir（目录维度）
    {"id": "src/a.js", "size": 250, "chunks": ["app"],
     "deps": ["src/b.js"], "dir": "src"}
  ]
}
```

- `chunk.size`：资源块（产物文件）的实际字节数，即“占用总量”的来源。
- `module.size`：模块体积；`deps` 为模块引用关系，`chunks` 为所属资源块。

## 归因语义

1. **共享模块只计一次**：被多个资源块引用的模块，归属于其资源块列表中
   ID 字典序最小的块（确定性归属规则）。因此各资源块归因之和恒等于
   已归属模块体积之和，重复引用不会重复计费。其他块对该模块的引用体积
   以 `shared_bytes` 信息字段展示，不参与计费。
2. **残差明确**：每个资源块的 `residual = chunk.size − 归属模块体积之和`，
   表示清单未能解释的部分（如运行时胶水代码），允许为负并如实上报。
   报告级 `residual = total_chunk_bytes − attributed_bytes`，
   恒有 `attributed_bytes + residual == total_chunk_bytes`。
3. **无法归属的模块**：不属于任何资源块（或所属资源块不存在）的模块列入
   `unassigned` 并汇总为 `unassigned_bytes`，不计入任何资源块，也不静默丢弃。
4. **三个维度**：
   - `chunks`：按资源块（含 owned_modules 明细）；
   - `dirs`：按目录聚合（`dir` 为空归入 `(root)`）；
   - `subtrees`：按依赖子树，根模块及其全部传递依赖，子树内部去重，
     每个模块只计一次；成环引用通过 visited 集合安全处理。

## 差异类别

`bsa diff` 在模块与资源块两个粒度上给出四类差异，体积不变的条目不出现：

| 类别      | 含义                     | delta         |
|-----------|--------------------------|---------------|
| `added`   | 新产物中新增             | `+next_size`  |
| `removed` | 相对基线被删除           | `−base_size`  |
| `grown`   | 两侧都存在且体积增大     | `next − base` |
| `shrunk`  | 两侧都存在且体积减小     | `next − base` |

每条记录含 `base_size`/`next_size`/`delta`，可定位到具体模块与资源块；
全部条目按 ID 排序，相同输入下结果完全可复现。

## 传输体积估算

`estimate` 分别给出压缩前（`raw`）与压缩后（`compressed`）字节数。
算法可选 `none` / `gzip` / `deflate`，`level` 取 1(最快)..9(最佳)，
0 表示算法默认档位。估算全程流式进行（边读边压缩边计数），
不会为得到数字把整份内容读入内存。

## 预算配置

规则文件为 JSON 数组，每条规则作用于一个范围：

```json
[
  {"name": "total-cap", "scope": "total", "limit": 1048576},
  {"name": "app-cap",   "scope": "chunk", "target": "app", "limit": 512000},
  {"name": "js-cap",    "scope": "kind",  "target": "js",  "limit": 800000}
]
```

- `total`：全部资源块字节总量；`chunk`：单个资源块（`target` 为块 ID）；
  `kind`：某一类资源（`target` 为 chunk 的 `kind`）。
- 判定为 `actual <= limit` 通过；超限记录 `over_by` 明细。
- 输出包含每条规则的评估明细（`evaluations`），任一规则未通过则整体
  `passed=false`，CLI 以退出码 2 阻断合并。目标不存在时 `found=false`
  且实际值按 0 计。判定完全确定：同一报告与规则列表必然得到同一结果。

## 异常输入

`manifest.Validate`（结果透传至归因报告的 `issues` 字段）定位并区分：

| kind                   | 含义                             |
|------------------------|----------------------------------|
| `missing_module`       | 依赖指向不存在的模块             |
| `missing_chunk`        | 模块指向不存在的资源块           |
| `dependency_cycle`     | 模块引用成环（环表示已规范化）   |
| `duplicate_module_id`  | 模块 ID 重复                     |
| `duplicate_chunk_id`   | 资源块 ID 重复                   |
| `negative_size`        | 体积为负                         |

异常不阻断分析，也不静默丢弃：问题以 `issues` 显式上报，
正常部分的归因结果不受影响。

## 并发模型

`internal/tasks` 提供任务管理器：多个分析/对比任务并发运行，
各自维护独立进度（`Progress(id)`）；并发度由信号量上限控制，
单个慢任务只占用一个额度，不会阻塞其他任务。已结束任务记录有保留
上限（FIFO 淘汰，默认 1024），长时间运行不会内存无界增长；
`Close` 等待全部任务 goroutine 退出，不产生后台任务泄漏。

## 目录结构

```
cmd/bsa/               CLI 入口
internal/manifest/     清单解析（v1/v2 兼容）与校验
internal/attribution/  体积归因（块/目录/依赖子树三维度）
internal/estimate/     流式传输体积估算
internal/diff/         基线增量对比
internal/budget/       预算规则与判定
internal/tasks/        并发任务管理
```

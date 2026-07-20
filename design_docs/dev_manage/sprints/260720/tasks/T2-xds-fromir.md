# T2：deliver/xds FromIR 纯函数装配 + 依赖引入

- **状态**: 未开始
- **依赖**: T1（仅文档对齐；代码只依赖 IR 契约，可先行）
- **验收锚点**: requirements A1

## 目标

实现 `internal/deliver/xds` 包的 Snapshot 装配纯函数 `FromIR`，把 IR 五类资源映射为 go-control-plane `cache.Snapshot`，含 `Consistent()` 引用闭合自检（下发层 §2.1）。本 task 同时完成 go-control-plane 根模块依赖引入（单独 commit）。

## 上游设计引用

- [下发层设计](../../../../system_design/260717-1-deliver-layer-design.md) §2.1（组件与装配、映射表、Consistent 双保险）
- [编译层设计](../../../../system_design/260716-2-compile-ir-design.md) §7（IR 契约）、§5（命名规约与版本）
- 冲刺 [technical_design.md](../technical_design.md) SD1（依赖版本）、§3 冻结签名
- 现有代码：`internal/ir/ir.go`（IR 五类资源）、`internal/deliver/snapshot.go`（M0 的 SnapshotJSON，仅供形态参考，不改动）

## 执行步骤

1. **依赖引入（独立 commit）**：`go get github.com/envoyproxy/go-control-plane@v0.14.0`（根模块，提供 `pkg/cache/v3` 等；`envoy` 子模块保持 v1.37.0，MVS 自动提升根模块声明的 v1.36.0——SD1 已实测兼容）。`go mod tidy` 后跑 `make build test lint`；licenses 检查（CI go-licenses job 同源）若报错当步解决。commit message 注明配对理由。
2. **核对实际 API**（重要）：v0.14 的 `cache.NewSnapshot` 签名、`Snapshot.Consistent()`、`resource.Type` 常量与设计文档示例（v0.13 时代）可能有出入。以 `$(go env GOMODCACHE)` 中的实际源码为准；出入点记入进展记录，但「纯函数装配 + Consistent 自检」语义不得偏离。
3. 实现 `internal/deliver/xds/snapshot.go`：

   ```go
   // FromIR 把 IR 装配为 xDS Snapshot（纯函数，无 IO、无全局状态）。
   // version = ir.Version（全类型共用，下发层 §2.2 规则 2）。
   // 失败即 Apply 同步报错（stage=assemble），非法 snapshot 不出管理面。
   func FromIR(i *ir.IR) (*cache.Snapshot, error)
   ```

   映射（§2.1 表）：Endpoints→EDS、Clusters→CDS、Routes→RDS、Listeners→LDS、Secrets→SDS；`IR.Bootstrap` 不下发。`i == nil` 或 `i.Version == ""` 报错。
4. 表驱动单测：
   - 正例：用 `internal/compile` 编译 `testdata/s1/input`（ModeXDS）得到真实 IR，FromIR 成功；逐类型断言资源名与数量（lis/*、rc/*、us/*、CLA、crt/*）；`Consistent()` 通过；version == IR.Version。
   - 反例（构造性）：手工造 IR——Route 引用不存在的 cluster、Listener RDS 引用不存在的 routeConfig、Secret 引用缺失等（至少覆盖「RDS/EDS/SDS 引用不闭合」三类），断言 Consistent 报错且错误信息含可定位信息。
   - 纯函数性：同一 IR 两次调用互不影响（修改返回的 snapshot 不污染第二次结果，按 go-control-plane 语义核对后写明结论）。
5. `make build test lint` 全绿。commit scope：`xds`（依赖）+ `xds`（FromIR），至少两个 commit。

## 验收标准

- A1：映射正确、Consistent 正例通过、三类不闭合反例报错；
- 依赖引入独立 commit，`go.mod` 新增根模块为直接依赖。

## 进展记录

| 日期 | 记录 |
|---|---|
| 2026-07-20 | task 创建 |

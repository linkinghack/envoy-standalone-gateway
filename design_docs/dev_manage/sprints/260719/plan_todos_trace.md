# Sprint 260719 任务拆分与进展追踪

> **给接手会话的指引（Codex / Claude Code 必读）**
> 1. 先读本文件确定当前进行到哪个 task；再读 `tasks/<ID>.md` 获取该 task 的详细步骤与已记录进展。
> 2. 开工前把对应 task 状态改为 `进行中`，完成后改为 `已完成` 并填写完成摘要；**每次会话结束前必须回写进展**（做到哪一步、遇到什么问题、下一步是什么）。
> 3. 遵守 [`technical_design.md`](technical_design.md) §3 的接口冻结点；需要变更接口时先更新该文件并在此处记录。
> 4. 每个 task 至少一个独立 commit，提交信息格式见[工程基线](../../dev_design/260719-1-engineering-baseline.md) §5。
> 5. 冲刺内设计决议（C1/C3/C4）落地后，回写上游设计文档的未决事项表。

## 任务总览

| ID | 任务 | 状态 | 依赖 | 验收锚点 |
|---|---|---|---|---|
| T1 | 仓库工程基线（module/目录/Makefile/lint/CI） | 已完成 | — | A8 |
| T2 | internal/protocol：类型、strict decode、loader、defaults、JSON Schema | 已完成 | T1 | A1(输入侧)、C3 |
| T3 | internal/compile F2：链接与语义校验 + CompileError 模型 | 已完成 | T2 | A5 |
| T4 | internal/compile F3：四类 Builder 与策略映射 | 未开始 | T3 | A1、C4 |
| T5 | internal/compile F4/F5/F6 + IR/哈希/SourceMap | 未开始 | T4 | A4、A6、C1 |
| T6 | static 渲染 + F7 envoycheck + `esgw compile` CLI | 未开始 | T5 | A2、A7 |
| T7 | golden/e2e 测试设施与 CI 集成收口 | 未开始 | T6（golden 可提前随 T4/T5 增量补） | A1~A3、A8 |

状态枚举：`未开始` / `进行中` / `已完成` / `受阻(附原因)`

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-19 | 冲刺创建：需求、技术设计、7 个 task 文档就绪；等待 T1 开工 |
| 2026-07-19 | T1 完成（commit `2adb7b1`）：Go module/骨架/Makefile/golangci-lint/CI/go-licenses；Envoy 支持区间定 1.37~1.39；T2 开工 |
| 2026-07-19 | T2 完成（commits `6b76692`/`39facc0`/`4f0ba48`/`2b4e4de`）：internal/protocol 全部类型+strict decode loader+defaults+JSON Schema；57 用例全绿；C3 决议落地（见决议记录与 T2 进展记录，含 SD2 YAML 库偏离说明）；T3 开工 |
| 2026-07-19 | T3 完成（commits `86333d5`/`2b3714a`/`feda68a`）：CompileError/SourceRef 错误模型、F2 链接与语义校验全规则、Compile() 骨架（F3+ 占位）、EndpointSource 接口定义；33 用例全绿，S1/S2 过 F2 零错误；ipAccess 层级等 6 条设计澄清见 T3 进展记录；T4 开工 |

## 决议记录（冲刺内产生的设计决策）

| 未决项 | 决议 | 日期 | 已回写文档 |
|---|---|---|---|
| C1 | （待定） | | |
| C3 | 采用 invopop/jsonschema 由 Go 类型生成：协议 §6 要求「Schema 由协议 Go 类型生成，单一事实来源」，反射输出对枚举/oneOf/Duration 经 `JSONSchema()` 类型钩子（PolicyAttachment union、Duration pattern、RawJSON 任意、枚举值）与后置注入（每文档 apiVersion/kind 常量、name pattern）后质量达标，bundle = oneOf 六文档 + 共享 $defs；用 santhosh-tekuri/jsonschema/v6 测试加载与正/反校验。手写方案被淘汰：双份真源难维护 | 2026-07-19 | plan_todos_trace（本表）、compile-ir-design §9、T2 进展记录 |
| C4 | （待定） | | |

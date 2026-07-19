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
| T1 | 仓库工程基线（module/目录/Makefile/lint/CI） | 未开始 | — | A8 |
| T2 | internal/protocol：类型、strict decode、loader、defaults、JSON Schema | 未开始 | T1 | A1(输入侧)、C3 |
| T3 | internal/compile F2：链接与语义校验 + CompileError 模型 | 未开始 | T2 | A5 |
| T4 | internal/compile F3：四类 Builder 与策略映射 | 未开始 | T3 | A1、C4 |
| T5 | internal/compile F4/F5/F6 + IR/哈希/SourceMap | 未开始 | T4 | A4、A6、C1 |
| T6 | static 渲染 + F7 envoycheck + `esgw compile` CLI | 未开始 | T5 | A2、A7 |
| T7 | golden/e2e 测试设施与 CI 集成收口 | 未开始 | T6（golden 可提前随 T4/T5 增量补） | A1~A3、A8 |

状态枚举：`未开始` / `进行中` / `已完成` / `受阻(附原因)`

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-19 | 冲刺创建：需求、技术设计、7 个 task 文档就绪；等待 T1 开工 |

## 决议记录（冲刺内产生的设计决策）

| 未决项 | 决议 | 日期 | 已回写文档 |
|---|---|---|---|
| C1 | （待定） | | |
| C3 | （待定） | | |
| C4 | （待定） | | |

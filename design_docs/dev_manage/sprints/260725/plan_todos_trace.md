# Sprint 260725 任务拆分与进展追踪

| ID | 任务 | 状态 | 验收 |
|---|---|---|---|
| T1 | proc/static 配置、Envoy 发现与版本检查 | 已完成 | strict config + discover tests |
| T2 | 进程记录、spawn/接管、退出与退避 | 进行中 | supervisor fault tests |
| T3 | static 原子下发与 hot restart epoch | 待开始 | writer/restart invariant tests |
| T4 | 四组合 M-CORE 装配与恢复 | 待开始 | composition/restart tests |
| T5 | 真实 Envoy e2e、DL 实测与冲刺收口 | 待开始 | static hot restart + full gates |

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-22 | S6 收口后启动 S7；复核下发层 §4~§6 和 M1 实施计划，冻结 DL1/DL4/DL6、最小权限托管默认及事务边界，T1 开始。 |
| 2026-07-22 | T1 完成：static/proc 配置 schema、安全时序约束、发现优先级与 Envoy 版本区间门禁通过单测；T2 开始。 |

## 验收核验

| ID | 状态 | 证据 |
|---|---|---|
| A1 发现/版本 | 通过 | `internal/config` + `internal/proc` 表驱动单测 |
| A2 接管/不杀数据面 | 待核验 | — |
| A3 退避/degraded | 待核验 | — |
| A4 static last-good/原子写 | 待核验 | — |
| A5 hot restart 不变量 | 待核验 | — |
| A6 仅下发不越权 | 待核验 | — |
| A7 四组合/恢复 | 待核验 | — |
| A8 真实 e2e/质量门禁 | 待核验 | — |

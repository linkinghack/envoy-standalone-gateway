# Sprint 260728 任务拆分与进展追踪

| ID | 任务 | 状态 | 验收 |
|---|---|---|---|
| T1 | 发布确认、superseded、timeout recheck 与异步回滚不变量 | 待开始 | store/conf unit + state integration |
| T2 | 双视角 preview、publish run 与版本 API/OpenAPI | 待开始 | contract + API integration |
| T3 | JSON Schema 表单与 YAML 无损双向编辑 | 待开始 | component/API round-trip |
| T4 | Releases 发布流、历史/diff/compiled/回滚 UI | 待开始 | desktop/mobile interaction |
| T5 | Route stat prefix 与统计查询语义 | 待开始 | proto/unit + real Envoy stats |
| T6 | route/cluster 统计控制台 | 待开始 | metrics UX + responsive tests |
| T7 | 跨模块 e2e、全门禁与 S10 收口 | 待开始 | A1–A8 + clean diff |

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-23 | S9 A1–A8 关闭后启动 S10；审计确认版本/状态 API 骨架可复用，但发布确认、双 diff、表单双向、统计维度与控制台均未达到 P1 完成口径；冻结七任务与不降级边界，T1 开始。 |

## 验收核验

| ID | 状态 | 证据 |
|---|---|---|
| A1 发布状态机 | 待核验 | — |
| A2 preview/双 diff | 待核验 | — |
| A3 表单↔YAML | 待核验 | — |
| A4 版本/回滚 | 待核验 | — |
| A5 route/cluster stats | 待核验 | — |
| A6 统计控制台 | 待核验 | — |
| A7 真实跨模块 e2e | 待核验 | — |
| A8 全量门禁 | 待核验 | — |

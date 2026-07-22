# T6：S4 状态采集前置骨架

## 目标

提前落地 M-STATE 的安全只读边界与最小版本确认能力，使 S3 发布状态机具备 `CONFIRMING → EFFECTIVE` 的集成契约。

## 执行步骤

1. 实现 UDS/TCP Envoy admin client 与 GET 路径白名单；
2. 实现 Prometheus 流式透传；
3. 解析 config dump 的版本与 listener/cluster 基础状态；
4. 实现期望版本快速确认与有界内存 SeriesStore；
5. 将完整调度、归一化和并发质量审计移交 Sprint 260722。

## 验收记录

- 2026-07-20：基础实现随 commit `6e32f3c` 落地于 `internal/state`。
- 2026-07-22：S4 已补齐骨架后续缺口；本任务作为 S3/S4 交界任务完成，最终证据见 [`../../260722/plan_todos_trace.md`](../../260722/plan_todos_trace.md)。

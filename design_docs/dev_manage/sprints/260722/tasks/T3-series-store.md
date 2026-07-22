# T3：时间序列环形缓冲与 S4 基础收口

## 目标

提供进程内有界统计序列，避免状态采集因时间或指标基数无界增长。

## 执行步骤

1. 为每个维度/名称/指标组合实现固定容量环形缓冲；
2. 限制全局 series 基数，超限返回明确错误；
3. 提供按 dimension/name/metric 的组合过滤查询；
4. 接入 stats 周期采集并覆盖并发访问。

## 验收记录

- 2026-07-20：实现落地于 `internal/state/series.go`，随 S4 基础 commit 交付。
- 2026-07-22：`TestSeriesStoreRingAndCardinality` 及全量 `go test ./...` 通过。任务完成。

# T4：发布运行状态机基础

## 目标

把第一阶段的“草稿→编译→快照→下发”串成可被后续 M-STATE/M-API
消费的最小发布运行记录：持久化 `publish_runs`、支持 `baseHash` 乐观并发，
并区分下发受理（`CONFIRMING`）与状态采集确认（`EFFECTIVE`）。

## 实现

- `internal/store` 增加 `PublishRun` 读写 API，并为既有数据库补充
  `version_seq`、`trigger_by` 列；
- `internal/conf.Publisher.PublishWithBase` 推进
  `VALIDATING → VALIDATED → PUBLISHING → CONFIRMING`；
- `Publisher.Confirm` 校验观测到的 IR 版本后推进 `EFFECTIVE`；
- 保留 `Publish` 兼容入口，立即调用 `Confirm`，不改变既有调用方行为；
- 发布器进程内串行，数据库部分唯一索引继续作为跨实例兜底；
- 新增生命周期、版本确认和 baseHash 冲突单测。

## 验收

`make build test lint` 全部通过。


# T2：状态快照与版本确认快速通道

## 目标

从 Envoy admin 数据构建归一化状态快照，并对发布版本执行 `CONFIRMED`/`TIMEOUT` 生效确认。

## 执行步骤

1. 解析 `/server_info`、`/config_dump?include_eds` 与 `/clusters?format=json`；
2. 实现 listener/route/cluster/endpoint/certificate 归一化与配置对象归属反解；
3. 实现 `ExpectVersion` 轮询、指数间隔与事件订阅；
4. 排除没有稳定 `version_info` 的 EDS 对确认判断的干扰；
5. 失败时保留最后成功快照并标记 `Stale`；
6. 与 `Publisher.AttachState` 联调自动推进发布状态。

## 验收记录

- 2026-07-20：基础能力 commit `2dc9ed9`；归一化、超时、自动确认等缺口由 `1d90053`、`c3c79f2` 补齐。
- 2026-07-22：`TestServiceConfirmsConfigDumpVersion`、`TestServiceTimesOutVersionConfirmation`、`TestServiceRetainsStaleSnapshot`、`TestPublisherAutoConfirmsFromStateEvent` 通过。任务完成。

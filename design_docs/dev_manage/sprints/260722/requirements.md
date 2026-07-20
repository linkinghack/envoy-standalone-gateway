# Sprint 260722 需求：状态采集与生效确认

## 目标

实现 M-STATE 只读状态采集、Envoy admin API 安全访问、xDS 版本确认快速通道和内存统计序列基础能力，打通 S3 发布流的 `CONFIRMING → EFFECTIVE`。

## 范围

- UDS/TCP admin client，GET 路径白名单与 Prometheus 流式透传；
- `/server_info`、`/config_dump?include_eds`、`/clusters?format=json` 采集与归一化；
- `ExpectVersion` 确认轮询、CONFIRMED/TIMEOUT 事件；
- Listener/Cluster 基础归属反解；
- 有界内存时间序列环形缓冲；
- 与 `Publisher.Confirm` 联调契约和单测。

## 非范围

- 管理 REST API、鉴权会话；
- static hot restart/M-PROC；
- 多节点、完整 stats 解析和 UI。

## 验收

- `make build test lint` 全绿；
- admin 写端点不可访问；
- config_dump 版本一致时产生 CONFIRMED，超时产生 TIMEOUT；
- 管理面不可达时保留上次快照并标记 Stale。

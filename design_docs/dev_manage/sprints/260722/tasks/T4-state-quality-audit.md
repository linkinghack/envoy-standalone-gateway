# T4：S4 完整性审计与状态采集补齐

## 目标

对 M-STATE 基础骨架进行契约级验证，避免“构建和少量单测全绿”被误判为设计项全部完成；补齐状态采集设计中尚未落地的功能和测试。

## 已完成

- admin GET 白名单、HTTP 状态码、Prometheus 流式透传测试；
- UDS/TCP client 错误路径与 JSON 解码测试；
- SeriesStore 环形淘汰、基数上限测试；
- `CONFIRMED`/`TIMEOUT` 事件测试；
- listener/cluster owner 反解和 endpoint 地址、健康、权重、失败标记解析；
- 周期采集与 `ready`、`stats`（含 histogram quantiles）、`certs`、routes 归一化；
- EDS 版本从确认一致性判断中排除；
- `Publisher.AttachState` 自动消费确认事件并推进发布状态；
- admin 请求串行化、按端点 singleflight、指数失败退避、启动期 ready 探测；
- S1~S3 高风险入口的回归测试（native inline resources、rollback force guard、SnapshotJSON、ValidateIR、active publish query）。
- 全量 `go test`、`go test -race`、`golangci-lint` 通过。

## 待完成

1. 对 worker/singleflight/退避增加更多时序、取消和并发测试；
2. Store/conf 剩余低覆盖路径（`RollbackPublish`、migration failure、watch polling）增加故障注入测试；
3. 接入 M-API 后进行鉴权、REST 和 UI 集成验收。

## 当前验收结论

S4 已达到“核心采集与确认能力可用”，但 M-API 集成和少量跨模块故障注入仍未完成；在 T4 完成前不得进入 S5 管理 API 的最终验收。

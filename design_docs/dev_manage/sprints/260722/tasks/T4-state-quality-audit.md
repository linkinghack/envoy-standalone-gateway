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
- singleflight 等待者独立取消、退避 1s→60s 递增/封顶及成功复位；
- S1~S3 高风险入口的回归测试（native inline resources、rollback force guard、SnapshotJSON、ValidateIR、active publish query）。
- `RollbackPublish` 完整恢复/发布/版本血缘、watch polling 变更与取消、SQLite migration failure 故障注入；
- 全量 `go test`、`go test -race`、`golangci-lint` 通过。
- 2026-07-22 补跑 static/ADS 两套真实 Envoy e2e，并完成 Envoy 1.37.5、1.38.3、1.39.0 × 6 份 static golden 的 validate matrix，全部通过。

## 后续边界

- M-API 鉴权、REST 与 UI 集成属于本 Sprint `requirements.md` 明确的非范围，转入 S5/S6 验收；
- Envoy 多 minor 版本矩阵继续由 CI `validate-matrix` 门禁持续执行；本轮本地矩阵亦已全绿。

## 当前验收结论

S4 已完成：需求列出的只读 admin 访问、状态归一化、版本确认、Stale 保留、时间序列和发布自动确认均有直接测试，T4 审计缺口已补齐，可以进入 S5。

# T4：S4 完整性审计与状态采集补齐

## 目标

对 M-STATE 基础骨架进行契约级验证，避免“构建和少量单测全绿”被误判为设计项全部完成；补齐状态采集设计中尚未落地的功能和测试。

## 已完成

- admin GET 白名单、HTTP 状态码、Prometheus 流式透传测试；
- UDS/TCP client 错误路径与 JSON 解码测试；
- SeriesStore 环形淘汰、基数上限测试；
- `CONFIRMED`/`TIMEOUT` 事件测试；
- listener/cluster owner 反解和 endpoint 地址、健康、权重、失败标记解析；
- 全量 `go test`、`go test -race`、`golangci-lint` 通过。

## 待完成

1. 周期调度器：分级 ticker、失败退避和启动期 `/ready` 探测；
2. `/stats?format=json` 解析并写入 SeriesStore；
3. `/certs` 归一化到 `CertState`，并验证过期阈值；
4. 快照模型增加 ready 状态及 routes/certs/stats 消费路径；
5. 单一 admin worker、按需请求 singleflight；
6. `Publisher` 订阅 `VersionConfirmEvent`，自动推进 `CONFIRMING → EFFECTIVE/TIMEOUT`；
7. 对上述流程增加时序、取消、失败退避和并发测试。

## 当前验收结论

S4 仅达到“基础骨架可用”，不能宣称完整设计验收通过；在 T4 完成前不得进入 S5 管理 API 的最终验收。

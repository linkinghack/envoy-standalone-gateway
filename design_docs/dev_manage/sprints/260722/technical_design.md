# Sprint 260722 技术设计

M-STATE 作为只读旁路，通过 `internal/state.AdminClient` 访问 Envoy admin。请求仅允许设计清单内 GET 路径；UDS 使用 `unix:///` 地址，TCP 使用 host:port。`Service` 缓存归一化快照，refresh 失败保留上次数据并标记 `Stale`。

发布确认由 `ExpectVersion` 登记期望 IR.Version，快速通道以 500ms 起步、指数退避至 5s 轮询 config_dump；LDS/RDS/CDS/SDS 的 `version_info` 收敛为期望版本时发 `CONFIRMED`，超时发 `TIMEOUT`，均不自动回滚。

时间序列使用固定容量环形缓冲，默认 2160 点、10 秒间隔、5000 条序列上限，进程重启即清零。

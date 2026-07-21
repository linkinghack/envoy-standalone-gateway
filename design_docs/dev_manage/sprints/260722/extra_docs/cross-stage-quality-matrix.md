# S1–S4 跨阶段质量核验矩阵

## 验证证据

| 阶段 | 关键链路 | 单元/属性验证 | 真实功能验证 | 当前结果 |
|---|---|---|---|---|
| S1 | 协议加载、默认值、严格字段、F1–F6 编译、确定性 IR、static 渲染 | `internal/protocol`、`internal/compile`、golden、fuzz、determinism | `make e2e`：TLS/SNI、路由、HTTP→HTTPS、未知 SNI 拒绝 | 通过 |
| S2 | bootstrap、ADS Snapshot、ACK/NACK、版本一致性、M-CORE serve | `internal/deliver/xds`、`internal/config`、`internal/core` | `make e2e-xds`：Envoy 真实 ADS、LDS/RDS/CDS/EDS/SDS、版本、真实流量 | 通过 |
| S3 | SQLite migration、draftHash、快照、diff、回滚、发布状态机、baseHash | `internal/store`、`internal/conf`，包括 native、完整 RollbackPublish、watch polling、migration failure、active-run、自动确认测试 | 发布状态机使用 fake deliver；真实 Envoy 发布联动待管理 API 接入 | 通过；API 外部联调属于 S5 |
| S4 | admin 只读访问、状态归一化、ready/stats/certs/routes、版本确认、时间序列 | `internal/state`，包括 race、串行、singleflight/等待者取消、退避递增/封顶/复位、确认事件测试 | admin HTTP/Prometheus 契约测试；版本确认使用 fake admin | 通过；M-API 集成属于 S5 |

## 质量门禁

```text
go test ./...
go test -race ./...
go vet ./...
golangci-lint run ./...
make build
make e2e
make e2e-xds
make validate-matrix
```

2026-07-21 收口实跑 `make build`、`go test ./...`、`CC=clang CGO_ENABLED=1 go test -race ./...`、`go vet ./...`、`golangci-lint run ./...`，均通过。2026-07-22 Docker 恢复后补跑 `make e2e`、`make e2e-xds`，全部断言通过；同时实跑 `make validate-matrix`，Envoy 1.37.5/1.38.3/1.39.0 对 6 份 static golden 的 18 个组合全部通过。覆盖率用于发现未验证路径，不作为单独的正确性证明。

## 尚未宣称完成的内容

- S4 collector 已完成；管理 API、鉴权和 UI 不属于当前冲刺，生产 M-API 挂载属于 S5；

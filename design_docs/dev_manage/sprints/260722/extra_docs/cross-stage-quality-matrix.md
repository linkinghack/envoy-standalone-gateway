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
```

2026-07-21 收口实跑 `make build`、`go test ./...`、`CC=clang CGO_ENABLED=1 go test -race ./...`、`go vet ./...`、`golangci-lint run ./...`，均通过。两套真实 Envoy e2e 已于 2026-07-20 在运行时代码的当前提交通过；7 月 21 日仅新增测试与文档，Docker daemon 不可用，未重复运行。覆盖率用于发现未验证路径，不作为单独的正确性证明。

## 尚未宣称完成的内容

- S4 管理 API、鉴权和 UI 不属于当前冲刺；
- S4 collector 已完成；生产 M-API 挂载属于 S5；
- Envoy 多 minor 版本矩阵需要 CI/外部环境逐版本运行，单一本地镜像不替代矩阵证据。

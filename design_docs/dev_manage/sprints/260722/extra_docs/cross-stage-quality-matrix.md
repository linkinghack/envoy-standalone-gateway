# S1–S4 跨阶段质量核验矩阵

## 验证证据

| 阶段 | 关键链路 | 单元/属性验证 | 真实功能验证 | 当前结果 |
|---|---|---|---|---|
| S1 | 协议加载、默认值、严格字段、F1–F6 编译、确定性 IR、static 渲染 | `internal/protocol`、`internal/compile`、golden、fuzz、determinism | `make e2e`：TLS/SNI、路由、HTTP→HTTPS、未知 SNI 拒绝 | 通过 |
| S2 | bootstrap、ADS Snapshot、ACK/NACK、版本一致性、M-CORE serve | `internal/deliver/xds`、`internal/config`、`internal/core` | `make e2e-xds`：Envoy 真实 ADS、LDS/RDS/CDS/EDS/SDS、版本、真实流量 | 通过 |
| S3 | SQLite migration、draftHash、快照、diff、回滚、发布状态机、baseHash | `internal/store`、`internal/conf`，补充 native/rollback/active-run/自动确认测试 | 发布状态机使用 fake deliver；真实 Envoy 发布联动待管理 API 接入 | 核心通过，完整 API 外部联调留后续 |
| S4 | admin 只读访问、状态归一化、ready/stats/certs/routes、版本确认、时间序列 | `internal/state`，包括 race、退避、singleflight、确认事件测试 | admin HTTP/Prometheus 契约测试；版本确认使用 fake admin | 核心通过，M-API 集成待后续 |

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

以上命令在本次审计中均通过。全仓库语句覆盖率约 81.6%；覆盖率用于发现未验证路径，不作为单独的正确性证明。

## 尚未宣称完成的内容

- S4 管理 API、鉴权和 UI 不属于当前冲刺；
- S4 当前 collector 已有串行请求、按端点 singleflight 和指数退避，但尚无生产 M-API 挂载；
- S3 的 `RollbackPublish`、watch polling 和部分 migration 故障注入仍需继续增加测试；
- Envoy 多 minor 版本矩阵需要 CI/外部环境逐版本运行，单一本地镜像不替代矩阵证据。

# M1 需求—实现—证据矩阵

## P0 功能需求

| ID | 结论 | 实现 | 直接证据 |
|---|---|---|---|
| FR-1.1 | 达成 | v1alpha1 Gateway/Listener/Route/Upstream/Policy 协议 | `internal/protocol`、`internal/protocol/load_test.go` |
| FR-1.2 | 达成 | HTTP(S)、匹配/改写、TLS/SNI、LB、健康检查、超时/重试 | `internal/compile/*_test.go`、`testdata/s1` 真实流量 |
| FR-1.5 | 达成 | `apiVersion: esgw/v1alpha1` 严格校验 | `internal/protocol/envelope.go`、`internal/protocol/load_test.go` |
| FR-2.1 | 达成 | 原生 Envoy static → IR，版本/发布路径复用 | `internal/conf/advanced_test.go`、`internal/conf/replace_test.go`、配置 API tests |
| FR-2.3 | 达成 | static/xDS 编译产物 API 与控制台查看 | `cmd/esgw/compile_test.go`、`web/src/pages/expert-page.tsx` |
| FR-3.1 | 达成 | static 原子文件、托管 hot restart、失败恢复 last-good | `internal/deliver/static`、`e2e/static-managed/run.sh` |
| FR-3.2 | 达成 | ADS 五类资源、ACK/NACK 与 bootstrap | `internal/deliver/xds`、`e2e/xds/run.sh` |
| FR-3.3 | 达成 | load/link/build/validate/envoy 全链路后原子发布 | `internal/compile`、`internal/conf/publish_test.go`、拓扑矩阵非法配置断言 |
| FR-4.1 | 达成 | 五对象配置工作区、草稿校验/发布 | `web/src/pages/configuration-page.tsx`、Vitest/Playwright |
| FR-4.3 | 达成 | listeners/clusters/endpoints/routes/certs 实际状态 | `internal/state`、`web/src/pages/runtime-page.tsx` |
| FR-4.6 | 达成 | 本地账号、Argon2id、会话、CSRF 与限流 | `internal/auth`、`internal/api/auth_handlers_test.go` |
| FR-5.1 | 达成 | 单 binary、hardened systemd、安装/升级/卸载 | `packaging/systemd`、`packaging/tests/install_test.sh` |
| FR-5.2 | 达成 | `esgw`/all-in-one 镜像与三份 compose | `packaging/docker`、`packaging/tests/image_smoke.sh` |
| FR-5.4 | 达成 | SQLite 内嵌、文件真源、无外部运行时依赖 | `internal/store`、静态 binary、资源基线 |
| FR-5.6 | 达成 | 管理面退出后 Envoy 保持 last-good | `e2e/topology-matrix/run.sh` 四组合连续流量 |
| FR-6.4 | 达成 | 静态 IP 与 logical/strict DNS upstream | `internal/compile/build_upstream_test.go`、quickstart DNS 后端 |

## 非功能需求与边界

| ID | 结论 | 证据/边界 |
|---|---|---|
| NFR-1 | 达成 | `docs/quickstart.md` 与可执行 quickstart compose/配置门禁 |
| NFR-2 | 达成 | 2026-07-22 最终门禁空载稳态 RSS 29,016 KiB，目标 153,600 KiB；一次性 Argon2 bootstrap 峰值 158,380 KiB 不属于常驻 |
| NFR-3 | 达成 | 原子发布、last-good 与四拓扑管理故障连续流量 |
| NFR-4 | 达成 | 管理面不在请求数据路径，代理由官方 Envoy 承载 |
| NFR-5 | M1 边界接受 | 控制台强鉴权/文件权限完成；跨主机 xDS mTLS 与私钥 KMS/静态加密按已冻结范围延后，见 `docs/security.md` |
| NFR-6 | 达成 | Envoy 1.37.5/1.38.3/1.39.0 validate matrix |
| NFR-7 | 达成 | linux/darwin × amd64/arm64 `make portability-test`；发行归档提供两种 Linux 架构 |

M1 关闭标准是全部 P0 功能需求有直接实现与测试证据；P1/P2 与上表明确的 NFR-5 延后项进入 S9+，不在 M1 中隐式宣称完成。

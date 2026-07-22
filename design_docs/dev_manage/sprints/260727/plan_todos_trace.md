# Sprint 260727 任务拆分与进展追踪

| ID | 任务 | 状态 | 验收 |
|---|---|---|---|
| T1 | L4 语义、TCP/TLS/UDP Builder 与 golden | 已完成 | unit + 3-version validate |
| T2 | extAuth HTTP/gRPC 编译与冲突模型 | 已完成 | unit + auth service traffic |
| T3 | IPAccess 与动态 key 本地限流 | 已完成 | policy unit + isolated quota e2e |
| T4 | downstream mTLS、熔断与协议校验收紧 | 已完成 | TLS/circuit tests + traffic |
| T5 | schema/spec/examples 独立发行与 conformance | 已完成 | clean-diff + external runner |
| T6 | static/xDS 真实矩阵、文档与 Sprint 收口 | 已完成 | full gates + A1–A8 |

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-23 | M1 完成度审计确认 S9+ 尚未开展；制定 M2 完成计划并拆为 S9–S12。S9 冻结 L4、P1 策略、限流不降级、downstream mTLS 与协议发行边界，T1 开始。 |
| 2026-07-23 | T1 完成 TCP/TLS passthrough/UDP Envoy Builder、L4 路由/Policy/TLS 终止冲突诊断与 F6 typed filter cluster 引用闭合；纠正旧协议中 TLS passthrough 强制终止证书的矛盾。 |
| 2026-07-23 | T2 完成 HTTP/gRPC extAuth、Listener provider 冲突模型、route disable、HTTPS 显式信任和自动 auth cluster；真实验证 allow/deny/fail-open/fail-closed。 |
| 2026-07-23 | T3 完成 IPAccess 与动态 key 本地限流：修复旧实现默认 0%/共享桶降级，加入 wildcard descriptor、动态桶 LRU 上限和安全来源地址边界；三个 Envoy 版本真实验证 IP/header 独立配额与 XFF 不可伪造。 |
| 2026-07-23 | T4 完成 downstream mTLS 与熔断：client CA static 文件/xDS SDS 双形态、ClientAuth 强制验证、熔断合法范围和 DEFAULT priority；三版本真实验证证书拒绝/通过及并发 503 overflow。 |
| 2026-07-23 | T5 完成独立协议发行：schema/spec/examples、稳定诊断 conformance CLI、仓库外 clean-diff、CI 与双架构 release 归档闭环；完整 release 通过并修复归档清单 SIGPIPE/SPDX 匹配旧缺陷。 |
| 2026-07-23 | T6 与 S9 完成：无 bind-mount 的 P1 组合配置在 Envoy 1.37.5/1.38.3/1.39.0 完成 static validate、真实 ADS 五类资源 ACK/无 NACK及全能力流量；Go/Web/协议/文档与 3×10 validate 全门禁通过。 |

## 验收核验

| ID | 状态 | 证据 |
|---|---|---|
| A1 TCP/TLS/UDP 编译 | 已核验 | unit、static/xDS golden；Envoy 1.37.5/1.38.3/1.39.0 × 10 artifacts validate 全通过 |
| A2 L4 真实流量 | 已核验 | `e2e/l4/run.sh`：TCP、双 SNI TLS passthrough、未知 SNI 拒绝、UDP echo |
| A3 extAuth | 已核验 | `testdata/extauth` static/xDS + 三版本 validate；`e2e/extauth/run.sh` HTTP/gRPC 真实流量 |
| A4 IPAccess/限流 key | 已核验 | `testdata/ipaccess` static/xDS；`e2e/policy/run.sh` 三版本 allow/deny/XFF 边界与 clientIP/header 独立桶 |
| A5 downstream mTLS/熔断 | 已核验 | `testdata/mtls-circuit` static/xDS；`e2e/mtls-circuit/run.sh` 三版本证书验证与真实 503 overflow |
| A6 协议发行/conformance | 已核验 | `make protocol-check` 仓库外通过；提交 schema/spec/6 样例；双架构 release 含完整 bundle 且 SHA256 校验通过 |
| A7 三版本/static/xDS | 已核验 | `e2e/p1-xds/run.sh`：每版先验 combined static，再连接真实 ADS 核验 LDS/RDS/CDS/EDS/SDS 版本、Secret、ACK/无 NACK与 P1 全流量 |
| A8 全量门禁 | 已核验 | gofumpt/build/unit/race/vet/golangci/fuzz；protocol clean-diff/conformance；3×10 Envoy validate；4 个分项 e2e + 组合 ADS；Web generate/typecheck/Vitest/build/桌面移动 Playwright；docs 全通过 |

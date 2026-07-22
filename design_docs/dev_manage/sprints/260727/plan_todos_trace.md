# Sprint 260727 任务拆分与进展追踪

| ID | 任务 | 状态 | 验收 |
|---|---|---|---|
| T1 | L4 语义、TCP/TLS/UDP Builder 与 golden | 已完成 | unit + 3-version validate |
| T2 | extAuth HTTP/gRPC 编译与冲突模型 | 已完成 | unit + auth service traffic |
| T3 | IPAccess 与动态 key 本地限流 | 进行中 | policy unit + isolated quota e2e |
| T4 | downstream mTLS、熔断与协议校验收紧 | 待开始 | TLS/circuit tests + traffic |
| T5 | schema/spec/examples 独立发行与 conformance | 待开始 | clean-diff + external runner |
| T6 | static/xDS 真实矩阵、文档与 Sprint 收口 | 待开始 | full gates + A1–A8 |

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-23 | M1 完成度审计确认 S9+ 尚未开展；制定 M2 完成计划并拆为 S9–S12。S9 冻结 L4、P1 策略、限流不降级、downstream mTLS 与协议发行边界，T1 开始。 |
| 2026-07-23 | T1 完成 TCP/TLS passthrough/UDP Envoy Builder、L4 路由/Policy/TLS 终止冲突诊断与 F6 typed filter cluster 引用闭合；纠正旧协议中 TLS passthrough 强制终止证书的矛盾。 |
| 2026-07-23 | T2 完成 HTTP/gRPC extAuth、Listener provider 冲突模型、route disable、HTTPS 显式信任和自动 auth cluster；真实验证 allow/deny/fail-open/fail-closed。 |

## 验收核验

| ID | 状态 | 证据 |
|---|---|---|
| A1 TCP/TLS/UDP 编译 | 已核验 | unit、static/xDS golden；Envoy 1.37.5/1.38.3/1.39.0 × 7 artifacts validate 全通过 |
| A2 L4 真实流量 | 已核验 | `e2e/l4/run.sh`：TCP、双 SNI TLS passthrough、未知 SNI 拒绝、UDP echo |
| A3 extAuth | 已核验 | `testdata/extauth` static/xDS + 三版本 validate；`e2e/extauth/run.sh` HTTP/gRPC 真实流量 |
| A4 IPAccess/限流 key | 待核验 | — |
| A5 downstream mTLS/熔断 | 待核验 | — |
| A6 协议发行/conformance | 待核验 | — |
| A7 三版本/static/xDS | 待核验 | — |
| A8 全量门禁 | 待核验 | — |

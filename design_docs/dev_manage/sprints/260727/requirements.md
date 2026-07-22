# Sprint 260727 需求：P1 数据面与协议发行

## 目标

消除协议已声明但编译器未实现或语义降级的能力，交付 extAuth、IPAccess、按 key 限流、TCP/TLS/UDP、下游客户端证书校验与可独立消费的协议包，为后续控制台和 Kubernetes 冲刺提供稳定契约。

## 范围

- TCP proxy、TLS passthrough（SNI 分流）与 UDP proxy 的统一 IR/编译/下发；
- HTTP/gRPC extAuth 与 route 级关闭；IP allow/deny；
- `rateLimit.key=clientIP|header:<name>` 的真实动态分桶语义；
- HTTPS `clientCA` 的客户端证书验证；熔断字段的结构校验和真实产物证据；
- 协议 JSON Schema、规范、示例和 conformance bundle 独立发行；
- static/xDS、Envoy 1.37–1.39 validate 与真实流量回归。

## 非范围

- Basic Auth、OIDC、多账号/RBAC、审计（P2）；
- 多 Envoy 节点和跨主机 xDS mTLS（P2）；
- HTTP/3、Kubernetes EDS 跟踪、分布式限流（P2）；
- 统计/版本控制台与 Helm（S10/S11）。

## 验收标准

1. TCP 双向流量、TLS passthrough SNI 分流和 UDP 请求/响应均在真实 Envoy 上通过，static/xDS 产物覆盖同一 IR；
2. extAuth HTTP/gRPC 分别有允许、拒绝、fail-open、route disabled 流量证据，非法或同 Listener 冲突配置必须定位报错；
3. IP allow/deny 优先级和可信源地址边界有单测与流量测试；限流不同 IP/header 值使用独立配额而非共享桶；
4. `clientCA` 无客户端证书拒绝、有可信证书通过；熔断字段范围校验与 Envoy proto 断言完整；
5. schema/spec/examples bundle 可在仓库外校验合法与非法样例，生成后 clean diff；
6. 全量 Go/Web 回归、三版本 Envoy validate 和本 Sprint e2e 通过，无未说明的 unsupported/降级路径。

## 验收结论

2026-07-23 已完成 1–6：三版本 static/ADS 组合流量覆盖全部 P1 数据面能力，协议 bundle 可在仓库外独立复现，Go/Web/协议/Envoy/文档全门禁通过。唯一需显式保留的边界是 P1 SDS Secret 仍引用 filename DataSource，数据面证书材料必须落地到相同路径；跨主机独立材料传输属于已列明的 P2 非范围，不构成静默降级。

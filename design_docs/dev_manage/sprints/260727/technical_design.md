# Sprint 260727 技术设计

## 1. 统一编译模型

HTTP 与 L4 都从同一 `protocol.ConfigSet` 进入 F1–F7。IR 继续承载 Envoy v3 Listener/Route/Cluster/Endpoint/Secret；L4 Listener 不创建 RouteConfiguration，直接把 `Route.spec.forward` 编译为 listener filter chain。static 与 xDS 只负责形态化，不知道协议类型。

| Listener 协议 | Envoy 映射 | 路由约束 |
|---|---|---|
| TCP | `envoy.filters.network.tcp_proxy` | 恰好一个无 `sniHosts` forward |
| TLS | `tls_inspector` + 按 `sniHosts` 排序的 filter chain + `tcp_proxy` | 每条 route 至少一个 SNI；缺失/重复报错 |
| UDP | UDP listener + `envoy.filters.udp_listener.udp_proxy` | 恰好一个 forward；禁止 TLS/HTTP 字段 |

TCP/TLS/UDP cluster 复用现有 Upstream。Envoy 的 Cluster 在此路径承载端点和负载均衡、没有独立的 raw UDP protocol 开关；UDP 语义由 listener socket 与 `udp_proxy` 的 upstream socket 管理。单后端 UDP 暂用跨 1.37–1.39 稳定的 `cluster` route specifier；其替代 matcher 在官方 API 中仍标记 work-in-progress，升级前不得为了消除 deprecated 标记牺牲稳定性。L4 不生成 HTTP route filter、retry/header/policy 配置，挂 HTTP-only policy 必须报 link 错误。

`protocol: TLS` 固定为 passthrough：禁止 `Listener.spec.tls`，不生成 DownstreamTlsContext/Secret；`tls_inspector` 只读取 ClientHello SNI。HTTPS 仍是唯一使用 Listener TLS 终止配置的协议。

## 2. P1 HTTP 策略

### 2.1 extAuth

Envoy ext_authz 的服务配置位于 HCM filter 而非 per-route。一个 Listener 内所有最终启用的 extAuth 必须解析为同一服务、协议和 `failOpen`；否则 F3 报定位错误。filter 顺序固定为 `rbac → cors → jwt_authn → ext_authz → local_ratelimit → router`。未挂 extAuth 或 `disabled: true` 的 route 写入 `ExtAuthzPerRoute.disabled=true`。

HTTP 地址解析为 scheme/host/port，自动生成 STRICT_DNS/STATIC auth cluster；gRPC `host:port` 生成 HTTP/2 cluster。cluster 名由规范化配置哈希派生，避免与用户 Upstream 冲突。禁止 userinfo、path/query/fragment、非 http(s) scheme 和缺失端口/主机。HTTPS 必须显式提供 `caFile`，或明确声明 `insecureSkipVerify: true`；不得虚构 Envoy 会自动使用系统 CA 的隐式默认。

### 2.2 IPAccess

使用 `envoy.filters.http.rbac` 的 per-route `RBACPerRoute`。CIDR 先规范化、排序、去重。语义冻结为：命中 deny 永远拒绝；allow 非空时仅 allow 集合可进入；allow 为空表示默认允许。规则组合为 `allow AND NOT deny`，来源采用 Envoy downstream remote address。Standalone 边缘模式固定 `use_remote_address=true`、`xff_num_trusted_hops=0`，客户端自带 XFF 不参与可信源地址计算；可信上游代理链配置不在本 Sprint，部署在代理之后时必须保留该限制说明，不能把任意 XFF 当作客户端身份。

### 2.3 本地限流 key

禁止继续共享单桶降级。Builder 使用 per-route local-rate-limit `rate_limits` action 生成动态 descriptor：`remote_address` 对应 clientIP，`request_headers` 对应 header；descriptor 使用同 key 和空 value。Envoy API 明确定义空 value 为 wildcard，会按每个运行时 value 创建独立 token bucket；`maxKeys` 映射 `max_dynamic_descriptors`，默认 10000、协议范围 1..100000，以 LRU 限制高基数内存。filter 必须显式 100% enabled/enforced，并设置 `always_consume_default_token_bucket=false`，避免 descriptor 命中后再消费共享桶；header 缺失时没有 descriptor，使用默认桶而不是绕过限流。三版本真实流量必须分别锁定上述行为。

## 3. TLS 与连接保护

`Listener.tls.clientCA` 编译为 DownstreamTlsContext validation context 的 trusted CA，并设置 `require_client_certificate=true`。编译期解析 CA PEM，错误定位到 `spec.tls.clientCA`。Upstream `maxConnections/maxPendingRequests` 必须为正数，默认 priority 为 DEFAULT；补齐 proto/golden/真实过载证据，不再只做“字段透传”。

## 4. 协议发行

`internal/protocol` 仍是单一事实来源。新增 `esgw schema` 命令导出 bundle，仓库提交 `protocol/schema/`、`protocol/examples/` 与面向第三方的规范索引。生成器在临时目录输出后与提交内容逐字节比较；示例按 valid/invalid 分类，conformance runner 返回稳定诊断码和 SourceRef。

本 Sprint 保持 `esgw/v1alpha1`，因为 S10/S11 仍可能产生加法字段；S12 再根据兼容审计决定 v1beta1/v1。新增字段只做加法，未知字段继续严格失败。

## 5. 测试与风险

- 单测：协议校验、link 互斥、filter chain/typed Any、确定性排序、SourceMap；
- golden：HTTP P1、TCP、TLS passthrough、UDP 各一组 static/xDS；
- e2e：真实 echo/auth/UDP/TLS 后端，连续流量和错误路径；
- validate：1.37.5、1.38.3、1.39.0；
- 风险：UDP xDS 资源依赖 Envoy 扩展可用性、extAuth filter 单实例限制、动态限流 descriptor 跨版本差异。任何差异先形成 spike 证据并回写本设计。

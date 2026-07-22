# Sprint 260727 技术设计

## 1. 统一编译模型

HTTP 与 L4 都从同一 `protocol.ConfigSet` 进入 F1–F7。IR 继续承载 Envoy v3 Listener/Route/Cluster/Endpoint/Secret；L4 Listener 不创建 RouteConfiguration，直接把 `Route.spec.forward` 编译为 listener filter chain。static 与 xDS 只负责形态化，不知道协议类型。

| Listener 协议 | Envoy 映射 | 路由约束 |
|---|---|---|
| TCP | `envoy.filters.network.tcp_proxy` | 恰好一个无 `sniHosts` forward |
| TLS | `tls_inspector` + 按 `sniHosts` 排序的 filter chain + `tcp_proxy` | 每条 route 至少一个 SNI；重叠/兜底冲突报错 |
| UDP | UDP listener + `envoy.filters.udp_listener.udp_proxy` | 恰好一个 forward；禁止 TLS/HTTP 字段 |

TCP/TLS cluster 复用现有 Upstream；UDP cluster 显式设置 UDP upstream protocol options。L4 不生成 HTTP route filter、retry/header/policy 配置，挂 HTTP-only policy 必须报 link 错误。

## 2. P1 HTTP 策略

### 2.1 extAuth

Envoy ext_authz 的服务配置位于 HCM filter 而非 per-route。一个 Listener 内所有最终启用的 extAuth 必须解析为同一服务、协议和 `failOpen`；否则 F3 报定位错误。filter 顺序固定为 `rbac → cors → jwt_authn → ext_authz → local_ratelimit → router`。未挂 extAuth 或 `disabled: true` 的 route 写入 `ExtAuthzPerRoute.disabled=true`。

HTTP 地址解析为 scheme/host/port，自动生成 STRICT_DNS/STATIC auth cluster；gRPC `host:port` 生成 HTTP/2 cluster。cluster 名由规范化配置哈希派生，避免与用户 Upstream 冲突。禁止 userinfo、fragment、非 http(s) scheme 和缺失端口/主机。

### 2.2 IPAccess

使用 `envoy.filters.http.rbac` 的 per-route `RBACPerRoute`。CIDR 先规范化、排序、去重。语义冻结为：命中 deny 永远拒绝；allow 非空时仅 allow 集合可进入；allow 为空表示默认允许。规则组合为 `allow AND NOT deny`，来源采用 Envoy downstream remote address；受代理链影响的 XFF 信任配置不在本 Sprint，文档必须提示边界。

### 2.3 本地限流 key

禁止继续共享单桶降级。Builder 使用 route rate-limit actions 生成动态 descriptor：`remote_address` 对应 clientIP，`request_headers` 对应 header；local rate limit descriptor 必须与动作生成的 key/value 匹配。实现前用三个支持版本的真实 Envoy spike 锁定“空 descriptor value 是否通配”的行为；若版本不一致，改用各版本均支持的 filter-state/input matcher 方案，不能缩减验收标准。

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

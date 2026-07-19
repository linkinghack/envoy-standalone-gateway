# T4 internal/compile F3：Builder 与策略映射

- **状态**: 已完成
- **依赖**: T3
- **设计依据**: [编译层 §3/F3（Builder 表 + 策略实现映射表）、§5 命名规约](../../../../system_design/260716-2-compile-ir-design.md)、协议 §5 映射概览

## 目标

四类 Builder 纯函数：已链接 ConfigSet → 逻辑 Envoy v3 资源（go-control-plane proto），严格遵循命名规约与确定性规则。**M0 范围**：HTTP/HTTPS Listener、全部 Route rule 能力（match/rewrite/backends 权重/redirect/directResponse/timeout/retry）、Upstream 三来源中的 endpoints/dns、策略 `headerModifier`/`cors`/`rateLimit`/`jwt`。L4/UDP、mTLS clientCA、熔断字段留 P1（结构预留，值透传但不设计专项测试）。

## 执行步骤

1. **命名与确定性基建**：资源命名函数（`lis/ rc/ vh/ us/ crt/` 规约）；map 按 name 排序输出；Builder 禁止读时间/随机/环境（§5 禁止项，code review 检查点）。
2. **ListenerBuilder**：HTTP/HTTPS；HTTPS 按证书 SAN/CN 提取域名生成 filter_chain_match（SNI），每证书一条 chain 共享 HCM 配置；HCM `stat_prefix = lis/<name>`、挂 RDS 名 `rc/<listener>`；Gateway.http 默认值与 Listener 覆盖合并；access log 配置。表驱动用例覆盖：单证书/多证书/通配 SAN/无 SAN 用 CN/SAN 重叠（技术设计 §4 风险项）。
3. **RouteBuilder**：每 Listener 一份 RouteConfiguration；每 Route 一个 VirtualHost（domains=hostnames，按精确>通配>兜底排序）；rules 保序翻译 match（path 三形态/methods/headers/queryParams）、rewrite（pathPrefix/path/regex + host）、weighted_clusters、redirect、direct_response、timeout、retry_policy（`on` 枚举映射 retry_on 串）。
4. **UpstreamBuilder**：endpoints→STATIC + CLA、dns→LOGICAL/STRICT_DNS；loadBalancer 策略映射（含 ringHash/maglev 的 hashOn→route hash_policy 联动）；healthCheck；上游 TLS transport socket；connection 参数。
5. **策略归一化纯函数**（技术设计 §4 风险项）：输入 Gateway/Listener/Route/rule 四级 policies（引用 + 内联），输出每 rule 最终生效策略集（同类型就近覆盖整体替换，协议 §3.5 规则 3）；单测穷举：四级覆盖、内联覆盖引用、`jwt.optional` 局部关闭。
6. **PolicyBuilder**：按编译层映射表实现——headerModifier 落 route 层字段；cors/local_ratelimit/jwt_authn 生成 HCM filter（固定顺序 `cors → jwt_authn → local_ratelimit → router`，全策略常驻链 + 默认 pass-through）+ typed_per_filter_config per-rule 生效。**C4 决议**：jwt providers 聚合去重 key（建议 `issuer + 规范化 jwks 来源`），记录并回写。
7. 单元测试以 S1/S2 场景为主干 + 各 Builder 边界例；产物结构断言用 protojson 快照（golden 正式化在 T7）。

## 验收

- [ ] S1/S2 全部对象可构建出结构正确的逻辑资源
- [ ] 策略四级挂接与就近覆盖语义测试穷举通过
- [ ] 命名规约与排序确定性有测试锁定
- [ ] C4 决议已记录并回写

## 进展记录

### 2026-07-19 完成

**交付**：`internal/compile/` 新增 `names.go`（命名规约）、`build.go`（build 编排 + buildContext）、`build_listener.go`、`build_route.go`、`build_upstream.go`、`build_policy.go`（策略归一化 + PolicyBuilder + JWT 装配），测试 5 个文件约 229 个用例/断言点全绿；`make build && make test && make lint` 全绿。Builder 以包内可单测函数交付，未动 `Compile()` 接线（T5）。

**C4 决议（jwt providers 聚合去重键）**：`issuer + "|" + 规范化 jwks 来源`（`uri:` + TrimSpace(uri) 或 `file:` + file）；audiences 不参与去重——同一 provider 的不同受众经 requirement_map 的 `provider_and_audiences` 表达。provider 名内容寻址：`jwt/<sha256(key) 前 8 位 hex>`，与收集顺序无关、跨 Listener 稳定。requirement 名：provider 名（无 audiences）或 `provider:aud1,aud2`；`jwt.optional=true` → 固定 requirement `allow-missing`（映射 Envoy allow_missing：token 缺失放行、存在仍校验）。

**与设计的出入/实现决策（非静默偏离）**：

1. `rateLimit.key`（clientIP | header:<name>）M0 不生效：Envoy 本地限流的 descriptor entry 是静态键值，无法表达按客户端 IP/请求头动态分桶（动态键属全局限流 service 能力）。M0 per-rule 落令牌桶（requests/unit/burst），代码注释与本记录标明。
2. `Listener.http.http3`（P2）预留不生效，不生成 QUIC。
3. redirect `code` 省略默认 301（MOVED_PERMANENTLY）；支持 301/302/303/307/308，其余值报 build 错误。
4. `healthCheck` 的 interval/timeout/threshold 协议未声明默认值：缺省不设置，由 F6 PGV 把关。
5. cors per-route 配置消息是 `extensions.filters.http.cors.v3.CorsPolicy`（非 route.v3.CorsPolicy），实现按前者。
6. `WeightedCluster.total_weight` 已被 Envoy 废弃（自动求和），不设置。
7. `crt/<listener>/<n>` 命名（SDS Secret）随 T5 形态化落地时引入，本 task 证书走文件路径内联。
8. L4/UDP Listener、`kubernetesService` 上游、extAuth/ipAccess/basicAuth 策略在 build 阶段显式报「not implemented in M0」错误（不静默跳过）。
9. 远程 JWKS 自动生成抓取集群 `jwt-jwks/<host:port>`（STRICT_DNS + https 时上游 TLS + SNI），HttpUri.timeout 取 5s（PGV 必填）。
10. HCM RDS ConfigSource 固定 ADS（逻辑资源形态），static 内联属 F5 职责。
11. `jwt_authn` per-rule 配置用 `requirement_name` 引用 requirement_map；filter 常驻链 + 默认 pass-through 经「无 providers/无 requirement_map、无 filter 级 token bucket、cors 空配置」实现，链结构不随策略增删抖动。

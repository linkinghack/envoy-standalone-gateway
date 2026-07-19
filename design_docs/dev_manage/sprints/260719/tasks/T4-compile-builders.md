# T4 internal/compile F3：Builder 与策略映射

- **状态**: 未开始
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

（接手会话在此追加）

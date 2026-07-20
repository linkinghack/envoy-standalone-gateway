# 260716-2 编译层与 IR 设计（M-COMPILE / IR Design）

- **项目**: envoy-standalone-gateway
- **上游文档**: [`260715-2-overall-architecture.md`](260715-2-overall-architecture.md)（A1 单一 IR、M-COMPILE 模块）、[`260716-1-gateway-config-protocol-v0.md`](260716-1-gateway-config-protocol-v0.md)（输入规格）
- **文档状态**: 初稿（Draft）
- **日期**: 2026-07-16
- **性质**: 模块详细设计。覆盖架构 §8 清单第 2 项：IR 定义、编译流水线、escape hatch 合成、三层校验、确定性与测试策略。M0 编译器原型按此实现。

---

## 1. IR 定义

IR（Intermediate Representation）= **一份完整、自洽、已通过校验的 Envoy v3 资源集合**（架构 A1），不自造中间模型：

```go
// internal/ir
type IR struct {
    Listeners map[string]*listenerv3.Listener             // key = 资源 name
    Clusters  map[string]*clusterv3.Cluster
    Routes    map[string]*routev3.RouteConfiguration      // RDS 资源
    Endpoints map[string]*endpointv3.ClusterLoadAssignment // EDS 资源（动态端点来源时）
    Secrets   map[string]*tlsv3.Secret                    // SDS 资源（xDS 模式证书）
    Bootstrap *bootstrapv3.Bootstrap                      // 骨架（admin、node、access log 默认等）

    Version   string            // 内容哈希（§6 确定性）
    SourceMap map[ResourceKey]SourceRef // Envoy 资源 → 协议对象溯源（排障/FR-2.3）
}
```

要点：

1. **IR 即下发载荷**：xDS 路线直接装入 go-control-plane `Snapshot`；static 路线渲染为单文件 YAML。两个输出都是 IR 的纯函数，属 M-DELIVER 职责（本文档 §7 只定义输出接口）。
2. **动静差异收敛在 IR 内部形态**：xDS 模式下 Listener 引 RDS/SDS、Cluster 引 EDS；static 模式下同一批资源内联化（route_config 内嵌、证书走文件路径、端点走 static/STRICT_DNS）。编译器主体只产出"逻辑资源"，最后由**形态化阶段**（§3 阶段 F5）按目标模式决定引用还是内联——这是编译层唯一感知下发模式的地方。
3. `SourceMap` 支撑两件事：校验错误回指到用户 YAML 的对象与路径；控制台"查看编译产物"按协议对象分组展示（FR-2.3）。

## 2. 输入模型

编译入口接受**配置集（ConfigSet）**——配置域（M-CONF）加载并解析信封后的对象集合：

```go
type ConfigSet struct {
    Gateway        *proto.Gateway            // 可为 nil（隐式默认）
    Listeners      []*proto.Listener
    Routes         []*proto.Route
    Upstreams      []*proto.Upstream
    Policies       []*proto.Policy
    EnvoyResources []*proto.EnvoyResources
}
```

- 协议 Go 类型定义在 `internal/protocol`（含版本转换：旧 apiVersion → 最新版，编译层只见最新版）。
- **专家模式整套原生 static 配置**（FR-2.1）走独立入口：原生 YAML → protojson 严格解析为 Bootstrap/资源集 → 直接构造 IR（跳过 F2~F4），复用同一套校验（F6）与输出。即"原生 static → IR 解析器"与"抽象协议 → IR 编译器"在 F6 汇合。
- k8s 动态端点：`kubernetesService` 类型的 Upstream 编译为 EDS 引用 + 占位 ClusterLoadAssignment；运行期 M-DISCO 只更新 `IR.Endpoints` 对应条目（走独立的 EDS 快速通道，不触发全量重编译）。编译层通过 `EndpointSource` 接口取初始端点，对 client-go 无依赖（A4）。

## 3. 编译流水线

```
ConfigSet
  F1 结构校验      JSON Schema（由 protocol Go 类型生成）+ strict decode（已在解析层完成）
  F2 链接与语义校验 引用解析（route→listener/upstream、policy ref）、跨对象约束
  F3 构建          每对象 Builder → 逻辑 Envoy 资源（确定性命名 §5）
  F4 合成          envoyPatch 应用 → EnvoyResources 合并
  F5 形态化        按下发模式（xds | static）决定 RDS/EDS/SDS 引用 vs 内联
  F6 资源校验      PGV Validate() 全量 + 跨资源引用完整性
  F7 终检(可选)    渲染等价 static → envoy --mode validate（发布流触发，非库内强制）
→ IR (+ Version, SourceMap)
```

各阶段职责与关键规则：

### F2 链接与语义校验

- 引用解析：全部 name 引用建立指针；悬空引用、重名对象（同 kind）报错。
- 跨对象约束（枚举，编译期全量检查）：
  - Listener `address:port` 唯一；
  - Route 挂接的 Listener 协议与 Route 形态匹配（HTTP rules vs L4 forward，协议 §3.3.5）；
  - 同 Listener 下 hostname 冲突检测（同精度重复 = 错误，协议 §3.3 要点 1）；
  - Policy `spec` 单类型键；策略挂接层级合法性（如 `ipAccess` 不可挂 rule 级以下）；
  - 证书文件存在性与密钥配对校验（static 模式下发前还会再查一次，此处早失败）；
  - TLS Listener 必须有 `tls`，HTTP Listener 不得有。
- 默认值填充在本阶段完成（协议文档声明的所有默认值集中在一处 `defaults.go`，避免散落）。

### F3 构建（Builder）

每个协议对象一个纯函数 Builder，输入已链接对象、输出逻辑资源：

| Builder | 产物 | 关键映射规则 |
|---|---|---|
| ListenerBuilder | Listener + FilterChain 骨架 + Secret | HTTPS：按证书 SAN 生成 filter_chain_match（SNI），每证书一条 chain 共享 HCM；HCM 挂 RDS 名 `rc/<listener>` |
| RouteBuilder | RouteConfiguration / VirtualHost / Route | 每 Listener 一份 `rc/<listener>`；每 Route 对象一个 VirtualHost（domains=hostnames）；rules 保序翻译；hostname 精度排序只影响 vhost domains 归组，Envoy 自身按精确>通配匹配 vhost，与协议语义一致 |
| UpstreamBuilder | Cluster + CLA | endpoints→STATIC、dns→LOGICAL/STRICT_DNS、kubernetesService→EDS；healthCheck/LB/TLS/熔断直译 |
| PolicyBuilder | HTTP filter + typed_per_filter_config | 见 §4 策略实现表 |
| L4 forward | tcp_proxy/udp_proxy filter | weight 单目标直连 cluster |

**策略实现映射**（PolicyBuilder 细则）：

| 策略类型 | Envoy 实现 | 层级机制 |
|---|---|---|
| headerModifier | route 层 `request_headers_to_add/...`（不占 filter） | 直接落在 vhost/route |
| cors | `envoy.filters.http.cors` + CorsPolicy per-route | per_filter_config |
| rateLimit | `envoy.filters.http.local_ratelimit` | per_filter_config（token bucket per rule） |
| jwt | `envoy.filters.http.jwt_authn` | providers 全局去重聚合 + requirement_map per-route |
| extAuth | `envoy.filters.http.ext_authz` | per-route disable/override |
| ipAccess | `envoy.filters.http.rbac` | per_filter_config |

HCM filter chain 固定顺序（协议 §3.5 规则 4）：`rbac → cors → jwt_authn → ext_authz → local_ratelimit → router`（cors 先于鉴权类 filter：preflight 请求不带凭证，必须在鉴权前被应答，见协议 §3.5 规则 4）。所有策略 filter 常驻链上、默认 pass-through，实际生效范围由 per-route 配置控制——避免"某 rule 加策略导致 filter chain 结构抖动"的大 diff。

### F4 合成（escape hatch）

顺序敏感，规则固定：

1. **envoyPatch 先于 EnvoyResources**；同对象多个 patch 按书写顺序应用。
2. patch 施加于 `target` 指向的**本对象产出资源**（经 SourceMap 定位），`merge` 用 protojson→JSON Merge Patch→protojson 回转；`jsonPatch` 用 RFC 6902。回转失败（字段不存在/类型不符）= 编译错误并回指 patch 位置。
3. EnvoyResources 合并：`@type` 分发到对应 IR 集合；与编译产物重名时默认报错，`allowOverride: true` 则整体替换（替换后 SourceMap 指向 EnvoyResources 对象）。
4. 合成产物**无豁免**地进入 F6 校验（协议 §7.1 承诺："写坏 = 发布被拦截"）。

### F6 资源校验

- 全资源 PGV `Validate()`（go-control-plane 生成的校验方法）。
- 跨资源完整性：RDS/EDS/SDS 引用名闭合、route 指向的 cluster 存在（EnvoyResources 引入的资源同样纳入）。
- 错误模型见 §5。

### F7 envoy 终检

`envoy --mode validate -c <rendered-static>`。xDS 模式也渲染等价 static 后验（可行性 §2.3）；由发布流按配置调用（默认开，无 envoy 二进制时降级警告）。不放进库内同步路径，避免编译 API 依赖外部二进制。

## 4. 错误模型

```go
type CompileError struct {
    Stage    string      // schema | link | build | patch | validate | envoy
    Source   SourceRef   // 文件、kind/name、YAML 路径（如 spec.rules[2].retry.on）
    Message  string      // 面向用户，中英文由 API 层处理
    Severity Level       // Error | Warning（如 F7 缺 envoy 二进制）
}
```

- **同阶段收集不中断**（collect, don't abort）：一次编译返回全部结构/链接错误，用户不必逐个试错；阶段间失败则短路（链接失败不进构建）。
- 全部错误必须带 `SourceRef`——UI 据此在 YAML 编辑器行内标注（FR-4.2），这是硬性验收标准。

## 5. 确定性与命名规约

**同一 ConfigSet + 同一目标模式 → 字节级相同的产物**。这是 golden file 测试、版本 diff（FR-3.4）、以及"配置未变则不触发下发"判断的基础。

| 规约 | 内容 |
|---|---|
| 资源命名 | `lis/<listener>`、`rc/<listener>`、`vh/<route>`、`us/<upstream>`、`crt/<listener>/<n>`；`/` 分隔避免与用户 name 字符集冲突，前缀短因其出现在 stats 名中（M-STATE 反解归属靠它） |
| stats 前缀 | HCM `stat_prefix` = `lis/<listener>`（listener 维度 HTTP stats 以 `http.lis/<n>.*` 归组）；cluster 维度天然以资源名 `us/<upstream>` 归组——这是 M-STATE 统计归组的静态契约（[260717-4](260717-4-state-collection-design.md) §5.1 消费） |
| 排序 | map 输出按 name 排序；rules 保持用户顺序；vhost domains 按精确>通配>兜底再字典序 |
| 版本号 | `IR.Version` = 全资源确定性序列化的 SHA-256 前 12 位；同时作为 Snapshot version 与 static 文件头注释 |
| 禁止项 | Builder 内禁止读取时间、随机数、环境；所有环境输入（如 envoy 版本目标）显式入参 |

## 6. 包结构与公共 API

```
internal/protocol/   信封与对象 Go 类型、strict decode、版本转换、JSON Schema 生成、defaults
internal/compile/
    link.go          F2
    build_*.go       F3（每 kind 一文件）
    patch.go         F4
    materialize.go   F5
    validate.go      F6
    envoycheck/      F7（exec 封装，独立子包便于禁用）
internal/ir/         IR 类型、SourceMap、哈希、static 渲染器与 Snapshot 装配的消费接口
```

公共入口（供 M-CONF 与 M0 CLI 共用）：

```go
func Compile(cs *protocol.ConfigSet, opts Options) (*ir.IR, []CompileError)
type Options struct {
    Mode           Mode   // ModeXDS | ModeStatic
    EndpointSource EndpointSource // k8s 端点注入接口，可 nil
    EnvoyValidate  *EnvoyValidateOpts // nil = 跳过 F7
}
```

M0 CLI 即 `esgw compile -f dir/ --mode static -o envoy.yaml` 对该函数的薄封装——先于任何服务端代码交付，用于协议表达力实证。

## 7. 与相邻模块的契约

| 相邻方 | 契约 |
|---|---|
| M-CONF（上游） | 提供 ConfigSet；消费 IR.Version 做版本记录、CompileError 做发布流校验结果 |
| M-DELIVER（下游） | 消费 IR：`snapshot.FromIR(ir)`（装入 SnapshotCache）与 `static.Render(ir)`（渲染 YAML）均为 ir 包内纯函数 |
| M-DISCO | 实现 `EndpointSource`；运行期端点变化直接调 M-DELIVER 的 EDS 通道更新 `IR.Endpoints` 副本，不经过 Compile |
| M-API | FR-2.3"查看编译产物"：按 SourceMap 分组返回资源的 protojson |

## 8. 测试策略

1. **Golden file 快照**：`testdata/<case>/{input/*.yaml, want-static.yaml, want-xds.json}`，S1、S2、escape hatch、L4、错误用例全覆盖；`-update` 刷新。确定性（§5）保证快照稳定。
2. **错误用例表驱动**：每条 F2 语义规则至少一个反例，断言 Stage/SourceRef/Message。
3. **多版本 envoy validate**：CI 矩阵拉官方镜像对 golden static 产物跑 `--mode validate`（支持区间 2~3 个 minor，对应 RK4/NFR-6）。
4. **端到端冒烟（M0 验收）**：docker 起 envoy 加载 S1 产物，curl 断言 TLS 与转发；M1 前补 ADS 拉起同一 IR 的 spike（可行性 §6.3）。
5. **patch 模糊测试**：随机 JSON Patch 施加于合法资源，断言"要么编译错误要么产物过 validate"，不允许第三态。

## 9. 未决事项

| # | 事项 | 计划 |
|---|---|---|
| C1 | ~~`envoyPatch` 的 `target` 取值全集与 per-rule 定位语法~~ **已决议（2026-07-19，Sprint 260719 T5）**：target 按对象 kind 定表——Listener→`listener`/`secret`；Route→`virtualHost`/`route`（rule 级须写作 `route/<ruleName>`）；Upstream→`cluster`/`endpoints`；Gateway→`bootstrap` 不允许（C2 留 M1，Gateway 任何 envoyPatch 均报错）。rule 级定位采用给 rule 加可选 `name` 字段（Route 内唯一、字符集同 metadata.name，F1 loader 校验）；无 `name` 的 rule 不可被 rule 级 patch 定位。实现见 `internal/compile/patch.go`（`patchTargets` 定表）与 `internal/protocol/route.go` | 已定 |
| C2 | EnvoyResources 中 Bootstrap 级字段（如 overload manager）是否允许 patch | M1 前定；v0 先只允许资源级 |
| C3 | ~~JSON Schema 生成工具选型（invopop/jsonschema vs 手写）~~ **已决议（2026-07-19，Sprint 260719 T2）**：invopop/jsonschema 由 Go 类型生成 + `JSONSchema()` 类型钩子处理 union/Duration/枚举，单一事实来源；bundle 见 `internal/protocol/schema.go` 的 `Schemas()` | 已定 |
| C4 | ~~jwt providers 聚合去重的 key 规则（issuer+jwks 相同视为同 provider？）~~ **已决议（2026-07-19，Sprint 260719 T4）**：去重键 = `issuer + "|" + 规范化 jwks 来源`（`uri:` + TrimSpace(uri) 或 `file:` + file），同 issuer+jwks 视为同一 provider；audiences 不参与去重，经 requirement_map 的 `provider_and_audiences` 表达；provider 名内容寻址 `jwt/<sha256(key) 前 8 位 hex>`；`jwt.optional=true` → requirement `allow-missing`。实现见 `internal/compile/build_policy.go`（`jwtProviderKey`） | 已定 |

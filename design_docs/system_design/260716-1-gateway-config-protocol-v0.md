# 260716-1 网关配置抽象协议规范 v0（Gateway Config Protocol v0）

- **项目**: envoy-standalone-gateway
- **上游文档**: [`requirements/01-initial-requirements-analysis.md`](../requirements/01-initial-requirements-analysis.md)（FR-1、FR-2.2/2.3）、[`260715-2-overall-architecture.md`](260715-2-overall-architecture.md)（§6 方向、A1/A4 原则）
- **文档状态**: 初稿（Draft），出稿门槛已达成：S1/S2 纸面演练通过（见 §8）
- **日期**: 2026-07-16
- **性质**: 协议专项设计。定义面向用户的声明式配置协议 v1alpha1，是 M0 编译器的输入规格；对应风险 RK1 的主要攻坚产出。

---

## 1. 设计目标与原则

| # | 原则 | 说明 |
|---|---|---|
| P1 | **五个名词讲清 80% 场景** | `Gateway` / `Listener` / `Route` / `Upstream` / `Policy`，与 NFR-1（一页文档可讲清）对齐；每个名词与用户心智中的既有概念（"入口端口""转发规则""后端"）一一对应 |
| P2 | **平台中立** | 协议不引用任何 k8s CRD 语义；k8s Service 只是 Upstream 的一种可选后端来源（FR-6），协议在裸机上语义完整 |
| P3 | **声明式、可 git 管理** | YAML/JSON 表达，多文档单文件或目录多文件均可；文件即真源（架构原则 A5） |
| P4 | **显式优于隐式** | 规则匹配按书写顺序自上而下（不做 nginx 式最长前缀隐式优先级）；对象间引用全部按 `name` 显式声明 |
| P5 | **表达力上限 = Envoy** | 通过两级 escape hatch（§7）保证任何 Envoy 能力最终可达，抽象不足不形成硬阻塞（FR-1.4 / RK1 缓解） |
| P6 | **严格解析** | 未知字段一律报错（strict decode），拼写错误在校验层暴露而非静默忽略 |

**非目标（v0 不覆盖，见 §10）**：Service Mesh 语义、多 Gateway 实例差异化配置、流量镜像、WASM 插件、分布式限流。

## 2. 协议总览

### 2.1 资源信封（Envelope）

所有对象采用统一信封，借用 k8s 风格（用户熟悉、天然支持版本化）但**不是 CRD**：

```yaml
apiVersion: esgw/v1alpha1     # 协议版本（组名待 D1 命名决议后替换，语义不变）
kind: Route                   # Gateway | Listener | Route | Upstream | Policy | EnvoyResources
metadata:
  name: api                   # 同 kind 内唯一，^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$
  labels: {}                  # 可选，供 UI 分组/筛选，编译不消费
spec: {...}
```

- 一个 YAML 文件可含多个 `---` 分隔的文档；配置目录下所有 `*.yaml|*.yml|*.json` 合并为一个配置集（加载细节属配置域文档）。
- 对象间引用一律 `name` 字符串（P4），悬空引用是编译错误。

### 2.2 五对象职责与关系

```
Gateway (1, 全局默认与实例级设置)
   └── Listener (n, 入口: 地址/端口/协议/TLS)
          ▲ 被引用
   Route (n, 匹配与转发规则) ──► Upstream (n, 后端与负载均衡)
     │
     └──(引用/内联)──► Policy (n, 可插拔策略: 限流/鉴权/CORS/改头…)
```

- v0 只有**一个隐式 Gateway 实例**：`Gateway` 对象可省略（全部取默认值）；显式声明时 `metadata.name` 必须为 `default`。多 Gateway 留给协议后续版本（字段已预留扩展空间）。
- `Route` 通过 `spec.listeners` 挂到一个或多个 Listener 上；`Policy` 可挂在 Listener / Route / 单条 rule 三个层级。

## 3. 对象规格

### 3.1 Gateway

实例级全局设置。v0 刻意保持最小集：

```yaml
apiVersion: esgw/v1alpha1
kind: Gateway
metadata:
  name: default
spec:
  accessLog:
    enabled: true
    format: json                # json | text；自定义格式模板 P1
    path: /var/log/esgw/access.log   # 省略 = stdout
  http:                         # 所有 HTTP 类 Listener 的默认值，Listener 可覆盖
    idleTimeout: 60s
    maxRequestHeadersKb: 60
    serverHeader: esgw          # 响应 server 头；"" 表示透传上游
  policies: []                  # 全局策略引用（作用于所有 HTTP Listener）
```

### 3.2 Listener

一个入口 = 地址 + 端口 + 协议 + （可选）TLS：

```yaml
apiVersion: esgw/v1alpha1
kind: Listener
metadata:
  name: web-https
spec:
  address: 0.0.0.0              # 默认 0.0.0.0；支持 IPv6 "::"
  port: 443
  protocol: HTTPS               # HTTP | HTTPS | TCP | TLS | UDP（见下表）
  tls:                          # 仅 protocol: HTTPS 时必填；TLS passthrough 禁止配置
    certificates:               # 多证书 = SNI 多域名（FR-1.2）
      - certFile: /etc/esgw/certs/example.com.crt
        keyFile: /etc/esgw/certs/example.com.key
      - ref: shop-example-com   # 引用管理面证书库中托管的证书（由控制台上传）
    minVersion: "1.2"           # 默认 1.2
    alpn: [h2, http/1.1]        # 默认值即此
    clientCA: /path/ca.crt      # 可选，启用 mTLS 客户端验证（P1）
  http:                         # 覆盖 Gateway.spec.http 同名字段
    http2: true                 # 默认 true（HTTPS）；HTTP 明文默认 false
    http3: false                # P2
  policies: []                  # Listener 级策略引用
```

| protocol | 语义 | 优先级 |
|---|---|---|
| HTTP | 明文 HTTP/1.1（可开 h2c） | P0 |
| HTTPS | TLS 终止 + HTTP，SNI 按证书 SAN 自动分派 | P0 |
| TCP | L4 透明转发（Route 用 `forward` 形态，§3.3.5） | P1（FR-1.6） |
| TLS | TLS passthrough，按 SNI 分流不解密 | P1 |
| UDP | UDP 转发 | P2 |

约束：`address:port` 组合在配置集内唯一（编译期检查）。HTTPS 证书的 SNI 匹配由编译器从证书 SAN/CN 生成 filter chain match，证书之间重叠是编译错误；`protocol: TLS` 不终止 TLS，SNI 来自其 L4 Route 的 `forward.sniHosts`。

### 3.3 Route

匹配与转发规则，是用户打交道最多的对象。

```yaml
apiVersion: esgw/v1alpha1
kind: Route
metadata:
  name: api
spec:
  listeners: [web-https]        # 必填，挂接目标
  hostnames: ["api.example.com"]  # 域名匹配，支持 "*.example.com" 前缀通配；省略 = 兜底 "*"
  policies: []                  # Route 级策略，作用于全部 rules
  rules:                        # 顺序即优先级：自上而下首个 match 命中生效（P4）
    - name: users               # 可选；rule 级 envoyPatch（§7.1，target: route/<name>）的定位锚，Route 内唯一
      match:
        path:
          prefix: /v1/          # exact | prefix | regex 三选一
        methods: [GET, POST]    # 可选
        headers:                # 可选，全部条件 AND
          - name: x-tenant
            exact: acme         # exact | regex | present 三选一
        queryParams:            # 可选，同 headers 形态
          - name: debug
            exact: "1"
      rewrite:                  # 可选
        pathPrefix: /           # 将命中的 prefix 替换为此值
        # path: /fixed          # 或整路径替换
        # regex: {pattern: "^/v1/(.*)$", substitution: "/\\1"}
        host: user.internal     # 改写发往上游的 Host
      backends:                 # 与 redirect / directResponse 三选一
        - upstream: user-svc    # 引用 Upstream.name
          weight: 90            # 权重分流（灰度）；单后端可省略
        - upstream: user-svc-canary
          weight: 10
      timeout: 15s              # 请求总超时；0s = 不限（默认 15s）
      retry:
        attempts: 2
        perTryTimeout: 2s
        on: [5xx, connect-failure, reset]   # 枚举映射 Envoy retry_on
      policies: [ratelimit-basic]           # rule 级策略
    - match: {path: {prefix: /}}
      redirect: {scheme: https, code: 301}  # redirect 形态
      # directResponse: {status: 404, body: "not found"}  # 直接响应形态
```

要点：

1. **匹配语义**：`hostnames` 先选中 Route（同 Listener 下多个 Route 的 hostname 重叠时，精确域名优先于通配，通配优先于兜底；同精度冲突是编译错误），rules 再顺序匹配。这条两级规则是协议里唯一的隐式优先级，需在文档中用一句话讲清："**先挑域名，再从上往下**"。
2. 一条 rule 的动作三选一：`backends` / `redirect` / `directResponse`。
3. `retry.on` 枚举 v0 支持：`5xx`、`gateway-error`、`connect-failure`、`reset`、`retriable-4xx`；默认不重试（显式声明才开启，避免非幂等请求意外重试）。
4. rule 可带可选 `name`（Route 内唯一，字符集同 `metadata.name`）：它是 rule 级 `envoyPatch`（§7.1，`target: route/<name>`）的定位锚；无 `name` 的 rule 不可被 rule 级 patch 定位（编译层未决事项 C1 决议，2026-07-19）。`name` 不影响匹配语义与顺序。

#### 3.3.5 L4 形态（P1，protocol: TCP/TLS/UDP 的 Listener）

L4 无 HTTP 匹配语义，Route 退化为 `forward` 单字段（TLS passthrough 可按 `sniHosts` 分流）：

```yaml
kind: Route
metadata: {name: mysql}
spec:
  listeners: [mysql-tcp]
  forward:
    upstream: mysql-primary
    # sniHosts: ["db.example.com"]   # 仅 protocol: TLS 的 listener 可用
```

`rules` 与 `forward` 互斥；挂接 L4 Listener 的 Route 出现 `rules` 是编译错误（反之亦然）。TCP/UDP Listener 必须恰好挂一条 forward Route；TLS Listener 可挂多条，但每条必须提供至少一个 `sniHosts`，同一 Listener 内大小写归一后不得重复。TLS 是原始字节透传，不配置 Listener `tls` 终止字段。L4 Listener/Route 禁止挂仅对 HTTP 生效的 Policy。

### 3.4 Upstream

后端服务定义。端点来源三选一：

```yaml
apiVersion: esgw/v1alpha1
kind: Upstream
metadata:
  name: user-svc
spec:
  # ── 端点来源（三选一）──
  endpoints:                    # ① 静态地址列表（FR-6.4，P0）
    - address: 10.0.0.1
      port: 8080
      weight: 1                 # 可选
  # dns:                        # ② 域名解析（FR-6.4，P0）
  #   hostname: user.internal
  #   port: 8080
  #   resolution: logical       # logical(默认,LOGICAL_DNS) | strict(STRICT_DNS)
  # kubernetesService:          # ③ k8s Service 引用（FR-6.3，P2；仅 k8s 环境可用）
  #   namespace: default
  #   name: user
  #   port: 8080

  loadBalancer:
    policy: roundRobin          # roundRobin(默认) | leastRequest | random | ringHash | maglev
    hashOn:                     # 仅 ringHash/maglev 需要
      - header: x-user-id

  healthCheck:                  # 可选；主动健康检查（FR-1.2）
    http:
      path: /healthz
      expectedStatuses: [200]   # 默认 [200]
    # tcp: {}                   # 或 TCP 探活
    interval: 10s
    timeout: 2s
    healthyThreshold: 2
    unhealthyThreshold: 3

  tls:                          # 可选；对上游启用 TLS
    enabled: true
    sni: user.internal          # 默认取 dns.hostname / rewrite.host
    caFile: /etc/ssl/ca.crt     # 省略 = 系统 CA
    insecureSkipVerify: false

  connection:
    connectTimeout: 5s          # 默认 5s
    http2: false                # 对上游使用 h2（gRPC 后端置 true）
    maxConnections: 1024        # 熔断参数（P1，映射 circuit_breakers）
    maxPendingRequests: 1024
```

k8s 环境下 `kubernetesService` 由 M-DISCO 提供候选与 EDS 动态端点（架构 A4：编译层只见统一的端点来源接口，对 k8s 无感知）。

### 3.5 Policy

可插拔策略，一个对象承载**一种**策略类型（`spec` 下有且仅有一个类型键，编译期校验）：

```yaml
apiVersion: esgw/v1alpha1
kind: Policy
metadata:
  name: ratelimit-basic
spec:
  rateLimit:                    # 本地令牌桶限流（P1，FR-1.3）
    requests: 100
    unit: minute                # second | minute | hour
    burst: 20                   # 默认 = requests
    key: clientIP               # clientIP(默认) | header:<name> —— 限流维度
    maxKeys: 10000              # 动态 key 桶 LRU 容量；默认 10000，范围 1..100000
```

v0/v1 策略类型清单（与 FR-1.3 对齐）：

| 类型键 | 能力 | 关键字段 | 优先级 |
|---|---|---|---|
| `headerModifier` | 请求/响应头增删改 | `request/response.{set,add,remove}` | P0 |
| `cors` | CORS | `allowOrigins`（支持通配）、`allowMethods`、`allowHeaders`、`allowCredentials`、`maxAge` | P1 |
| `rateLimit` | 本地限流（单实例令牌桶） | 见上例；分布式限流为后续版本 | P1 |
| `jwt` | JWT 校验 | `issuer`、`audiences`、`jwks.{uri,file}`、`forwardPayloadHeader`、`optional` | P1 |
| `extAuth` | 外部鉴权 | `grpc.address` 或 `http.{address,pathPrefix,caFile,insecureSkipVerify}`、`failOpen`、`disabled`（局部关闭，见规则 3a） | P1 |
| `ipAccess` | IP 黑白名单 | `allow[]` / `deny[]`（CIDR） | P1 |
| `basicAuth` | Basic 认证 | `users`（htpasswd 文件或内联哈希） | P2 |

**挂接与生效规则**：

1. 挂接点四级：`Gateway.spec.policies` → `Listener.spec.policies` → `Route.spec.policies` → `rule.policies`。
2. `policies` 列表元素既可是字符串（引用 Policy 名，复用场景），也可是内联匿名对象（一次性场景，形态同 `Policy.spec`）：

```yaml
policies:
  - cors-default                # 引用
  - headerModifier:             # 内联
      response:
        set: {x-frame-options: DENY}
```

3. **同类型冲突：就近覆盖（closest wins），整体替换不做深合并**。rule 级 > Route 级 > Listener 级 > Gateway 级。v0 用最简单可预测的语义，深合并留给后续版本按真实反馈决定。

   3a. **局部关闭**：不引入通用 `disable` 机制，各策略类型自带显式"关"字段，配合就近覆盖即可为个别路径豁免——`jwt: {optional: true}`、`extAuth: {disabled: true}`（映射 Envoy `ExtAuthzPerRoute.disabled`）。典型用法：Route 级挂全站鉴权，免鉴权的 rule 以内联匿名对象就近覆盖（见 §8.2 登录免 JWT 的同构写法）。
4. 不同类型策略执行顺序由编译器固定（ipAccess → cors → jwt/extAuth/basicAuth → rateLimit → headerModifier），用户不可排序——可预测性优先。cors 必须先于鉴权类策略：浏览器 CORS preflight（OPTIONS）不携带凭证，若先过 jwt/extAuth 会被 401 拦截，导致"启用 JWT 的接口 CORS 必坏"的经典故障（Envoy Gateway 等实现同此顺序）。

## 4. 通用字段约定

| 约定 | 规则 |
|---|---|
| 时长 | Go duration 字符串：`5s`、`1m30s`、`500ms` |
| 大小 | 显式带单位后缀的字段名（如 `maxRequestHeadersKb`），不做 `10Mi` 式解析 |
| 布尔默认 | 所有布尔字段默认 false，除非规格中标注（如 HTTPS 的 `http2` 默认 true） |
| 枚举 | camelCase 字符串，编译期强校验 |
| 名称引用 | 一律 `metadata.name`；跨 kind 不需要前缀（引用字段本身已表意，如 `upstream:`、`listeners:`） |

## 5. 与 Envoy 模型的映射（编译产物概览）

映射细节属编译层文档（[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md)），此处给出对应关系以证明"抽象与 Envoy 存在自然映射、无复杂语义变换"：

| 协议对象 | Envoy 资源 |
|---|---|
| Gateway | bootstrap 骨架、access log 默认、HCM 通用参数 |
| Listener | `envoy.config.listener.v3.Listener`（含 filter chains、TLS transport socket / SDS） |
| Route（HTTP） | `RouteConfiguration` 内的 `VirtualHost`（hostnames）与 `Route`（rules，保序） |
| Route（L4 forward） | `tcp_proxy` / `udp_proxy` filter 配置 |
| Upstream | `Cluster` + `ClusterLoadAssignment`（static/EDS）、health check、transport socket |
| Policy | HTTP filter（HCM filter chain）+ `typed_per_filter_config`（rule/Route 级覆盖） |

用户可随时通过 FR-2.3"查看编译产物"导出上述映射结果，用于学习与排障。

## 6. 版本化与演进（FR-1.5）

| 规则 | 内容 |
|---|---|
| 版本标识 | `apiVersion: esgw/v1alpha1`；alpha → beta → 稳定（`v1`） |
| alpha 阶段 | 允许破坏性变更，但每次变更提供迁移说明；M0/M1 期间迭代 |
| v1 起 | 同 major 内只做加法（新增可选字段/枚举值/策略类型）；破坏性变更必须升 major 并提供自动迁移工具 |
| 多版本共存 | 管理面同时接受最近两个版本，内部统一转换为最新版后编译（转换层在协议包内） |
| 严格解析 | 未知字段报错（P6）；因此新增字段不会被旧版本静默吞掉，而是显式提示升级 |
| 第三方可实现 | 协议规范 + JSON Schema 独立发布（FR-1.7，M2），Schema 由协议 Go 类型生成，单一事实来源 |

## 7. Escape Hatch（FR-1.4）

两级粒度（承接架构 §6 方向），表达力上限 = Envoy 本身：

### 7.1 对象级 `envoyPatch`

任何对象的 `spec` 都接受可选 `envoyPatch` 列表，作用于**该对象编译出的 Envoy 资源**：

```yaml
kind: Upstream
metadata: {name: user-svc}
spec:
  endpoints: [{address: 10.0.0.1, port: 8080}]
  envoyPatch:
    - target: cluster           # 该对象产出的资源类别：cluster | listener | virtualHost | route ...
      op: merge                 # merge = JSON Merge Patch (RFC 7386)
      value:
        upstream_connection_options:
          tcp_keepalive: {keepalive_time: 300}
    - target: cluster
      op: jsonPatch             # RFC 6902，精确操作
      value:
        - {op: replace, path: /dns_lookup_family, value: V4_ONLY}
```

- `target` 的合法取值由对象 kind 决定（C1 决议定表，2026-07-19）：Listener→`listener`/`secret`；Route→`virtualHost`/`route`（rule 级须写作 `route/<ruleName>`，定位带 `name` 的 rule，见 §3.3 要点 4）；Upstream→`cluster`/`endpoints`；Gateway→`bootstrap` 不允许（C2 留 M1）。patch 施加于编译完成之后、校验之前（顺序细节见编译层文档）。
- patch 后的资源仍要过全部校验（PGV + envoy validate），写坏 = 发布被拦截，而非运行时炸。

### 7.2 顶层原生资源 `EnvoyResources`

整段内嵌原生 Envoy 资源，与编译产物合并：

```yaml
apiVersion: esgw/v1alpha1
kind: EnvoyResources
metadata:
  name: custom-lua
spec:
  resources:
    - "@type": type.googleapis.com/envoy.config.cluster.v3.Cluster
      name: legacy-cluster
      connect_timeout: 3s
      type: STRICT_DNS
      load_assignment: {...}
  # allowOverride: false        # 默认 false：与编译产物重名 = 编译错误；true = 替换
```

这同时是 FR-2.1 专家模式（整套原生 static 配置）的协议载体之一——专家可以把配置几乎全部写进 `EnvoyResources`，与抽象对象自由混用（FR-2.2 的实现基础）。

**使用引导**：UI 与文档将 escape hatch 标注为高级功能；每次使用在发布流中显式提示"此对象包含原生 patch"。长期看，高频出现的 patch 模式是协议下一版本吸收为一等字段的信号来源。

## 8. 纸面演练（出稿门槛验证）

### 8.1 S1：单机多域名 TLS 反代（Nginx 替代主场景）

需求：一台 VPS，`www.example.com` 与 `blog.example.com` 两个域名 TLS 终止，分别转发到本机 3000/4000 端口，HTTP 全量跳转 HTTPS。

```yaml
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: https}
spec:
  port: 443
  protocol: HTTPS
  tls:
    certificates:
      - {certFile: /etc/esgw/certs/www.crt,  keyFile: /etc/esgw/certs/www.key}
      - {certFile: /etc/esgw/certs/blog.crt, keyFile: /etc/esgw/certs/blog.key}
---
kind: Listener
apiVersion: esgw/v1alpha1
metadata: {name: http}
spec: {port: 80, protocol: HTTP}
---
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: www}
spec:
  listeners: [https]
  hostnames: [www.example.com]
  rules:
    - match: {path: {prefix: /}}
      backends: [{upstream: www-app}]
---
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: blog}
spec:
  listeners: [https]
  hostnames: [blog.example.com]
  rules:
    - match: {path: {prefix: /}}
      backends: [{upstream: blog-app}]
---
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: http-redirect}
spec:
  listeners: [http]
  rules:
    - match: {path: {prefix: /}}
      redirect: {scheme: https, code: 301}
---
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: www-app}
spec:
  endpoints: [{address: 127.0.0.1, port: 3000}]
---
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: blog-app}
spec:
  endpoints: [{address: 127.0.0.1, port: 4000}]
```

**结论**：7 个对象、零 escape hatch、无一字段需要 Envoy 知识；对照等价 nginx 配置（2 个 server 块 + certbot 产物）复杂度持平，而 SNI 多证书无需手工配 server_name 与证书对应关系。✅

### 8.2 S2：API 网关（路由/重写/限流/JWT/CORS/超时重试）

需求：`api.example.com` 统一入口；`/users/*` → user 服务（灰度 10% 到 canary）、`/orders/*` → order 服务（gRPC）；全站 JWT 鉴权 + CORS；登录接口免鉴权且单独限流；路径前缀剥离。

```yaml
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: api}
spec:
  listeners: [https]
  hostnames: [api.example.com]
  policies: [jwt-main, cors-web]        # Route 级：默认全部规则生效
  rules:
    - match: {path: {exact: /auth/login}, methods: [POST]}
      backends: [{upstream: auth-svc}]
      policies:
        - jwt-off                        # 就近覆盖：登录免 JWT
        - rateLimit: {requests: 10, unit: minute, key: clientIP}   # 内联限流
    - match: {path: {prefix: /users/}}
      rewrite: {pathPrefix: /}
      backends:
        - {upstream: user-svc, weight: 90}
        - {upstream: user-svc-canary, weight: 10}
      timeout: 10s
      retry: {attempts: 2, perTryTimeout: 2s, on: [5xx, connect-failure]}
    - match: {path: {prefix: /orders/}}
      rewrite: {pathPrefix: /}
      backends: [{upstream: order-svc}]
      timeout: 30s
---
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: jwt-main}
spec:
  jwt:
    issuer: https://auth.example.com
    audiences: [api.example.com]
    jwks: {uri: https://auth.example.com/.well-known/jwks.json}
    forwardPayloadHeader: x-jwt-payload
---
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: jwt-off}
spec:
  jwt: {optional: true}                  # 同类型就近覆盖 → 该 rule 实际关闭强制校验
---
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: cors-web}
spec:
  cors:
    allowOrigins: ["https://*.example.com"]
    allowMethods: [GET, POST, PUT, DELETE]
    allowHeaders: [authorization, content-type]
    maxAge: 24h
---
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: order-svc}
spec:
  dns: {hostname: order.internal, port: 9090}
  connection: {http2: true}              # gRPC 后端
  healthCheck: {http: {path: /healthz}, interval: 10s, timeout: 2s,
                healthyThreshold: 2, unhealthyThreshold: 3}
# user-svc / user-svc-canary / auth-svc 同构，略
```

**结论**：FR-1.2 + FR-1.3 全部能力（域名/路径/方法匹配、前缀重写、权重灰度、超时重试、JWT、CORS、限流、健康检查、gRPC 上游）均有一等表达；"登录免鉴权"这类典型例外通过就近覆盖自然表达，未触发 escape hatch。✅

**演练暴露并已吸收的取舍**：① `jwt.optional` 承担"局部关闭"语义，避免引入额外的 `disable: true` 通用机制（留观后续版本）；② 权重灰度放在 `backends[].weight` 而非独立 TrafficSplit 对象——保持名词数量不增。

## 9. 竞品模型对照（RK1 核对）

| 能力 | nginx | Caddy | Traefik | Gateway API | **本协议** |
|---|---|---|---|---|---|
| 概念数（核心） | server/location/upstream + 大量指令 | site/handler | router/service/middleware + IngressRoute | Gateway/HTTPRoute/BackendRef + 6 种 Policy 附着 | 5 名词 |
| 域名+路径匹配 | server_name + location（隐式优先级复杂） | 一等 | 规则表达式字符串 | 一等 | 一等，顺序显式（P4） |
| 权重灰度 | split_clients 变通 | 一等 | 一等 | 一等 | `backends[].weight` |
| TLS/SNI 多证书 | 手工 server 块对应 | 自动 | 一等 | 一等 | 证书 SAN 自动分派 |
| 超时/重试 | 指令分散 | 一等 | 一等 | v1.1+ | rule 级一等 |
| 限流/JWT/CORS | 部分商业版/三方模块 | 插件 | middleware | 无（各实现扩展） | Policy 一等 |
| L4 | stream 块 | 有限 | TCP/UDP router | TCPRoute/UDPRoute | Listener protocol + forward |
| escape hatch | 无需（本体即底层） | 无 | 无（能力受限即受限） | 各实现私有注解 | 两级，上限=Envoy |
| 平台依赖 | 无 | 无 | 倾向容器生态 | **强绑定 k8s** | 无 |

结论：名词数量与 Traefik（3）、Gateway API（3+n）同量级，低于 nginx 的指令面；关键差异化是**显式顺序匹配 + 全量策略一等公民 + 有底的 escape hatch**。未发现某竞品有而本协议无法表达（含 escape hatch）的常见网关能力。

## 10. v0 明确不做（防蔓延清单）

流量镜像、请求缓冲/缓存、WASM/Lua 一等字段（走 escape hatch）、分布式限流、OIDC 终止（区别于 JWT 校验）、主动/被动混合异常剔除（outlier detection，P1 观察后加）、多 Gateway 实例、协议级 include/模板机制（组合复用由文件拆分 + git 承担）。

## 11. 未决事项

| # | 事项 | 计划 |
|---|---|---|
| PD1 | `apiVersion` 组名（依赖 D1 项目命名） | D1 决议后全局替换，alpha 期无兼容负担 |
| PD2 | 同类型策略深合并需求是否真实存在 | v1beta1 前收集反馈 |
| PD3 | 证书托管引用（`ref:`）与文件路径两种形态是否都保留 | M1 控制台证书管理设计时终审 |
| PD4 | `hostnames` 通配是否需要中缀（`a.*.b`） | 默认不支持，Envoy route 层亦不鼓励；观察需求 |

## 12. M0 验证衔接

本规范即 M0 编译器原型的输入规格。M0 验收（对应可行性 §6）：S1/S2 两组 YAML 经编译器产出 static YAML，通过 `envoy --mode validate` 并跑通真实流量；escape hatch 原型（§7 两种形态各一例）合成正确。编译器设计见 [`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md)。

# 260717-5 k8s 环境感知模块设计（M-DISCO Design）

- **项目**: envoy-standalone-gateway
- **上游文档**: [`260715-2-overall-architecture.md`](260715-2-overall-architecture.md)（A4 原则、M-DISCO 模块、§5 T3 拓扑）、[`260716-1-gateway-config-protocol-v0.md`](260716-1-gateway-config-protocol-v0.md)（§3.4 `kubernetesService` 端点来源）、[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md)（`EndpointSource` 契约、命名规约、EDS 快速通道约定）、[`260715-1-feasibility-analysis.md`](260715-1-feasibility-analysis.md)（§2.6 可行性结论）
- **文档状态**: 初稿（Draft）
- **日期**: 2026-07-17
- **性质**: 模块详细设计。覆盖架构 §8 清单第 7 项：环境探测与启停、Service/Endpoint 发现、与配置域/编译层/下发层的集成契约、T3 部署形态。对应需求 FR-6.1~6.3 与 FR-5.3。

---

## 1. 定位与边界

### 1.1 职责

M-DISCO 是**可选增强**模块（架构 A4），只做三件事：

| 职责 | 对应需求 | 优先级 | 消费方 |
|---|---|---|---|
| 环境探测与模块启停 | FR-6.1 | P1 | M-CORE（生命周期编排） |
| Service/Endpoint 发现与本地缓存，提供 upstream 候选查询 | FR-6.2 | P1 | M-API → 控制台（[`260717-3-console-api-design.md`](260717-3-console-api-design.md)） |
| `kubernetesService` Upstream 的端点解析与运行期跟踪（EDS） | FR-6.3 | P2（设计本轮定稿，实现可分期） | M-COMPILE / M-DELIVER |

### 1.2 边界（明确不做）

| # | 边界 | 依据 |
|---|---|---|
| 1 | 不监听、不生成任何 CRD / Ingress / Gateway API 资源；k8s 对象对 M-DISCO **只读** | 需求 N2 非目标；配置载体永远是抽象协议文件（A5） |
| 2 | 不做 mesh / sidecar 语义（流量劫持、双向 mTLS 注入等） | 需求 N1 |
| 3 | 不改变配置模型：k8s Service 仅是 `Upstream` 三种端点来源之一，协议在裸机上语义完整 | 协议 §3.4、原则 P2 平台中立 |
| 4 | 不反向渗透：编译层、下发层、配置域对 client-go 零感知 | A4；编译层 §7 契约 |
| 5 | 非 k8s 环境**功能完整零影响**：模块停用后，除 `kubernetesService` 编译报明确错误外无任何行为差异 | A4 |

第 4 条在代码层面的落实：client-go 只允许出现在 `internal/disco` 包内，CI 以 import 约束守住（`internal/compile`、`internal/deliver`、`internal/conf` 禁止 import client-go）。

模块内部子组件与对外接口面：

```
M-DISCO (internal/disco，可选启用)
┌────────────────────────────────────────────────────────────┐
│ Detector         探测 + 权限自检 → 模块状态机（§2）            │
│ DiscoveryCache   informer(Service + EndpointSlice) → 本地索引 │
│ EndpointTracker  k8s 引用订阅 → 防抖合并 → CLA 重算（§5）      │
└───────┬───────────────────┬────────────────────┬───────────┘
        │ 候选查询接口        │ EndpointSource      │ EDS 快速通道
        ▼                   ▼                     ▼
      M-API              M-COMPILE             M-DELIVER
   (控制台 260717-3)  (编译层 §2/§6 契约)    (下发层 260717-1 §3)
```

## 2. 环境探测与启停

### 2.1 探测流程（FR-6.1）

启动时按序检查，全部通过才进入 k8s 模式：

| 步骤 | 信号 | 失败含义 |
|---|---|---|
| 1 | `KUBERNETES_SERVICE_HOST` / `KUBERNETES_SERVICE_PORT` 环境变量存在 | 不在 k8s Pod 内（该变量由 k8s 自动注入） |
| 2 | ServiceAccount token 文件 `/var/run/secrets/kubernetes.io/serviceaccount/token` 存在 | Pod 未挂 SA（如 `automountServiceAccountToken: false`） |
| 3 | `rest.InClusterConfig()` 构造 client 成功，且对 `services`、`endpointslices` 各一次 `limit=1` 的 list 调用成功 | API server 不可达 / **RBAC 权限不足**（403 时日志明确指出缺失的 resource/verb，对照 §3.2 清单） |

依据：环境变量与 token 文件是官方推荐的 in-cluster 判定信号；第 3 步直接对目标资源发真实请求做**权限自检**，而非 `SelfSubjectAccessReview`——最小 RBAC 下 SSAR 自身的 create 权限未必被授予，直接 list 得到的就是真实答案，且顺带验证连接。

### 2.2 模块状态机与降级路径

| 状态 | 进入条件 | 系统行为 |
|---|---|---|
| `disabled` | 不在 k8s 内（auto 探测失败）或 `enabled: off` | 模块不启动；候选接口返回"未启用"；`kubernetesService` 编译报明确错误（§4.2）；其余功能零影响 |
| `active` | 探测 + 权限自检通过，informer `WaitForCacheSync` 完成 | 全功能 |
| `degraded` | 在 k8s 内但权限不足 / API 不可达 / 缓存未就绪（启动期）；或运行期 watch 长期失联 | 候选查询返回降级标记与原因；`kubernetesService` 编译报错附原因；失联期间**保留**最后已知端点（不主动清空，交由 Envoy 健康检查剔除），watch 恢复后 informer relist 自动对账 |

启动顺序：M-DISCO 以 `degraded`（缓存未就绪）状态参与 M-CORE 装配，`WaitForCacheSync`（超时 30s）完成后转 `active`，超时保持 `degraded`——esgw 启动不被 k8s API 可用性阻塞。

启停配置（esgw 主配置，M-CORE 装配注入）：

```yaml
disco:
  enabled: auto        # auto(默认) | on | off
  namespaces: []       # 空 = 全集群；非空 = 仅列出的 namespace
  edsDebounce: 200ms   # 端点变化合并窗口（§5.4）
  resyncInterval: 10m  # informer 周期对账
```

- `auto`：按 §2.1 探测，失败降级（WARN 日志一次，不刷屏）——裸机用户零感知。
- `off`：强制停用。适用于"跑在 k8s 里但不想配 RBAC"的用户。
- `on`：强制启用，探测失败**启动报错退出**（fail-fast）。适用于 Helm 部署——RBAC 配错在 Pod 启动期即暴露，而非静默降级后用户对着空的候选下拉框困惑。

### 2.3 决策：探测结果启动时定型，运行期不热切换

| 选项 | 结论 |
|---|---|
| 运行期热切换（检测到 token 文件出现后动态启用等） | **不做**。环境变量与 token 注入发生在 Pod 创建时，"运行中变成 k8s"现实里不存在；热切换要求 `EndpointSource` 可热拔插、存量 Upstream 语义中途变化，复杂度与收益完全不成比例 |
| 配置变更（`auto→off`、`namespaces` 调整）的生效方式 | **重启 esgw 进程**。管理面重启不影响数据面流量（FR-5.6 / A3），代价可接受；部署文档明确标注 |

## 3. 服务发现

### 3.1 选型：client-go informer List-Watch Service + EndpointSlice

| 决策点 | 选择 | 依据 |
|---|---|---|
| 客户端机制 | client-go `SharedIndexInformer`（List-Watch） | 自带本地缓存、namespace 索引、断线 relist 重连，是 k8s 生态标准做法；手写 watch 循环在重对账与抖动处理上重复造轮子（可行性 §2.6 已定 client-go 方向） |
| 端点资源 | **EndpointSlice**，不 watch 旧 `Endpoints` API | EndpointSlice 自 k8s 1.21 GA，上游已弃用 Endpoints API；分片设计（默认每片 100 端点）使大 Service 的 watch 流量与更新开销按片计，远优于整体对象的 Endpoints |
| 集群版本区间 | k8s ≥ 1.21 | 跟随上游最近若干 minor 的支持策略（同 RK4 对 Envoy 的版本区间思路），下限即 EndpointSlice GA |
| 不 watch 的对象 | Pod、Node、Ingress、ConfigMap、Secret 等一律不碰 | 最小权限（§3.2）；Pod IP 与端口已由 EndpointSlice 携带 |
| Service ↔ 端点关联 | EndpointSlice label `kubernetes.io/service-name` | 官方约定键 |

Headless Service（`clusterIP: None`）**天然支持**：EDS 端点映射只依赖 EndpointSlice 的 Pod 地址，不依赖 clusterIP，v0 不做特殊处理（控制台展示语义见 KD5）。

### 3.2 watch 范围与 RBAC 最小权限

- `disco.namespaces` 为空 → 单 informer factory 全集群 watch，Helm 渲染 ClusterRole；
- 非空 → per-namespace informer，Helm 渲染对应 namespace 的 Role。

最小权限清单（FR-5.3 Helm chart 默认即此）：

```yaml
rules:
  - apiGroups: [""]
    resources: ["services"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["discovery.k8s.io"]
    resources: ["endpointslices"]
    verbs: ["get", "list", "watch"]
```

决策：**不申请 `namespaces` 权限**。控制台所需的 namespace 候选列表从 Service 缓存派生（去重）——空 namespace 里没有 Service，对"选 upstream"场景无意义；少一个权限点，RBAC 审查更好过。

### 3.3 `kubernetesService` 端口解析规则

协议 §3.4 的引用形态为 `{namespace, name, port}`（port 为数值）。解析算法（编译期 Resolve 与运行期跟踪共用同一份逻辑）：

1. `Service.spec.ports` 中找 `port == ref.port` 的项；找不到 → `ErrPortNotFound`（错误消息列出该 Service 全部可用端口，方便用户自查）。
2. 该项有 `name` → 在该 Service 所有 EndpointSlice 的 `ports[]` 中按同名取 endpoint 端口（k8s 控制面保证 EndpointSlice 的 port name 与 Service port name 一致；这同时覆盖 `targetPort` 为命名端口的情形）。
3. 该项无 `name`（仅单端口 Service 允许省略）→ 取 EndpointSlice 中唯一（无名）port 项。
4. EndpointSlice 无 ports 信息（罕见窗口，如端点全未就绪）→ 回退该项 `targetPort` 数值 → 再回退 `ref.port` 本身。

端点地址：EndpointSlice 每个 ready endpoint 的 `addresses` 全部展开为独立端点（兼容 dual-stack）。

### 3.4 本地缓存与查询接口

informer store 即缓存（规模假设：数千 Service / 万级端点，内存增量在 NFR-2 <150MB 预算内，可行性 §2.7）。对模块外暴露只读查询面：

```go
// DiscoveryQuery：供 M-API（控制台候选）与编译期校验使用
type DiscoveryQuery interface {
    ListNamespaces() []string                           // Service 缓存派生（§3.2）
    SearchServices(ns, keyword string, limit int) []ServiceCandidate
    GetService(ns, name string) (*ServiceDetail, error) // 含 ports 与当前就绪端点数
    SnapshotEndpoints(ref KubernetesServiceRef) ([]Endpoint, error)
}

type ServiceCandidate struct {
    Namespace, Name string
    Ports           []ServicePort // {name, port, targetPort, protocol}
    ReadyEndpoints  int           // 就绪端点数（列表展示用）
}
```

## 4. 与配置域 / 控制台集成

### 4.1 upstream 候选查询（FR-6.2）

- `DiscoveryQuery` 由 M-API 包装为 REST（契约见 [`260717-3-console-api-design.md`](260717-3-console-api-design.md)）。控制台 Upstream 表单交互：选 namespace → 关键字搜索 Service → 下拉选择端口（`ServiceDetail.Ports`）→ 回填 `kubernetesService` 三字段。
- 除动态引用外，控制台提供"快照为静态地址"选项：调 `SnapshotEndpoints` 把当前端点一次性写入 `endpoints` 列表——适用于想冻结配置、不跟踪变化的用户。两种形态协议天然都支持（§3.4 端点来源三选一），M-DISCO 只提供数据。
- 模块 `disabled` / `degraded` 时候选接口返回明确状态与原因，控制台隐藏或置灰 k8s 选项卡——非 k8s 用户体验零差异（A4）。

### 4.2 `kubernetesService` 解析与校验行为矩阵

| 场景 | 编译期（F2 链接 / F3 构建） | 运行期 |
|---|---|---|
| 模块 `disabled`（非 k8s / off） | 编译错误："当前环境未启用 k8s 服务发现" | — |
| 模块 `degraded`（权限不足 / 缓存未就绪） | 编译错误，消息附缺失权限或就绪状态 | — |
| Service 不存在 | 编译错误 `ErrServiceNotFound`，SourceRef 指向 `spec.kubernetesService` | （发布后删除的兜底）见 §5.5 |
| 端口不匹配 | 编译错误 `ErrPortNotFound`，消息列出可用端口 | 运行期 Service 改端口致失配 → 清空 CLA + 事件告警 |
| Service 存在、零就绪端点 | **编译通过**，初始 CLA 为空 | 端点出现后自动补入 |
| 正常 | 编译通过，初始 CLA = 当前就绪端点 | 持续跟踪（§5.2） |

依据：「Service 不存在 / 端口失配」是配置错误，必须在发布前拦截（NFR-3"非法配置不出管理面"）；「零就绪端点」是合法运行态（服务缩容到 0），拦截它会误伤正常发布。错误经编译层 `CompileError`（Stage=link）回指 YAML 位置（编译层 §4），供 UI 行内标注。

## 5. 与编译层 / 下发层集成

### 5.1 编译期：实现 `EndpointSource`

接口由编译层定义、M-DISCO 实现（编译层 §2/§6，消费方定义接口，A4）：

```go
// internal/compile（编译层持有；M-DISCO 注入实现，nil = 无 k8s 能力）
type EndpointSource interface {
    ResolveKubernetesService(ref KubernetesServiceRef) ([]Endpoint, error)
}
```

- **初始 CLA 必须编译期填充**：新 snapshot 发布后 informer 不会主动再发事件，若 CLA 留空，端点要等下一次真实变化才出现。Resolve 从已同步的 informer 缓存读取，O(1)，不阻塞编译。
- 产物形态随下发模式分化（编译层 §1 要点 2"形态化"在本模块视角的细化）：
  - **xDS 模式**：Cluster `us/<upstream>` 为 EDS 类型；CLA 资源名 = Cluster 名（go-control-plane Snapshot 的 Endpoints key 即 ClusterName，与编译层 §5 命名规约一致），初始内容 = Resolve 结果。
  - **static 模式**：无 EDS 通道，编译为"**发布时刻端点快照**"内联 STATIC cluster，发布流给出 Warning（"static 模式下 k8s 端点为发布时刻快照、不跟踪变化；动态跟踪请用 xDS 模式"）。语义诚实、功能可用，不阻塞 static 场景使用 k8s 后端。

### 5.2 运行期：EDS 快速通道

契约遵循下发层 [`260717-1-deliver-layer-design.md`](260717-1-deliver-layer-design.md) §3（EDS 快速通道）与编译层 §7（"运行期端点变化不经过 Compile"）：

```
发布生效（260717-2 发布流"发布"步骤后）
  → M-DISCO.SyncTrackedServices(refs)   // refs 由发布流从 ConfigSet 提取；diff 出新增/移除的跟踪集合
  → informer 事件（Service / EndpointSlice 变化）
  → 防抖窗口合并（§5.4），从缓存全量重算受影响 CLA
  → M-DELIVER.ApplyEndpointUpdates(map[clusterName]*CLA)   // 一次调用批量携带，单次 snapshot 版本递增
  → 仅触发 EDS 推送；LDS/RDS/CDS/SDS 不动
```

- M-DISCO 的运行期更新只作用于当前 snapshot 版本：被取消跟踪的引用，其 CLA 随下次全量发布自然消失（下发层以编译产物整体换版，编译层 §7）。
- static 模式下 EndpointTracker 不启动（无 EDS 通道可言）。
- 版本号与 snapshot 递增规则由下发层持有（260717-1 §3），M-DISCO 只提交内容。

### 5.3 CLA 映射规则

```yaml
cluster_name: us/<upstream>
endpoints:                        # 单个 LocalityLbEndpoints：v0 不引入 locality/priority 分层（无拓扑路由）
  - lb_endpoints:
      - endpoint:
          address: {socket_address: {address: 10.244.1.7, port_value: 8080}}
        health_status: HEALTHY
        load_balancing_weight: 1
```

| 规则 | 决策 | 依据 |
|---|---|---|
| 就绪筛选 | v0 **仅发布 `conditions.ready == true`** 的端点，状态恒 `HEALTHY` | 最简单可预测；`serving` / `terminating` 的精细语义列入 KD1 实测后定 |
| 地址展开 | endpoint 的 `addresses` 全部展开为独立 LbEndpoint | dual-stack 兼容 |
| 权重 | 恒 1，按端点数量天然均摊 | k8s Service 语义即等概率；负载均衡策略由协议 `loadBalancer` 在 Cluster 层表达 |
| 元数据 | 不写入 zone/node 等 filter metadata | 无消费者（不做拓扑路由），保持产物最小与确定性（编译层 §5） |

### 5.4 防抖与批量合并

| 规则 | 决策 |
|---|---|
| 合并窗口 | 事件只标记 dirty cluster，`edsDebounce`（默认 200ms）后统一处理；滚动更新时多片陆续变化的尖峰合并为一次推送 |
| 重算基准 | **从 informer 缓存全量重算** dirty cluster 的 CLA，不增量应用事件——防 watch 事件丢失/乱序导致的漂移；缓存即真源，与 informer resync 对账模型一致 |
| 无变化跳过 | 重算 CLA 与当前内容哈希一致则不调下发层，与"配置未变不触发下发"的确定性原则（编译层 §5）对齐 |
| 批量调用 | 同窗口内多个 dirty cluster 合并为一次 `ApplyEndpointUpdates` 调用，减少 snapshot 版本抖动 |

### 5.5 Service 删除的兜底行为

发布后 Service 被删除 → **清空对应 CLA**（空端点列表，Envoy 对该 cluster 返回 503）+ 产生告警事件（控制台可见；事件模型归 [`260717-4-state-collection-design.md`](260717-4-state-collection-design.md) 与 260717-3）。

依据：k8s 中 Service 删除后其 Pod IP 随时可能被其他工作负载复用，保留陈旧端点等于把流量发给未知 Pod——比明确的 503 更危险。同名 Service 重建后 watch 命中，端点自动恢复，配置无需任何变更。

## 6. 部署形态（T3）

与架构 §5 T3 行对齐，两种拓扑。**M-DISCO 本身与拓扑无关**（只依赖 API server 可达 + SA），差异在 xDS/admin 通道与 chart 参数。

### 6.1 T3a：同 Pod 双容器（首选）

```
Pod (Deployment，可多副本)
├── container: esgw    # 挂 SA；xDS :18000；控制台 :8080
└── container: envoy   # 官方发行版（A2）；admin 绑 127.0.0.1:9901（RK5）
```

- xDS 走 Pod 内 localhost，明文 gRPC 可接受（不跨节点）；admin API 绑 127.0.0.1，满足架构 §5 硬约束（RK5）。
- esgw 与 Envoy 1:1 绑定，扩缩容以 Pod 为单位——心智与运维最简单，作为 T3 默认形态。
- 已知代价：esgw 升级触发 Pod 滚动会同时重建 Envoy 容器；生产部署用多副本 + PDB 平滑（chart 提供）。

### 6.2 T3b：esgw 独立 Deployment 驱动多 Envoy Pod（P2）

- 对应 FR-3.6 多实例接入；xDS 跨 Pod 必须 mTLS（NFR-5，架构 §5 约束），证书发放方案随 260717-1 下发层设计。
- 此拓扑下 Envoy admin 不再 localhost：状态采集的安全暴露方案属 [`260717-4-state-collection-design.md`](260717-4-state-collection-design.md) 范畴（RK5 约束下需专门设计，如 mTLS 代理），本文不展开。
- M-DISCO 行为与 T3a 完全一致。

### 6.3 Helm chart 要点（FR-5.3，M2 交付）

| 要点 | 内容 |
|---|---|
| 无 CRD | 硬约束（FR-5.3、N2）；chart 只含原生资源 |
| 资源清单 | Deployment（T3a 双容器）、ServiceAccount、ClusterRole+Binding（`disco.namespaces` 为空时）或 Role+Binding（指定时，每 namespace 一份）、Service（控制台 + 网关端口）、ConfigMap（esgw 配置）、可选 PDB |
| 关键 values | `disco.enabled`（默认 auto）、`disco.namespaces`（默认 []）、`envoy.image.tag`（锁定支持区间，RK4）、`topology`（t3a/t3b，P2 预留） |
| RBAC 默认 | §3.2 最小清单原样渲染；不额外申请 secrets/configmaps 等权限 |
| 交付时机 | 架构 §7：Helm chart 随 M2，与 FR-6 的 P1 排期一致 |

## 7. 未决事项

| # | 事项 | 计划决策时机 |
|---|---|---|
| KD1 | EndpointSlice 条件语义精化：v0 仅发布 `ready==true` 端点；`serving`/`terminating` 条件下是否提前摘除（影响滚动更新平滑度，及与 Envoy panic 阈值的交互） | M2 k8s sprint 真实集群实测后 |
| KD2 | 拓扑感知路由（EndpointSlice `hints`、same-zone 优先）是否进入协议 `Upstream.loadBalancer` 扩展 | 观察真实需求，协议 v1beta1 前 |
| KD3 | 是否支持外部 kubeconfig（管理面在集群外、发现集群内 Service）；v0 仅 in-cluster | 出现真实需求时 |
| KD4 | 大集群规模（万级 EndpointSlice）下 informer 内存与初次 list 耗时压测；必要时引入 label selector 缩小 watch 面 | M2 压测 |
| KD5 | Headless Service 在控制台的展示与"快照为静态地址"语义确认（端点映射逻辑已天然支持，仅 UI/交互细节） | M2 控制台联调 |

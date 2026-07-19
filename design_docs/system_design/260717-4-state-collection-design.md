# 260717-4 状态采集与统计模型设计（M-STATE Design）

- **项目**: envoy-standalone-gateway
- **上游文档**: [`260715-2-overall-architecture.md`](260715-2-overall-architecture.md)（M-STATE 模块、A3 只读旁路、T1~T3 拓扑、RK5）、[`260715-1-feasibility-analysis.md`](260715-1-feasibility-analysis.md)（§2.4 数据源结论）、[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md)（§5 命名规约与 IR.Version、§7 `static.Render`）
- **文档状态**: 初稿（Draft）
- **日期**: 2026-07-17
- **性质**: 模块详细设计。覆盖架构 §8 清单第 6 项：Envoy admin API 采集调度、状态归一化模型、生效版本确认（FR-4.5 支撑）、统计时间序列与 Prometheus 透传。M1 状态视图与发布流闭环按此实现。

---

## 1. 职责与数据源

M-STATE 是管理面与数据面之间的**只读旁路**（A3）：它对 Envoy 的全部交互是 admin API 的 GET 请求，采集结果供控制台状态视图（FR-4.3）、统计视图（FR-4.4）与发布流生效确认（FR-4.5）消费。它不做任何写操作，不做进程控制（归 M-PROC），不在数据路径上。

### 1.1 admin API 端点清单

| 端点 | 内容 | 消费场景 | 采集频率档（§2） |
|---|---|---|---|
| `/ready` | Envoy 就绪状态 | 启动探测、数据面存活联动（供 M-PROC/T1 健康展示） | 探测档 |
| `/server_info` | Envoy 版本、state（LIVE/DRAINING/…）、uptime、hot restart epoch | 控制台"数据面信息"卡片 | 低频 |
| `/config_dump?include_eds` | 全量生效资源（含 EDS）+ 动态资源 `version_info` + bootstrap | 实时生效状态视图（FR-4.3）；生效版本确认（§4） | 低频 + 事件触发 |
| `/clusters?format=json` | cluster membership、endpoint 健康状态与失败标记、熔断水位 | endpoint 健康视图 | 中频 |
| `/stats?format=json` | counters / gauges / histograms（quantile 近似值）全量 | 统计时间序列来源（§5） | 高频 |
| `/stats/prometheus` | Prometheus 文本格式 | 原样透传（§5.4），M-STATE 自身不解析 | 按需（外部抓取） |
| `/certs` | 证书链、SAN、序列号、过期时间 | 证书视图与过期提示 | 低频 |

### 1.2 只读边界（硬性）

M-STATE **禁止**调用任何副作用端点，包括但不限于：`/quitquitquit`、`/drain_listeners`、`/healthcheck/fail|ok`、`/runtime_modify`、`/logging`、`/reset_counters`、`/cpuprofiler` 等。该约束写入 code review 清单与 admin client 的实现层（client 只构造 GET 请求，端点路径白名单化）。进程控制（hot restart 信号、启停）属 M-PROC；运行时参数修改不在产品功能面内。

### 1.3 admin 绑定约束（RK5 落地）

admin API 无鉴权，绑定地址由管理面在 bootstrap 骨架中固定生成（IR.Bootstrap 的 admin 段，见 [`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §1）：

```yaml
# T1/T2（同机）默认：unix socket，文件权限即访问控制，无端口暴露
admin:
  address:
    pipe: {path: /var/run/esgw/envoy-admin.sock}
# T3（同 Pod）或用户显式选择 TCP 时：
admin:
  address:
    socket_address: {address: 127.0.0.1, port_value: 9901}
```

- uds 为 T1/T2 默认：不占端口、无端口冲突、`0600` 文件权限使 RK5 缓解比"绑定 127.0.0.1 依赖网络栈信任"更彻底。
- M-STATE 是管理面内 admin API 的**唯一**消费者；对外（浏览器/CLI/外部采集器）一律经 M-API 暴露归一化后的只读视图，不原样转发 admin——唯一例外是 Prometheus 透传端点（§5.4）。
- 多 Envoy 节点（FR-3.6，P2）：v1 数据模型按单节点设计，结构体预留 `NodeID` 字段，扩展方式见未决事项 S6。

## 2. 采集调度

### 2.1 三类采集路径

```
              ┌─ 周期调度器（分级 ticker，§2.2）
M-STATE ──────┼─ 事件调度器（ExpectVersion 触发的确认快速通道，§4）
              └─ 按需拉取（控制台打开页面 refresh=true，singleflight 合并）
                          │
                          ▼
               admin client（uds / 127.0.0.1，GET 白名单，5s 超时）
                          │  ★ 串行 worker：同一时刻至多一个在途请求
                          ▼
        归一化 → 状态快照缓存 + 时间序列环形缓冲 + 确认事件分发
```

关键决策：

1. **采集请求串行化**。Envoy admin 端点运行在数据面主线程/工作线程的事件循环上，高频并发拉取（尤其 MB 级的 `config_dump?include_eds`）会给数据面添压。所有采集请求进入单一 worker 队列顺序执行——这是 A3"不在数据路径上"精神向资源占用维度的延伸。代价是单次采集周期变长（全端点一轮 < 2s，可接受）。
2. **按需拉取不绕过队列**，仅插队优先执行；同一端点的并发按需请求用 singleflight 合并为一次实际拉取。控制台多个页面同时打开不会放大采集压力。
3. **归一化后丢弃原始报文**。`config_dump?include_eds` 原始 payload 在大配置下可达 MB 级，解析为 §3 状态模型后即释放，不缓存原始 JSON（NFR-2 内存约束）。

### 2.2 调度参数表（默认值）

| 参数 | 默认 | 说明 |
|---|---|---|
| `stats.poll_interval` | 10s（可配 5~60s） | 即时间序列分辨率（§5.2） |
| `clusters.poll_interval` | 15s | endpoint 健康；跟随主动健康检查典型周期（10s）量级 |
| `config_dump.poll_interval` | 60s | 兜底对账；主路径是发布事件触发（§4） |
| `certs.poll_interval` | 5m | 过期信息低频 |
| `server_info.poll_interval` | 60s | 与 config_dump 同档调度 |
| `ready.probe_interval` | 启动期 1s → 稳态 10s | 启动期指 M-PROC 拉起 Envoy 后至首次 ready |
| `confirm.initial_interval` / `confirm.max_interval` | 500ms / 5s | 确认快速通道轮询，指数退避 |
| `confirm.timeout` | 30s（可配） | 生效确认超时，见未决事项 S2 |
| `request.timeout` | 5s | 单次 admin 请求总超时（含 stats 大 payload） |
| `failure.backoff` | 1s 起步指数退避，上限 60s | 单端点连续失败后的重试节奏 |

### 2.3 失败与降级语义

- 单端点采集失败：该端点进入失败退避（上表），其余端点不受影响。
- Envoy 整体不可达（连接失败）：状态快照整体标记 `Stale: true` 并保留 `LastSuccessAt`；控制台展示"数据来自 N 秒前"而非清空——排障场景下"最后一次生效状态"恰恰是最有价值的信息。
- M-STATE 对不可达**只展示、不行动**：进程重启/拉起决策属 M-PROC；告警系统 v1 不存在，控制台展示即全部。
- 日志节流：重复失败按指数间隔降级输出，避免 Envoy 长时间宕机时刷爆管理面日志。

## 3. 状态归一化模型

控制台消费的统一状态模型（FR-4.3）。模型按**协议对象视角**组织：Envoy 资源只是载体，用户在 UI 上看到的是自己的 `Listener`/`Route`/`Upstream` 及其运行状态。

### 3.1 Go 结构定义

```go
// internal/state
type DataPlaneState struct {
    NodeID      string
    Envoy       ServerInfo
    CollectedAt time.Time
    Stale       bool          // 上次采集失败后为 true，配合 LastSuccessAt 展示
    LastSuccessAt time.Time
    Listeners   []ListenerState
    Clusters    []ClusterState
    Routes      []RouteState
    Certs       []CertState
    Version     VersionStatus // §4 生效确认状态
}

type ServerInfo struct {
    Version         string        // Envoy 版本，供 NFR-6 兼容性展示
    State           string        // LIVE | DRAINING | PRE_INITIALIZING | ...
    Uptime          time.Duration
    HotRestartEpoch int           // static 模式 hot restart 观测辅助信号
}

type ListenerState struct {
    Name     string     // lis/web-https
    Address  string     // 0.0.0.0:443（用于 listener.* stats 归组，§5.1）
    Protocol string     // 反解自 filter chain 内容：HTTP | HTTPS | TCP | TLS | UDP
    RouteConfig string  // 关联的 rc/<listener>
    Owner    *ObjectRef // 反解归属，nil = 未归属
}

type ClusterState struct {
    Name      string          // us/user-svc
    Owner     *ObjectRef
    Endpoints []EndpointState // 来自 /clusters?format=json
}

type EndpointState struct {
    Address    string
    Port       uint32
    Health     HealthStatus // HEALTHY | UNHEALTHY | DRAINING | TIMEOUT | UNKNOWN
    Weight     uint32
    FailFlags  []string     // /clusters 的 failed_active_health_check 等原始标记，排障直出
}

type RouteState struct {
    Name         string        // rc/<listener>
    Owner        *ObjectRef    // 归属 Listener
    VirtualHosts []VHostState
}

type VHostState struct {
    Name    string   // vh/<route>
    Domains []string
    Owner   *ObjectRef // 归属 Route
}

type CertState struct {
    Name     string   // crt/<listener>/<n>（xDS）或 static 文件路径
    Owner    *ObjectRef
    Subject  string
    SANs     []string
    NotAfter time.Time
    DaysLeft int       // 过期提示（控制台黄色 <30d / 红色 <7d，阈值见 260717-3）
    Serial   string
}

type ObjectRef struct {
    Kind string // Listener | Route | Upstream
    Name string
}
```

### 3.2 归属反解：命名规约为唯一权威路径

归属反解完全依据编译层命名规约（[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §5，该文档已明确"前缀短因其出现在 stats 名中（M-STATE 反解归属靠它）"——本节即该契约的消费方）：

| Envoy 资源 | 命名形态 | 归属协议对象 |
|---|---|---|
| Listener | `lis/<name>` | `Listener/<name>` |
| RouteConfiguration | `rc/<name>` | `Listener/<name>`（其内 vhost 再归属 Route） |
| VirtualHost | `vh/<name>` | `Route/<name>` |
| Cluster / CLA | `us/<name>` | `Upstream/<name>` |
| Secret | `crt/<listener>/<n>` | `Listener/<listener>` |
| 无规约前缀 | — | **未归属**（用户原生直写 / `EnvoyResources` 引入），UI 单列分组 |

```go
func resolveOwner(resourceName string) *ObjectRef {
    switch {
    case strings.HasPrefix(resourceName, "lis/"): return &ObjectRef{"Listener", after(resourceName)}
    case strings.HasPrefix(resourceName, "rc/"):  return &ObjectRef{"Listener", after(resourceName)}
    case strings.HasPrefix(resourceName, "vh/"):  return &ObjectRef{"Route", after(resourceName)}
    case strings.HasPrefix(resourceName, "us/"):  return &ObjectRef{"Upstream", after(resourceName)}
    case strings.HasPrefix(resourceName, "crt/"): return &ObjectRef{"Listener", seg(resourceName, 1)}
    default: return nil
    }
}
```

决策与依据：

- **不依赖 IR.SourceMap 的运行时副本做归属**。命名规约反解是无状态纯函数：管理面重启后、static 模式下、甚至对接按我们 bootstrap 拉起的外部 Envoy，反解都成立。SourceMap 的精确归属（如"该资源由哪个 `EnvoyResources` 对象引入"）属 M-API"查看编译产物"链路（FR-2.3，见编译层文档 §7），与状态视图解耦。
- 未归属资源**必须展示而非隐藏**：专家模式（FR-2.1）下它们恰恰是主体。UI 将其归入"未归属资源"分组（具体交互见 [`260717-3-console-api-design.md`](260717-3-console-api-design.md)）。
- Policy 无独立 Envoy 资源（编译为 filter 与 `typed_per_filter_config`），不进入状态模型的资源列表；其运行态（如限流命中计数）经 stats 体现（§5.1）。

## 4. 生效版本确认

发布流闭环的最后一环（FR-4.5）：M-DELIVER 完成下发后，M-STATE 从 admin API 观测数据面**实际落地**的版本，与已发布的 `IR.Version` 比对，产出确认事件。消费方为下发层（[`260717-1-deliver-layer-design.md`](260717-1-deliver-layer-design.md) §6）与发布流状态机（[`260717-2-config-domain-design.md`](260717-2-config-domain-design.md) §5）。

### 4.1 两模式的版本观测方式

| | xDS 模式 | static 模式 |
|---|---|---|
| 版本载体 | 动态资源 `version_info`（ADS SotW 下**非 EDS 类型**统一 = Snapshot version = `IR.Version`，见编译层文档 §5；EDS 快速通道使用 `<IR.Version>#eds-<seq>` 派生版本，[260717-1](260717-1-deliver-layer-design.md) §3.2） | **bootstrap `node.metadata` 注入**：`esgw.config_version: <IR.Version>` |
| 注入点 | 无需注入（Snapshot version 天然携带） | `static.Render(ir)` 渲染期写入 Bootstrap（M-DELIVER 纯函数，不回改 IR） |
| 观测点 | `/config_dump` 各 dynamic 段 `version_info` | `/config_dump` 的 bootstrap 段 `node.metadata` |

决策与依据：

- static 模式的版本标记选 `node.metadata` 而非文件头注释：注释不进 Envoy 内存，`/config_dump` 不可见；`node.metadata` 随 bootstrap 全量出现在 dump 中，是 static 模式下**唯一**零侵入、机器可读的版本观测通道。
- 约束（移交编译层/下发层）：注入的 `esgw.config_version` 字段**不参与** `IR.Version` 哈希计算（其值即哈希结果，参与会循环依赖）；注入发生在渲染期，`IR` 本体不变。
- static 文件头注释保留（编译层文档 §5 已定），服务"人读文件"场景；机器确认只认 metadata。
- 辅助信号：static hot restart 后 `/server_info` 的 `HotRestartEpoch` 递增，用于控制台展示"第 N 代进程"，**不**作为版本确认依据。

### 4.2 确认流程

```
M-CONF 发布 → M-DELIVER 下发（SetSnapshot(v) / 渲染 YAML + hot restart）
                  │ ExpectVersion(v, timeout=30s)
                  ▼
        M-STATE 确认快速通道：轮询 /config_dump
        （500ms 起步，指数退避至 5s，独立于 60s 周期档）
                  │
        ┌─────────┴──────────┐
        │ xDS: 非EDS动态资源    │ static: bootstrap
        │ version_info 去重集合  │ node.metadata
        │ == {v} ?             │ [esgw.config_version] == v ?
        └─────────┬──────────┘
                  ▼
   VersionConfirmEvent → M-DELIVER / M-CONF → 发布流"生效确认"闭环
```

```go
type VersionConfirmEvent struct {
    Expected  string            // 已发布 IR.Version
    Observed  string            // config_dump 实际观测到的版本（集合取并；未收敛时为观测明细摘要）
    Status    ConfirmStatus     // CONFIRMED | TIMEOUT
    Resources []ResourceVersion // 各 xDS 类型/资源的 version 明细，部分生效中间态排障用
    Elapsed   time.Duration     // 下发→确认耗时
    At        time.Time
}
```

比对规则（xDS）：收集 config_dump 中 **LDS/RDS/CDS/SDS 四类（非 EDS）**动态资源的 `version_info` 去重；集合恰为 `{v}` 则 CONFIRMED。**EDS 不参与相等判定**：EDS 快速通道使用 `<IR.Version>#eds-<seq>` 派生版本（[260717-1](260717-1-deliver-layer-design.md) §3.2），端点属运行态、不属配置版本（其 §3.1 规则 3）——若纳入比对，发布与端点变化交错时集合永远收敛不到 `{v}`，会将正常发布误报为 TIMEOUT；EDS 版本仍记入 `Resources` 明细供排障。下发后中间态（如 LDS 已更新、CDS 未更新）属正常窗口，继续轮询至收敛或超时。**TIMEOUT 不触发任何自动动作**——回滚与否是发布流状态机的决策（见 260717-2 §5），M-STATE 只报告事实。

### 4.3 与 NACK / 进程失败的职责边界

三条失败/确认通道并存，分工明确、互不替代：

| 通道 | 感知内容 | 责任模块 |
|---|---|---|
| ADS NACK | xDS 模式下 Envoy 拒绝配置（gRPC 流内快速失败） | M-DELIVER（下发层文档 §6）；M-STATE **不解析** ADS 流 |
| hot restart 失败 | static 模式新 epoch 进程起不来 | M-PROC（进程退出码/守护） |
| 版本落地观测 | 配置是否**真实生效**于数据面（正面确认与超时兜底） | M-STATE（本节） |

M-STATE 的 CONFIRMED 是唯一"数据面视角"的正面证据（xDS ACK 只代表 Envoy 接受并尝试应用）；TIMEOUT 是前两条通道都未报错时的最后兜底（如 static 模式渲染文件未被新进程加载的边角情形）。

## 5. 统计模型

### 5.1 指标清单与 Envoy stats key 映射

编译层把 HCM `stat_prefix` 设为规约资源名，stats key 因此自带归属信息（JSON 格式下 `/` 原样保留；Prometheus 格式下被替换为 `_`，M-STATE 内部只消费 JSON 格式）。归组入口：

- `http.<stat_prefix>.*`：stat_prefix 即 `lis/<listener>`，直接反解。
- `cluster.<name>.*`：name 即 `us/<upstream>`。
- `listener.<addr_port>.*`（L4 维度）：按地址归组——从 `ListenerState.Address` 建立 `0.0.0.0_443 → lis/web-https` 索引后反解。

| 维度 | 控制台指标 | Envoy stats key（JSON 格式） | 类型/口径 |
|---|---|---|---|
| 全局 | Envoy 版本/运行时长/状态 | `/server_info`（非 stats） | 直出 |
| Listener | 请求量 | `http.lis/<n>.downstream_rq_total` | counter → rate |
| Listener | 响应分布 | `http.lis/<n>.downstream_rq_2xx/3xx/4xx/5xx` | counter |
| Listener | 延迟 | `http.lis/<n>.downstream_rq_time` | histogram → P50/P90/P99 近似窗口值 |
| Listener | 活跃/累计连接（HTTP） | `http.lis/<n>.downstream_cx_active`、`downstream_cx_total` | gauge / counter |
| Listener | 活跃/累计连接（L4） | `listener.<addr>.downstream_cx_active/total` | 按地址归组 |
| Upstream | 请求量 | `cluster.us/<n>.upstream_rq_total` | counter → rate |
| Upstream | 响应分布 | `cluster.us/<n>.upstream_rq_2xx/4xx/5xx` | counter |
| Upstream | 延迟 | `cluster.us/<n>.upstream_rq_time` | histogram → P50/P90/P99 |
| Upstream | 上游活跃连接 | `cluster.us/<n>.upstream_cx_active` | gauge |
| Upstream | 连接失败 | `cluster.us/<n>.upstream_cx_connect_fail` | counter，排障重点 |
| Upstream | 重试 | `cluster.us/<n>.upstream_rq_retry` | counter |
| Upstream | 健康端点占比 | `cluster.us/<n>.membership_healthy` / `membership_total` | gauge |
| Upstream | 熔断水位 | `cluster.us/<n>.circuit_breakers.default.remaining_cx/rq` | gauge（P1 展示） |
| Route（v1 留口） | 每 rule 请求量/延迟 | `http.lis/<l>.vhost.vh/<r>.vcluster.vc/<r>/<i>.*` | **依赖编译层生成 virtual_cluster**，见未决事项 S1 |

口径声明（写入控制台文档与图表标注）：

- **成功率 = 1 − 5xx/total**。4xx 计入正常流量（客户端语义），控制台同时展示 2xx/3xx/4xx/5xx 四段分布，不单设"错误率"歧义指标。
- 延迟分位数来自 Envoy 直方图的 **quantile 近似窗口值**（`/stats?format=json` 的 `supported_quantiles`），是滑动窗口内的近似，非全局精确分位数；UI 标注"近似"。全分桶（bucket 累积）存储的演进见未决事项 S5。
- counter 存原始累积值，rate 在查询层按相邻点差值计算；Envoy 重启（counter 归零）时 rate 计算丢弃跨越零点的差值（显示为断点而非负值尖刺）。

### 5.2 内存环形时间序列缓冲

可行性 §2.4 已定方向：内存环形缓冲轻量实现，不引入 Prometheus 依赖。结构：

```go
// internal/state/ts
type SeriesKey struct{ Dim, Name, Metric string } // {cluster, "us/user-svc", "upstream_rq_total"}

type Series struct {
    Key      SeriesKey
    Interval time.Duration // = stats.poll_interval（10s），全 buffer 统一
    Base     time.Time     // 槽位 0 对应时刻
    Values   []int64       // 环形；直方图拆为 P50/P90/P99 三条序列当量
    Head, Filled int
}
```

- **单分辨率**：采样周期即分辨率，无降采样层级；槽位时间由 `Base + idx×Interval` 推得，点内不存时间戳。
- 序列按需创建：新 stats key 首次出现时建序列；配置换版后旧资源名的序列保留至滑出窗口，不主动清理（自然淘汰）。
- 上界保护：序列总数硬上限 5000（可配），超限拒绝新建并输出 warning——防止 EnvoyResources 引入的高基数 stats 名撑爆内存。

内存占用估算（每点 int64 = 8B；默认窗口 6h @10s = 2160 槽）：

| 规模 | 序列数估算 | 内存 |
|---|---|---|
| S1 典型（5 Listener / 10 Upstream / 20 rule，route 维度关） | ~200 | ~3.5MB |
| 设计上界（20 Listener / 100 Upstream / 200 rule，route 维度开） | ~2400（含直方图 3 倍当量） | ~42MB |

结论：典型规模可忽略；上界规模 ~42MB 对 NFR-2（<150MB）预算占比可观但可接受，提供 `stats.retention`（1h/6h/24h，默认 6h）供收紧。窗口/分辨率终审见未决事项 S3。

### 5.3 生命周期语义：重启即丢

时间序列纯内存持有，**esgw 重启即清零**——这是显式语义而非缺陷：

- 控制台图表展示采集起点（"数据始于 esgw 运行至今"），不暗示更长历史；
- A5（文件为真源）不覆盖统计数据，不引入统计持久化（SQLite 只存运行态元数据，不落时间序列）；
- 需要长期留存/告警的用户走外部 Prometheus + 透传端点（§5.4），管理面不重复造轮。

### 5.4 Prometheus 透传与管理面自身 metrics 的边界（FR-5.5）

| 端点 | 内容 | 命名空间 | 消费方 |
|---|---|---|---|
| `/metrics`（管理面端口） | esgw 自身指标：API 请求、编译耗时、发布次数、xDS 连接、采集成功率、序列数/缓冲字节数 | `esgw_*` | 运维 Prometheus（M-CORE 注册表，client_golang） |
| `/api/v1/envoy/stats/prometheus` | Envoy `/stats/prometheus` **原样流式透传** | `envoy_*` | 数据面指标的外部长期采集 |
| 控制台内部图表 | **不走 Prometheus**，走 M-STATE JSON API（§6） | — | 前端 SPA |

决策与依据：

- **不混挂**：`envoy_*` 不混入 `/metrics`。两个命名空间分属不同 target 语义（管理面进程 vs 数据面进程），混挂会让告警规则与实例标签产生歧义；外部 Prometheus 以两个 job 分别抓取。
- **透传零改写**：不加 label、不改名、不过滤（A2 零侵入精神的延伸），保持 `envoy_*` 与官方文档逐字一致，用户可直接复用社区 dashboard。实现上流式 copy，不全量缓冲 payload。
- **透传端点受鉴权保护**：admin 数据经管理面代理必须过 M-API 鉴权（NFR-5；Prometheus 抓取用 bearer token 等机器凭据，形态终审见未决事项 S4 与 [`260717-3-console-api-design.md`](260717-3-console-api-design.md)）。

## 6. 与相邻模块的契约

M-STATE 是旁路模块：不 import M-API / M-CONF / M-DELIVER，反向交互全部通过订阅与接口注入完成，保持架构 §3 的依赖单向性。

```go
// internal/state —— M-STATE 对外公共接口
type Service interface {
    // M-API 消费（控制台状态视图 FR-4.3 / 统计视图 FR-4.4）
    CurrentState(ctx context.Context, refresh bool) (*DataPlaneState, error)
    Series(ctx context.Context, q SeriesQuery) ([]Series, error)
    VersionStatus(ctx context.Context) VersionStatus

    // M-DELIVER 消费：发布成功后登记期待版本，启动确认快速通道（§4）
    ExpectVersion(version string, timeout time.Duration)

    // 确认事件订阅（M-DELIVER / M-CONF）
    SubscribeConfirm(ch chan<- VersionConfirmEvent) (cancel func())

    // Prometheus 透传 handler，由 M-API 挂载（§5.4）
    PrometheusProxy() http.Handler
}
```

| 相邻方 | 方向 | 契约 |
|---|---|---|
| M-API（[`260717-3-console-api-design.md`](260717-3-console-api-design.md)） | M-API → M-STATE | `CurrentState`（`refresh=true` 触发按需拉取）/ `Series` / `VersionStatus`；REST 形态已由该文档定稿为 `/api/v1/status/*` 与 `/api/v1/stats/*`（其 §3.3） |
| M-DELIVER（[`260717-1-deliver-layer-design.md`](260717-1-deliver-layer-design.md)） | M-DELIVER → M-STATE | 下发成功后调 `ExpectVersion`；订阅 `VersionConfirmEvent`（其 §6 的确认来源） |
| M-CONF（[`260717-2-config-domain-design.md`](260717-2-config-domain-design.md)） | M-CONF → M-STATE | 订阅 `VersionConfirmEvent`，驱动发布流状态机"生效确认"环节（FR-4.5，其 §5） |
| M-PROC | M-PROC → M-STATE | 提供 admin 端点（uds 路径/TCP 端口）与进程状态；M-STATE 只读降级展示（§2.3），不反向调用进程动作 |
| M-COMPILE | 静态契约 | 命名规约前缀唯一性是 §3.2 反解的权威依据（编译层文档 §5）；route 维度统计需 RouteBuilder 生成 virtual_cluster（未决事项 S1） |
| M-CORE | M-CORE → M-STATE | 自身 metrics 注册（采集成功率、序列数、缓冲字节数），挂入 `esgw_*` 注册表（§5.4） |

## 7. 未决事项

| # | 事项 | 计划决策时机 |
|---|---|---|
| S1 | route 维度统计的实现：编译层 RouteBuilder 为每条 rule 生成 virtual_cluster（命名拟 `vc/<route>/<n>`，match 与 rule 对齐），需编译层配合改动 | M1 统计视图开工前，与编译层迭代合并定稿 |
| S2 | 生效确认超时默认值（30s）是否适配 static hot restart 大配置慢场景；是否按下发模式分别取值 | M1 发布流端到端联调 |
| S3 | 时间序列默认窗口（6h @10s，上界 ~42MB）终审；是否需要"近 1h 高精度 + 24h 降采样"两级 | M1 统计视图实测内存后 |
| S4 | Prometheus 透传端点的机器凭据形态（bearer token / 独立抓取端口） | 随 260717-3 控制台 API 设计终审 |
| S5 | 延迟指标粒度演进：quantile 近似快照（v1）vs 全分桶累积存储 | M2 按控制台图表需求 |
| S6 | 多 Envoy 节点（FR-3.6，P2）下状态模型与序列缓冲按 NodeID 扩展的方案 | M2 后随多实例特性 |

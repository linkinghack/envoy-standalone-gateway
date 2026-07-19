# 260717-1 下发层设计（M-DELIVER / M-PROC Design）

- **项目**: envoy-standalone-gateway
- **上游文档**: [`requirements/01-initial-requirements-analysis.md`](../requirements/01-initial-requirements-analysis.md)（FR-3.1~3.6、FR-5.6、FR-6.3、NFR-5、NFR-6）、[`260715-2-overall-architecture.md`](260715-2-overall-architecture.md)（M-DELIVER/M-PROC 模块划分、T1~T3 拓扑、A1/A3/A6）、[`260715-1-feasibility-analysis.md`](260715-1-feasibility-analysis.md)（§2.1/§2.2 下发机制结论、RK2/RK4/RK5）、[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md)（IR 契约 §1/§7、EndpointSource §2、命名规约与版本 §5）
- **文档状态**: 初稿（Draft）
- **日期**: 2026-07-17
- **性质**: 模块详细设计。覆盖架构 §8 清单第 3 项：xDS snapshot 生命周期、static 模式 hot restart 协调协议；M-PROC 进程托管与下发强耦合，一并设计。发布流状态机见 [`260717-2-config-domain-design.md`](260717-2-config-domain-design.md)，生效确认采集见 [`260717-4-state-collection-design.md`](260717-4-state-collection-design.md)。M1 下发层与进程托管按此实现。

---

## 1. 职责总览与模式矩阵

### 1.1 职责切分

M-DELIVER 消费编译层产出的 IR（[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §7：`snapshot.FromIR` 与 `static.Render` 均为 IR 的纯函数），把它变成数据面实际生效的配置；M-PROC 负责本机 Envoy 进程的生命周期（FR-3.5，可选启用）。两者强耦合点只有一个：**static 模式下的生效需要 M-PROC 执行 hot restart**（§4），因此一并设计。

| 子模块 | 职责 | 对应需求 |
|---|---|---|
| M-DELIVER / xds | ADS gRPC server（go-control-plane，架构 §2 已定）、Snapshot 生命周期、ACK/NACK 跟踪 | FR-3.2、FR-3.3、FR-5.6 |
| M-DELIVER / static | `static.Render`、原子写文件、生效协调入口 | FR-3.1；产物与 FR-2.3"查看编译产物"同源 |
| M-DELIVER / eds | EDS 快速通道：运行期端点更新不经重编译下发（§3） | FR-6.3（P2） |
| M-PROC | envoy 二进制发现与版本检查、启动/接管、hot restart epoch 协调、崩溃守护退避 | FR-3.5、NFR-6 |

包结构（与 [`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §6 的包划分衔接）：

```
internal/deliver/
    deliver.go         Deliverer 接口、单写者串行、Status/Event（§6）
    xds/server.go      ADS gRPC server、callbacks（ACK/NACK 跟踪，§2.5）
    xds/snapshot.go    FromIR 装配（纯函数，§2.2）
    xds/bootstrap.go   接入 bootstrap 生成（托管落盘 / esgw bootstrap 导出，§2.7）
    static/render.go   Render(ir)（纯函数，含 node.metadata 版本注入，§4.1）
    static/writer.go   原子写（§4.1）
internal/proc/
    supervisor.go      启动/接管/崩溃退避/zombie reap（§5.2、§5.4）
    hotrestart.go      epoch 协调协议（§5.3）
    discover.go        二进制发现与版本检查（§5.6）
```

### 1.2 模式矩阵与拓扑映射

下发模式（`deliver.mode: xds | static`）× 进程托管（`proc.enabled`）共四种合法组合，与标准拓扑（架构 §5）一一对应：

| # | deliver.mode | proc.enabled | 拓扑 | 生效机制 | 定位 |
|---|---|---|---|---|---|
| ① | xds（默认） | 启用 | T1 | ADS Snapshot 原子换版，变更零重启（§2） | **默认推荐主场景**（S1/S3，Nginx 替代） |
| ② | xds | 仅下发 | T2/T3 | 同上；Envoy 生命周期由外部管理 | 同机分离部署、k8s |
| ③ | static | 启用 | T1 | 渲染落盘 + M-PROC hot restart（§4 b 档） | 坚持文件配置形态的自托管场景 |
| ④ | static | 仅下发 | T2/T3/导出 | 落盘即受理；外部进程管理器负责生效（§4 c 档） | YAML 导出、外部编排 |

决策与依据：

1. **xds 为默认模式**。可行性 §2.1/§2.2 已定：xDS 路线零技术风险、变更零重启、FR-5.6 为协议原生语义；static 定位为导出/迁移价值与极简场景补充。`esgw compile` CLI（M0 已交付，[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §6）覆盖"拿走 YAML 脱离 esgw 使用"的纯导出路径，不经过本节运行时下发层。
2. **单实例单模式**：同一 esgw 进程只启用一条下发通道，不做 xds+static 双通道并行下发——双通道会引入"以哪份为准"的语义分裂，且无任何需求支撑。
3. **模式切换是显式运维操作**：xds↔static 切换意味着 Envoy 需以另一种 bootstrap 启动。托管模式下由 M-PROC 以 hot restart 完成进程级替换（hot restart 对新旧进程的配置文件内容无约束，不因此断流）；仅下发模式由用户负责重启外部 Envoy。控制台将其呈现为带警告的显式操作（交互归 [`260717-3-console-api-design.md`](260717-3-console-api-design.md)），发布流按当时生效的模式记录 `Version.Mode`（[`260717-2-config-domain-design.md`](260717-2-config-domain-design.md) §2.3）。

### 1.3 配置形态（esgw.yaml 键草案）

```yaml
# 仅列本文档负责的键；完整配置 schema 归 M-CORE/安装交付物
deliver:
  mode: xds                      # xds | static（默认 xds，§1.2）
  xds:
    listen: 127.0.0.1:18000      # 非 loopback 必须配置 tls，否则启动报错（§2.7）
    nodeID: esgw-node            # §2.3
    ackTimeout: 15s              # NACK 观察窗口（§2.5），不阻塞受理
    # tls: {certFile, keyFile, clientCAFile}   # P2 预留（NFR-5 / FR-3.6，§2.7）
  static:
    outputPath: <data-dir>/envoy/envoy.yaml
proc:
  enabled: true                  # T1 默认；T2/T3 置 false
  envoyPath: ""                  # 空 = 按 §5.6 顺序发现
  baseID: 0                      # hot restart --base-id；同机多实例需错开
  liveTimeout: 30s               # 新 epoch live 判定超时（§5.3，DL1）
  drainTime: 600s                # --drain-time-s（DL1）
  parentShutdownTime: 900s       # --parent-shutdown-time-s；下限 120s（§5.3 安全不变量）
  restartBackoff: {initial: 1s, max: 30s, resetAfter: 60s, giveUpPer10m: 5}   # §5.4
  adoptPolicy: keep              # keep | restart（§5.2，DL4）
```

## 2. xDS 通道（ADS + SotW SnapshotCache）

### 2.1 组件与装配

沿用架构 §2 已定选型：**go-control-plane，ADS + SotW SnapshotCache**（Delta 演进时机归未决项 D4，不在本文讨论）。落地形态：

- `SnapshotCache` 以 `ads=true` 构造，node 哈希取 `node.Id`；单 gRPC server 上注册 ADS 服务，Envoy 以一条流复用 LDS/RDS/CDS/EDS/SDS 全部类型。
- Snapshot 装配为**纯函数** `snapshot.FromIR(ir)`，消费 `internal/ir`（契约见 [`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §7），映射关系：

| IR 集合 | xDS 类型 | 资源命名（编译层 §5 规约） |
|---|---|---|
| `IR.Listeners` | LDS | `lis/<listener>` |
| `IR.Routes` | RDS | `rc/<listener>` |
| `IR.Clusters` | CDS | `us/<upstream>` |
| `IR.Endpoints` | EDS | CLA 名 = Cluster 名（`eds_cluster_config.service_name` 缺省即 cluster 名，不另起规约） |
| `IR.Secrets` | SDS | `crt/<listener>/<n>` |
| `IR.Bootstrap` | **不下发** | 用于生成接入 bootstrap（§2.7）与 static 渲染（§4） |

```go
// internal/deliver/xds/snapshot.go —— 纯函数，无 IO、无全局状态
func FromIR(ir *ir.IR) (*cache.Snapshot, error) {
    snap, err := cache.NewSnapshot(ir.Version, map[resource.Type][]types.Resource{
        resource.EndpointType: toResources(ir.Endpoints),
        resource.ClusterType:  toResources(ir.Clusters),
        resource.RouteType:    toResources(ir.Routes),
        resource.ListenerType: toResources(ir.Listeners),
        resource.SecretType:   toResources(ir.Secrets),
    })
    if err != nil { return nil, err }
    return snap, snap.Consistent()   // 引用闭合自检（RDS/EDS/SDS 名与 Cluster/Listener 对得上）
}
```

`Consistent()` 是双保险：跨资源引用完整性在编译 F6 已全量检查（[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §3/F6），此处防装配层自身的 bug，失败即 Apply 同步报错（`stage=assemble`），非法 snapshot 不出管理面。

### 2.2 Snapshot 生命周期

```
esgw 启动：M-CONF 恢复当前 effective 版本（260717-2 §3 快照）
           → M-COMPILE 重编译 → M-DELIVER FromIR → SetSnapshot(v) → 再开始 accept xDS 连接
发布：     Apply(IR_v2) → FromIR → Consistent() → SetSnapshot(v2)     ← 旧 snapshot 整体替换
EDS 通道： UpdateEndpoints → 副本替换 Endpoints → per-type version 递增 → SetSnapshot（§3.2）
```

规则：

1. **Snapshot 不可变**：每次换版构造全新 `Snapshot` 对象，绝不原地修改旧对象（go-control-plane 约定，避免与在途响应的数据竞争）。
2. **version = IR.Version**：常规发布全类型共用同一版本串（编译层 §5：全资源确定性序列化 SHA-256 前 12 位）。同一内容重复 Apply 得到同一 version，Envoy 侧按 `version_info` 比对为无变化、不推送——"配置未变则零扰动"由此自然成立。EDS 快速通道的 per-type 版本例外见 §3.2。
3. **管理面不保留历史 snapshot**：历史版本的内容真源是 `versions/<seq>/config/`（[`260717-2-config-domain-design.md`](260717-2-config-domain-design.md) §3.1），需要旧版下发物时按确定性编译重建（编译层 §5），下发层不做版本仓库。
4. 容量假设：单实例、数百 route 级配置的全量 snapshot 在 MB 量级以内（可行性 §2.7 内存评估覆盖）。
5. **启动恢复对 M-DISCO 的容错**：恢复性重编译时若配置含 `kubernetesService` 而 M-DISCO 尚未 `active`（如 esgw 与 k8s API server 同时重启），`EndpointSource` 按降级规则返回空端点 + Warning、编译不失败（[`260717-5-k8s-disco-design.md`](260717-5-k8s-disco-design.md) §4.2 启动恢复特例），watch 就绪后经 EDS 快速通道（§3）自动补齐——否则启动链路会卡死在恢复失败上，数据面拿不到任何配置。

### 2.3 node id 约定与单节点假设

- 默认 `node.id = esgw-node`、`node.cluster = esgw`（可配 `deliver.xds.nodeID`），写入接入 bootstrap（§2.7）。
- **v1 单节点假设**：SnapshotCache 按 node id 为 key 天然支持多 snapshot，多 Envoy 节点（FR-3.6，P2）只需为每个 node id 装配 snapshot——本设计不做任何阻碍该扩展的假设，但 v1 只装配 `deliver.xds.nodeID` 一个节点；未知 node id 的 ADS 连接不装配 snapshot（SotW 语义下其请求挂起无响应），并输出 warning 日志提示误接。M-STATE 数据模型同样按单节点设计、预留 `NodeID`（[`260717-4-state-collection-design.md`](260717-4-state-collection-design.md) §1.3）。

### 2.4 原子换版语义（FR-3.3）

"一次变更要么整体生效要么整体不生效"由四层共同达成，如实界定各层职责：

| 层 | 机制 | 保证 |
|---|---|---|
| 下发前 | 编译 F1~F6 全量校验 + 可选 F7 `envoy --mode validate`（编译层 §3） | 非法配置不进入下发 |
| 装配 | `Consistent()` 自检（§2.1） | snapshot 内部引用闭合 |
| 推送 | ADS 单流复用，go-control-plane 按类型依赖序编排响应（先 CDS/EDS、后 LDS/RDS） | 类型间不出现倒挂引用 |
| 应用 | Envoy warming 语义：新 Listener/Cluster 在依赖资源就绪并完成 warming 前不承接流量 | 切换窗口内流量只打在完整配置上 |

如实说明的边界：SotW 是**逐类型**推送，网络故障等极端情形下类型间存在短暂混合窗口（如 Cluster 已更新而 Listener 未收到）；该窗口内旧配置仍在服务，且 warming 保证流量不会落到半成品上。因此 FR-3.3 的工程语义为"**流量维度无半生效状态**"，最终一致性由 SotW 重传与 M-STATE 生效确认（§6）兜底。

### 2.5 ACK/NACK 处理

- 实现：go-control-plane `server.Callbacks.OnStreamRequest` 检查 `DiscoveryRequest.ErrorDetail`，按 `(node, type_url)` 跟踪 `version_info` / `response_nonce`。某类型回报 `version_info == 当前 snapshot version` 且无 `ErrorDetail` 记为该类型 ACK。
- **NACK 语义 = Envoy 拒绝该类型资源、保持旧配置运行**——数据面安全（A3），这是 xDS 的协议级保护。
- NACK 是**最后防线而非正常路径**：F6 PGV + F7 envoy validate 已拦截绝大多数字段级错误（可行性 §2.3），NACK 出现基本意味着管理面 bug 或 Envoy 版本兼容问题（RK4）。处理：结构化记录（`type_url`、`response_nonce`、Envoy `error_detail` 原文）→ 事件上报发布流（§6.2）→ 日志与 `esgw_xds_nack_total` metric。**不自动重推、不自动回滚**——snapshot 保持，由下一次 Apply 覆盖；回滚决策归发布流（[`260717-2-config-domain-design.md`](260717-2-config-domain-design.md) §4.3）。
- `ackTimeout`（默认 15s）仅作为观察窗口用于状态展示，**不阻塞受理语义**（§6.1）；生效确认的权威通道是 M-STATE 的 config_dump 观测（[`260717-4-state-collection-design.md`](260717-4-state-collection-design.md) §4.3 已定："xDS ACK 只代表 Envoy 接受并尝试应用"）。

### 2.6 断连重连与管理面重启（FR-5.6）

| 场景 | 行为 |
|---|---|
| Envoy 断连重连 | SnapshotCache 常驻最新 snapshot；新流建立后按 `version_info` 比对，仅差异推送。管理面零介入 |
| esgw 停机期间 | Envoy 按最后有效配置继续服务（xDS 原生语义，FR-5.6/A3）；xDS 断连本身不产生流量影响 |
| esgw 重启 | 启动链路（§2.2）先装配 snapshot 再 accept 连接；确定性编译保证 `IR.Version` 与停机前一致 → Envoy 重连后比对版本相同 → **零推送、零扰动** |
| Envoy 重启（外部） | 以接入 bootstrap 重新连上 ADS，全量拉取当前 snapshot；配置真源不在 Envoy 侧，无需持久化 |

### 2.7 监听安全与接入 bootstrap

- 默认绑定 `127.0.0.1:18000` 明文 gRPC：T1/T2 同机、T3 同 Pod localhost，与 RK5 同级处理（loopback 绑定即访问控制）。**硬校验**：`deliver.xds.listen` 配置为非 loopback 地址且未配置 `tls` → esgw 启动报错，拒绝裸奔。
- 跨主机 xDS（FR-3.6 多节点，P2）强制 mTLS（NFR-5）：配置字段已预留（§1.3），服务端证书 + 客户端 CA 校验，实现随多节点特性专项（DL3）。
- **接入 bootstrap**（xds 模式下 Envoy 唯一的 static 文件）由 M-DELIVER 生成：托管模式落盘 `<data-dir>/envoy/bootstrap-xds.yaml` 并由 M-PROC 拉起；仅下发模式提供 `esgw bootstrap --mode xds` 命令导出等价文件，供用户的外部 Envoy 使用（T2/T3 交付物）。内容骨架：

```yaml
node: {id: esgw-node, cluster: esgw}
admin:
  address:
    pipe: {path: /var/run/esgw/envoy-admin.sock}   # T1/T2 默认 uds（RK5，见 260717-4 §1.3）
dynamic_resources:
  ads_config:
    api_type: GRPC
    transport_api_version: V3
    grpc_services:
      - envoy_grpc: {cluster_name: esgw_xds}
  cds_config: {ads: {}, resource_api_version: V3}
  lds_config: {ads: {}, resource_api_version: V3}
static_resources:
  clusters:
    - name: esgw_xds
      type: STATIC
      connect_timeout: 5s
      lb_policy: ROUND_ROBIN
      typed_extension_protocol_options:
        envoy.extensions.upstreams.http.v3.HttpProtocolOptions:
          "@type": type.googleapis.com/envoy.extensions.upstreams.http.v3.HttpProtocolOptions
          explicit_http_config: {http2_protocol_options: {}}
      load_assignment:
        cluster_name: esgw_xds
        endpoints:
          - lb_endpoints:
              - endpoint:
                  address: {socket_address: {address: 127.0.0.1, port_value: 18000}}
```

## 3. EDS 快速通道

M-DISCO 的运行期端点变化（k8s `Endpoints`/`EndpointSlice` watch，见 [`260717-5-k8s-disco-design.md`](260717-5-k8s-disco-design.md)）**不触发全量重编译**，经本通道直接更新下发物（契约源头：[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §7"运行期端点变化直接调 M-DELIVER 的 EDS 通道更新 `IR.Endpoints` 副本，不经过 Compile"）。

### 3.1 通道契约

```go
// M-DELIVER 对 M-DISCO 暴露的 EDS 快速通道。
// 语义：对 key 指定的 CLA 做全量替换；value 为 nil 表示移除该条目
// （Service 删除等兜底语义由调用方决策，见 260717-5）。
// 约束：key 必须是编译期登记为 EDS 来源的条目（kubernetesService Upstream，
// 命名 us/<upstream>，编译层 §2），否则返回错误。
UpdateEndpoints(ctx context.Context, updates map[string]*endpointv3.ClusterLoadAssignment) error
```

规则：

1. **单写者串行**：与 `Apply` 共用同一把写锁；基于当前 IR 的浅拷贝副本替换 `Endpoints` 条目，IR 资源对象只读约定不破（编译层 §1）。
2. **一次调用 = 一次原子换版**；通道内不丢弃、不乱序。**防抖与批量合并在调用方**（M-DISCO 聚合 watch 事件后调用，见 [`260717-5-k8s-disco-design.md`](260717-5-k8s-disco-design.md)），通道本身无内建防抖，契约保持最小。
3. **端点是运行态，不进版本历史**：本通道不改变 `IR.Version`、不产生 M-CONF 版本（FR-3.4 只管配置版本，A5）；生效确认对配置版本（§6）不受端点换版干扰（M-STATE 比对的非 EDS 类型版本不变，[`260717-4-state-collection-design.md`](260717-4-state-collection-design.md) §4.2）。
4. 非 EDS 来源的 Upstream（静态 `endpoints` → STATIC、`dns` → LOGICAL/STRICT_DNS）的解析与刷新由 Envoy 自身承担，与本通道无关。

### 3.2 xds 模式实现：per-type version 换版

机制仍是全量 `SetSnapshot`，但利用 go-control-plane 的分类型 version 能力，只推变化类型：

```go
// 以上一版 snapshot 的值拷贝构造全新 Snapshot（遵守 §2.2 规则 1，不原地修改旧对象）：
// 仅 Endpoints 类型换新 version，其余类型沿用原资源与原 version
// → SotW 按 version_info 比对，只向 Envoy 推 EDS。
next := *prev                                          // Snapshot 值拷贝（Resources 为定长数组，随值复制）
edsVer := fmt.Sprintf("%s#eds-%d", ir.Version, seq)    // seq 进程内单调递增
next.Resources[types.Endpoint] = cache.NewResources(edsVer, claItems)
// cache.SetSnapshot(ctx, nodeID, &next)
```

- `seq` esgw 重启后归零：重启后经 `EndpointSource` 取全量初值重新装配（§3.4），version 串不同即触发一次全量 EDS 推送——SotW 版本比较是相等性判断而非顺序判断，归零无歧义。
- 收益：端点高频变化时 LDS/RDS/CDS/SDS 零重推，Envoy 侧仅 EDS 增量生效，不触碰 listener/filter chain。

### 3.3 static 模式行为

static 形态下 EDS CLA 已内联进 Cluster（编译层 §1/F5 形态化），不存在运行期端点热更新通道。与 [`260717-5-k8s-disco-design.md`](260717-5-k8s-disco-design.md) §5.1/§5.2 对齐：static 模式下 M-DISCO 的 EndpointTracker **不启动**，`kubernetesService` Upstream 编译为**发布时刻端点快照**（发布流给出 Warning）。因此：

- 本通道在 static 模式下**不可用**：调用 `UpdateEndpoints` 返回错误（契约防御，正常路径不会发生）；端点更新只能通过再次发布取得新快照。
- 文档与控制台明示：**端点高频变动的场景（典型即 k8s）应选择 xds 模式**（矩阵 ①/②），static 模式下 `kubernetesService` 类 Upstream 不跟踪端点变化（FR-6.3 为 P2，该限制可接受）。

### 3.4 与编译层 EndpointSource 的衔接

| 时机 | 端点来源 |
|---|---|
| 发布（Compile） | 编译层经 `EndpointSource` 取**当时最新**端点注入 IR（[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §2），保证任何发布产物的端点不陈旧 |
| 发布后运行期 | 本通道增量替换（§3.1） |

发布与 `UpdateEndpoints` 交错时以**后发生者**为准（单写者串行保证语义）：若 Apply 基于的 `EndpointSource` 快照早于最近一次运行期更新，发布短暂回退端点，M-DISCO 的 watch 会在下一个事件将其纠正——该窗口为秒级且最终一致，不引入锁外协调。

## 4. static 通道

### 4.1 渲染与落盘

`static.Render(ir)` 为纯函数（契约见 [`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §7），输出**单文件** `envoy.yaml`：

```yaml
# Generated by esgw. DO NOT EDIT.
# config_version: 3f9a1c2b7d01
admin:
  address:
    pipe: {path: /var/run/esgw/envoy-admin.sock}    # T1/T2 默认 uds（260717-4 §1.3）
node:
  id: esgw-node
  cluster: esgw
  metadata: {esgw.config_version: 3f9a1c2b7d01}     # ← 渲染期注入，见下
static_resources:
  listeners: [...]    # lis/*；route_config 内联（F5 static 形态）
  clusters: [...]     # us/*；端点 static / STRICT_DNS 内联
```

渲染期两条版本标记（与 [`260717-4-state-collection-design.md`](260717-4-state-collection-design.md) §4.1 的既定契约严格一致）：

1. **机器可读**：向渲染产物的 `Bootstrap.Node.Metadata` 注入 `esgw.config_version: <IR.Version>`——M-STATE 从 `/config_dump` bootstrap 段读取，是 static 模式生效确认的**唯一**依据。**注入发生在渲染期，不回改 IR、不参与 `IR.Version` 哈希**（其值即哈希结果，参与会循环依赖）。
2. **人读**：文件头注释 `config_version`（编译层 §5 已定），服务手工排障与 git  diff。

落盘规则：

- **原子写**：写 `<data-dir>/tmp/envoy.yaml.<pid>` → fsync → `rename(2)` 到 `deliver.static.outputPath`（同文件系统保证原子；`tmp/` 约定见 [`260717-2-config-domain-design.md`](260717-2-config-domain-design.md) §3.1）。Envoy 或人工任何时刻读到的都是完整文件。
- 证书以**文件路径**引用（F5 已定 static 形态不走 SDS；协议对象中的 `certFile/keyFile` 直落，托管证书 `ref:` 的解密落盘归 M-STORE，方案属未决项 D5/PD3）——下发层产物只含路径，不碰私钥内容。

### 4.2 生效机制三档

可行性 §2.2 列出的三条路径，本文定案如下：

| 档 | 机制 | 覆盖变更范围 | 首期状态 |
|---|---|---|---|
| a | bootstrap 以 `*_config_path` 引用路由/证书文件，inotify 热加载（move 语义） | 仅 route 与证书**内容**；listener/cluster 结构变更不覆盖 | **不启用（预留）**：需拆分多文件渲染 + 变更分类判定（区分"仅路由/证书"与结构变更），与 RK2 缓解"收敛单一路径"冲突；启用与否归 DL2 |
| b | M-PROC 托管 hot restart（§5.3） | 任意变更 | **默认且唯一启用路径**（组合 ③） |
| c | 外部进程管理器重启（systemd restart / 容器重建） | 任意变更 | 组合 ④ 的唯一生效路径，esgw 不触发（§5.7） |

**收敛说明（与上游文献的关系）**：可行性 §2.2 曾表述"首期采用 a+b 组合"，其同篇 RK2 缓解又要求"static 首期限定'托管进程 + hot restart'单一路径"。本文按 RK2 收敛为 **b 单一路径**：a 档节省的只是热重启开销，引入的多文件渲染与变更分类复杂度在首期没有对应需求支撑；待 static 模式有真实使用反馈后由 DL2 终审是否补回。

### 4.3 生效语义声明（呼应 RK2）

不同组合的 Apply 受理语义不同，必须文档化并写入控制台提示（呈现归 [`260717-3-console-api-design.md`](260717-3-console-api-design.md)）：

| 组合 | Apply 返回受理的含义 | 生效确认 |
|---|---|---|
| ③ static+托管 | 新 epoch 已 live（旧连接按 drain 排空中，§5.3） | M-STATE 观测 `esgw.config_version`（§6） |
| ④ static+仅下发 | **文件已原子落盘，仅此而已**；生效责任在外部进程管理器 | M-STATE 异步观测，确认超时参数放宽（§6.1） |

## 5. M-PROC 进程托管

### 5.1 职责与边界

负责：envoy 二进制发现与版本检查（§5.6）、接入 bootstrap / envoy.yaml 落盘后的进程拉起、运行中进程接管（§5.2）、hot restart epoch 协调（§5.3）、崩溃守护退避（§5.4）、T1 容器内 PID 1 场景的 zombie reap（不引入 tini，A6）。

**边界（硬性）**：M-PROC **不直接访问 admin API**——M-STATE 是管理面内 admin 的唯一消费者（[`260717-4-state-collection-design.md`](260717-4-state-collection-design.md) §1.3）。M-PROC 的就绪与 epoch 判定全部经由 M-STATE 的探测结果（`/ready`、`/server_info` 的 hot restart epoch，探测档位见 260717-4 §2.2）；M-PROC 自身只做进程级观察（spawn 成败、wait 退出码、信号）。由此 M-CORE 启动序必须保证 M-STATE 就绪后 M-PROC 才接受热重启请求。

### 5.2 启动与接管

esgw 重启不得导致数据面重启（FR-5.6 在托管组合下同样成立）。M-PROC 以 `<data-dir>/run/proc.json`（`{pid, baseID, epoch, configPath, startedAt, envoyVersion}`）为线索：

```
proc.enabled 启动流程
  ├─ proc.json 存在 且 pid 存活（kill(pid,0)）？
  │    ├─ 是 → 等待 M-STATE 首次 /server_info（≤15s）
  │    │        ├─ epoch/state 与记录一致 → 接管：不重启 envoy，续管该 epoch
  │    │        └─ 不一致或超时 → adoptPolicy:
  │    │              keep（默认）：标记异常并告警，不擅杀数据面进程（A3 优先），待人工
  │    │              restart    ：普通重启 envoy（秒级断连，显式配置才启用）
  │    └─ 否（无记录/进程已死）→ 以 epoch 0 全新启动
  └─ 全新启动：xds → 落盘接入 bootstrap（§2.7）；static → 确保 envoy.yaml 已渲染
               → spawn: envoy -c <file> --base-id <id> --restart-epoch 0
```

### 5.3 hot restart 协调协议

Go 内嵌实现，语义等价官方 `hot-restarter.py`（RK2 攻坚点）。固定 Envoy 参数：`--base-id`（§1.3，同机多 esgw 实例需错开，共享内存段 `/dev/shm/envoy_shared_memory_<base_id>` 即以此为键）、`--drain-time-s`、`--parent-shutdown-time-s`、`--restart-epoch`。

```
esgw (M-PROC)              envoy epoch N            envoy epoch N+1
    │ Apply(static, IR_v2)      │ serve                    │
    │ 1. Render → 原子写 envoy.yaml（§4.1）                 │
    │ 2. spawn --restart-epoch N+1 ───────────────────────►│ start
    │                           │◄─ 共享内存(同 base-id)+uds ─┤
    │                           │   交接监听 fd / stats       │
    │ 3. 轮询 M-STATE：/ready=LIVE 且 epoch=N+1（≤liveTimeout 30s）
    │                           │◄─ 新进程通知开始 drain ─────┤ serve (LIVE)
    │                           │ DRAIN（≤drain-time-s 排空；  │
    │                           │ parent-shutdown-time 到期由  │
    │                           │ 新进程指令其退出）→ exit     │
    │ 4. 旧进程退出后 M-PROC 异步 reap（观察者，不发信号）
    │ 5. Apply 返回受理（→ 发布流 CONFIRMING，§6.1）
```

规则：

1. **live 判定**（步骤 3）：以 M-STATE 探测为准——`/ready` 返回 LIVE 且 `/server_info` 的 `HotRestartEpoch == N+1`；转换期 admin socket 交接会造成瞬时探测失败，窗口内容忍重试。
2. **旧 epoch 的 drain 与退出由 Envoy hot restart 协议自身驱动**：新进程初始化完成后经共享内存/uds 通知旧进程开始 drain（`--drain-time-s` 内排空存量连接），`--parent-shutdown-time-s` 到期由**新进程**指令旧进程退出——这是 Envoy 的官方协调语义。M-PROC 对旧 epoch **不发送任何信号**：外部 SIGTERM 走 Envoy 常规退出路径，会绕过 drain 协调造成断连。M-PROC 只做 wait/reap 与状态记录（与官方 `hot-restarter.py` 的职责边界一致），reap 异步进行、不阻塞 Apply。
3. **失败**（新进程退出码非零 / liveTimeout 超时）：SIGKILL 新 epoch，Apply 返回 `stage=hot_restart` 错误，附新进程 stderr 尾部（≤4KB，排障关键）。**安全不变量：`liveTimeout`(30s) << `--parent-shutdown-time-s`（配置下限 120s）**——旧 epoch 只会在新进程发出 parent-shutdown 指令后退出，回杀必然发生在该指令之前，任何时刻至少一个 epoch 承接流量。回杀后旧 epoch 继续服务；若交接已推进至监听 fd/admin 移交之后，旧 epoch 的 admin 观测可能短暂降级（M-STATE 展示 Stale），下一次发布以 epoch N+2 重试拉起（epoch 单调递增，不复用失败序号）；该边角的实测处置列 DL6。
4. 判定期间 M-STATE 自身故障 → 按超时失败处理（安全方向：不误杀新 epoch，也不触碰旧 epoch）。

### 5.4 崩溃守护与退避重启

| 场景 | 动作 |
|---|---|
| 托管 envoy 运行期非预期退出 | 以**当前落盘配置**原地重启（last-good 就在文件上，A5 推论）；退避 `initial 1s ×2ⁿ`，上限 `max 30s`；连续稳定运行 `resetAfter 60s` 重置退避 |
| 频繁崩溃（`giveUpPer10m: 5`） | 停止自动重启，进入 `degraded`：事件 + 日志告警（§6.2），等待人工介入或下一次发布（发布会重新尝试拉起） |
| esgw 自身重启 | 走 §5.2 接管流程，不计入崩溃退避 |

依据：配置已过三层校验（F6/F7），运行期崩溃大概率是资源（OOM）或 Envoy 自身问题，指数退避避免崩溃循环打满 CPU；`degraded` 让"反复死"显式可见而非静默挣扎。

### 5.5 就绪探测与健康检查

- 数据面就绪以 M-STATE `/ready` 探测为准（启动期 1s → 稳态 10s 档位，[`260717-4-state-collection-design.md`](260717-4-state-collection-design.md) §2.2），M-PROC 不另建探测通道（§5.1 边界）。
- M-CORE 健康检查端点组合：esgw 自身健康 + （托管时）envoy ready + 托管进程状态（running/degraded），供 systemd `Type=notify`/容器 liveness 使用（FR-5.1/FR-5.5）。

### 5.6 envoy 二进制发现与版本检查（NFR-6）

- 发现顺序：`ESGW_ENVOY_PATH` 环境变量 → `proc.envoyPath` 配置 → `PATH` 中查找 `envoy`。
- 启动时执行 `envoy --version` 解析版本，对照**支持区间常量**（随 esgw 版本发布维护，跟随 Envoy 最近 2~3 个 minor，架构 §2/NFR-6；CI 多版本 `--mode validate` 矩阵验证，RK4）。
- 区间处理：**低于下限 = M-PROC 拒绝托管启动并明确报错**（xds 仅下发模式不受影响，用户 Envoy 版本自负责）；**高于上限 = warning 继续**（新版本向后兼容概率高，且阻塞升级路径违背 NFR-6 演进策略）。

### 5.7 仅下发模式语义（proc.enabled=false）

- M-PROC 全部职责关闭：无 spawn、无接管、无退避、无 reap。
- xds 组合（②）：esgw 仅作 xDS server；外部 Envoy 以 `esgw bootstrap --mode xds` 导出的 bootstrap 接入（§2.7）。
- static 组合（④）：Apply = 渲染 + 原子写即返回受理；生效责任完全在外部进程管理器（c 档）。文档与控制台必须明示此语义（§4.3，RK2 呼应）；M-STATE 观测照常工作，为发布流提供异步确认（§6.1 超时放宽）。

## 6. 下发状态回报与发布流闭环

消费方为发布流状态机（[`260717-2-config-domain-design.md`](260717-2-config-domain-design.md) §5：VALIDATING→VALIDATED→PUBLISHING→CONFIRMING→EFFECTIVE / CONFIRM_TIMEOUT）。本节定义 M-DELIVER 侧的契约表面。

### 6.1 Apply 语义

```go
// internal/deliver
type Deliverer interface {
    // Apply 原子提交新 IR：xds = 换 snapshot 版本；static = 渲染落盘（+托管时热重启）。
    Apply(ctx context.Context, ir *ir.IR) error
    // UpdateEndpoints EDS 快速通道（§3.1）。
    UpdateEndpoints(ctx context.Context, updates map[string]*endpointv3.ClusterLoadAssignment) error
    // Status 当前下发状态快照（控制台/排障）；Events 异步事件流（§6.2）。
    Status() Status
    Events() (<-chan Event, cancel func())
}
```

| 模式 | Apply 同步路径 | 返回受理（PUBLISHING→CONFIRMING）的含义 |
|---|---|---|
| xds（托管/仅下发） | FromIR 装配 → Consistent() → SetSnapshot | snapshot 已装载（**不等待** Envoy 连接或 ACK——仅下发组合下 Envoy 可能尚未接入） |
| static 托管 | Render → 原子写 → M-PROC hot restart 至新 epoch live（§5.3） | 新 epoch 已拉起 |
| static 仅下发 | Render → 原子写 | 文件已落盘（生效责任外部，§5.7） |

规则：

1. **幂等跳过**：`IR.Version` 与当前下发版本相同 → 直接成功返回，不执行换版/渲染/热重启（配合 [`260717-2-config-domain-design.md`](260717-2-config-domain-design.md) §2.3"Seq 不同、IRVersion 相同可跳过重复下发"）。例外：static 仅下发组合**总是重写文件**——廉价且可自愈外部删改。
2. **受理后登记确认**：Apply 成功后 M-DELIVER 调 M-STATE `ExpectVersion(version, timeout)`（[`260717-4-state-collection-design.md`](260717-4-state-collection-design.md) §4.2），启动确认快速通道。timeout 默认 30s（xds / static 托管，对齐 260717-4 §2.2 `confirm.timeout`）；static 仅下发放宽至 10min 可配（外部重启节奏不可控；默认值终审归 CD3/S2）。
3. **单写者串行**：Apply / UpdateEndpoints 排队执行；发布流自身有单发布并发约束（260717-2 §5.3），下发层不再对配置版本做合并去重（除幂等跳过）。

### 6.2 状态与事件模型

```go
type Status struct {
    Version   string     // 当前受理的 IR.Version（"" = 尚未下发）
    Phase     Phase      // idle | delivering | awaiting_confirm | confirmed | nacked | failed
    Detail    string     // NACK error_detail / 失败摘要
    UpdatedAt time.Time
}

type Event struct {
    Kind    EventKind   // Applied | Nacked | HotRestartFailed | SupervisorDegraded
    Version string
    Detail  string      // Nacked: type_url + nonce + envoy error_detail 原文；
                       // HotRestartFailed: 新进程 stderr 尾部；SupervisorDegraded: 崩溃计数
    At      time.Time
}
```

- `confirmed` 由 M-STATE `VersionConfirmEvent`（CONFIRMED）驱动更新；`VersionConfirmEvent TIMEOUT` 时 Phase 保持 `awaiting_confirm` 并记录观测明细——**TIMEOUT 的终态语义（CONFIRM_TIMEOUT，非失败）归发布流裁决**（260717-2 §5.1），下发层只报告事实。
- NACK 为异步事件（§2.5）：发布流可消费 `Nacked` 提前结束 CONFIRMING 等待；保底路径仍是 M-STATE 确认超时——NACK 后 Envoy 保持旧配置，config_dump 永远观测不到新版本，必然超时，两条通道结论一致、快慢不同（职责边界见 [`260717-4-state-collection-design.md`](260717-4-state-collection-design.md) §4.3）。

### 6.3 失败路径对照表

| 失败点 | 检测方/方式 | 回报形态 | 发布流落点 | 数据面状态 |
|---|---|---|---|---|
| 装配失败（Consistent 不过） | M-DELIVER 同步 | Apply error（`stage=assemble`） | PUBLISH_FAILED | 旧配置不变 |
| SetSnapshot / 渲染 / 写文件失败 | M-DELIVER 同步 | Apply error（`stage=set_snapshot/render/write_file`） | PUBLISH_FAILED | 旧配置不变 |
| hot restart 新 epoch 未 live / 退出 | M-PROC → M-DELIVER 同步（Apply 内） | Apply error（`stage=hot_restart` + stderr 尾部） | PUBLISH_FAILED | 旧 epoch 继续服务（§5.3 不变量） |
| NACK（xds） | M-DELIVER callbacks 异步 | `Event{Nacked}` + Status | 发布流可提前结束 CONFIRMING；保底 CONFIRM_TIMEOUT | Envoy 拒绝新资源，保持旧配置 |
| 仅下发 xds 无 Envoy 接入 | 无同步信号 | Status 保持 `awaiting_confirm` | CONFIRM_TIMEOUT（可重查） | 取决于外部 Envoy |
| 版本未落地（原因不明） | M-STATE 确认超时 | `VersionConfirmEvent{TIMEOUT}` | CONFIRM_TIMEOUT（非失败终态） | 以 config_dump 观测为准 |
| 托管 envoy 频繁崩溃 | M-PROC | `Event{SupervisorDegraded}` | 不阻塞发布流；控制台告警 | 停止自动重启前按退避尝试 |

## 7. 与相邻模块的契约

| 相邻方 | 方向 | 契约 |
|---|---|---|
| M-CONF（[`260717-2-config-domain-design.md`](260717-2-config-domain-design.md)） | 调用方 | 发布流 PUBLISHING 阶段调 `Apply(ir)`；消费 Apply 同步错误、`Event`、`Status` 驱动状态机；`IR.Version` 幂等跳过配合其 Seq/IRVersion 双标识语义 |
| M-COMPILE（[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §7） | 上游 | 消费 IR：`snapshot.FromIR` / `static.Render` 均为 IR 纯函数；`IR.Version` 同时作为 snapshot version、static 文件头注释与 `esgw.config_version` 注入值 |
| M-STATE（[`260717-4-state-collection-design.md`](260717-4-state-collection-design.md)） | 旁路 | 受理后调 `ExpectVersion(version, timeout)`；消费 `VersionConfirmEvent` 更新 `Status.Phase`；M-PROC 的就绪/epoch 判定以 M-STATE 为唯一数据源（admin 单消费者边界，§5.1） |
| M-DISCO（[`260717-5-k8s-disco-design.md`](260717-5-k8s-disco-design.md)） | 调用方 | `UpdateEndpoints` 通道契约（§3.1）；初始端点经编译层 `EndpointSource`（编译层 §2），运行期增量走本通道；防抖批量在 M-DISCO 侧 |
| M-PROC（本文 §5） | 内部配对 | static 模式 Apply 经 M-PROC hot restart 生效；xds 托管模式经 M-PROC 拉起/接管 envoy；M-PROC 向 M-DELIVER 回报进程级失败 |
| M-CORE | 装配 | 启动序：M-CONF 恢复 effective 版本 → 重编译 → M-DELIVER 首版装配 → xDS listen / M-PROC 拉起（§2.2、§5.1）；`esgw_*` metrics：`esgw_deliver_apply_total{mode,result}`、`esgw_xds_nack_total`、`esgw_xds_connections`、`esgw_proc_envoy_state`、`esgw_proc_restart_total` |

## 8. 未决事项

| # | 事项 | 计划决策时机 |
|---|---|---|
| DL1 | hot restart 时序参数默认值：`liveTimeout`（拟 30s）、`--drain-time-s` / `--parent-shutdown-time-s` 是否调离 Envoy 默认（600s/900s）至网关场景更短值 | M1 编码期定 |
| DL2 | static 模式 a 档（文件级动态资源热加载）是否补回：首期按 RK2 收敛为 b 单一路径（§4.2），a 档留待真实使用反馈 | 1.0 前按 static 模式反馈终审 |
| DL3 | 跨主机 xDS mTLS 方案与多节点 node id 管理细节（§2.7 预留） | 随 FR-3.6（P2）专项设计 |
| DL4 | 接管边界：`adoptPolicy` 默认值（拟 `keep`）与"admin 不可达但进程存活"的处置是否需第三档（如强制新的 base-id 并行启动） | M1 编码期定 |
| DL6 | hot restart 失败回杀后的旧 epoch 边角：监听/admin fd 已移交时旧 epoch 的观测降级处置、以 epoch N+2 重试时共享内存段状态的兼容性 | M1 hot restart 联调期实测定 |
| DL5 | ~~static 模式 EDS 通道退化路径~~ **已决**：static 模式不提供 EDS 退化通道，端点为发布时刻快照（§3.3，与 260717-5 §5.2 对齐） | 已关闭 |

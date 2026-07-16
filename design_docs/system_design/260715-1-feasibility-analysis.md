# 260715-1 可行性分析（Feasibility Analysis）

- **项目**: envoy-standalone-gateway
- **上游文档**: [`requirements/01-initial-requirements-analysis.md`](../requirements/01-initial-requirements-analysis.md)
- **文档状态**: 初稿（Draft）
- **日期**: 2026-07-15
- **结论先行**: **可行**。所有 P0 能力均有成熟技术路径支撑，无需修改 Envoy，核心风险集中在"抽象协议表达力"与"static 模式生效机制"两处，均有明确的缓解与验证手段（见 §5、§6）。

---

## 1. 分析范围与方法

针对需求文档中的 G1~G7 目标与 P0/P1 功能需求，从四个维度评估可行性：

1. **技术可行性**：驱动 Envoy 的两种方式、状态采集、配置校验、管理面自身实现，是否都有成熟可依赖的技术手段。
2. **生态位可行性**：与现有开源/商业网关对比，本项目定位是否成立、是否存在被现有产品直接覆盖的风险。
3. **工程可行性**：以小团队 + AI 辅助开发的投入，里程碑 M0→M2 是否可达。
4. **许可证可行性**：AGPL-3.0 与依赖生态的兼容性。

## 2. 技术可行性

### 2.1 xDS 动态下发（FR-3.2）——✅ 成熟

- Envoy 官方维护 [go-control-plane](https://github.com/envoyproxy/go-control-plane)，提供 xDS/ADS gRPC server 实现与 SnapshotCache，是 Envoy Gateway、Contour、Gloo 等项目的共同基座，稳定性经过大规模生产验证。
- ADS（Aggregated Discovery Service）单流下发全部资源，天然满足 FR-3.3 的原子性要求（一个 snapshot 版本整体生效）。
- Envoy 以极小 bootstrap（仅含 ADS cluster + node id）接入，其余 LDS/RDS/CDS/EDS/SDS 全部动态化，变更零重启。
- 管理面宕机时 Envoy 保持最后有效配置继续服务（FR-5.6 / NFR-3），这是 xDS 协议的原生语义。

**结论**：xDS 路线零技术风险，直接决定管理面语言选型为 Go（见架构文档 §2）。

### 2.2 static YAML 模式（FR-3.1）——✅ 可行，需明确生效语义

Envoy static 配置的变更生效有三条路径，复杂度递增：

| 路径 | 机制 | 适用变更 | 约束 |
|---|---|---|---|
| a. 文件级动态资源 | bootstrap 中使用 `*_config_path`（route/SDS 等文件热加载，基于 inotify move 语义） | 路由、证书 | listener/cluster 结构变更不覆盖 |
| b. Hot restart | `--restart-epoch` + 官方 `hot-restarter.py` 等价逻辑，连接不断 | 任意变更 | 需要管理面托管 Envoy 进程（FR-3.5）；同机同 base-id |
| c. 普通重启 | systemd restart | 任意变更 | 断连，仅作兜底 |

**判断**：static 模式技术上完全可行，但生效体验弱于 xDS。策略上（与需求文档 R2 一致）：

- **xDS 为默认推荐模式**；
- static 模式的价值定位为：① 用户可拿走生成的 YAML 脱离本软件独立使用（学习/迁移价值，配合 FR-2.3）；② 极简单机场景不想常驻管理面。首期 static 模式采用"a + b"组合：托管进程 + hot restart 协调，文档明确其语义。
- M0 原型即以"抽象协议 → static YAML"编译器起步，编译器本身与下发方式无关（单向编译原则），不存在返工。

### 2.3 配置校验（FR-3.3）——✅ 可行，双层校验

1. **进程外静态校验**：管理面基于 Envoy 官方 protobuf（Go 生态 `go-control-plane/envoy` 包）做结构与 PGV（protoc-gen-validate）规则校验，可拦截绝大多数字段级错误。
2. **envoy 二进制校验**：`envoy --mode validate -c <file>` 对渲染产物做与运行时一致的完整校验（static 路线直接可用；xDS 路线可将 snapshot 渲染为等价 static 配置后校验，作为发布前可选步骤）。
3. 抽象协议层自身有 JSON Schema 校验（FR-1.7），在编译前拦截用户输入错误。

三层递进，"非法配置不出管理面"（NFR-3）可达成。

### 2.4 实时状态采集与统计（FR-4.3 / FR-4.4）——✅ 成熟

Envoy admin API 提供全部所需数据源，只读拉取即可，无需数据面配合改造：

| 需求 | admin API 端点 |
|---|---|
| 生效 listeners/clusters/routes/secrets | `/config_dump`（含 EDS: `?include_eds`） |
| endpoint 健康状态 | `/clusters?format=json` |
| 请求量/延迟/连接数统计 | `/stats?format=json`、`/stats/prometheus` |
| 证书信息 | `/certs` |

注意点：admin 接口无鉴权，**必须约束绑定在 localhost 或 unix socket**，由管理面代理访问——这是部署拓扑设计的硬约束（见架构文档 §5）。统计的时间序列展示首期可由管理面内存环形缓冲轻量实现，无需引入 Prometheus 依赖（NFR-2 轻量目标），同时暴露 `/stats/prometheus` 透传供外部采集。

### 2.5 抽象协议设计（FR-1）——⚠️ 可行，是本项目最大设计风险

这不是"能不能实现"的问题，而是"抽象取舍是否得当"的问题（需求 R1）。可行性依据：

- 前有可参考的成功先例：Caddy（极简 site → upstream 模型）、Traefik（router/service/middleware 三概念）、k8s Gateway API（Gateway/Route/Backend 分层）均证明"少量名词覆盖 80% 场景"可行；
- 需求已内置逃生舱机制（FR-1.4 escape hatch：内嵌原生 Envoy 片段/patch），使表达力上限等于 Envoy 本身，抽象不足不会形成硬阻塞；
- Envoy 的配置模型本身层级清晰（listener→route→cluster），抽象协议的 `Listener/Route/Upstream/Policy` 与之存在自然映射，编译器不需要复杂的语义变换。

**缓解措施**：协议设计单独出一份 system_design 文档，先做纸面演练——用协议草案完整表达 S1/S2 两个典型场景，再对照 nginx/Caddy/Traefik/Gateway API 逐能力核对取舍，M0 用真实编译器验证。

### 2.6 k8s 环境感知（FR-6）——✅ 成熟

client-go 探测 ServiceAccount 即可判定环境；List/Watch Service/Endpoints 是最基础的 k8s API 用法。封装为可选模块（构建上通过接口隔离，运行时按探测结果启停），非 k8s 环境零依赖，符合"增强不是依赖"原则。风险低，且优先级 P1/P2，不影响 MVP。

### 2.7 管理面轻量化（NFR-2）——✅ 可达

Go 单二进制 + embed 前端静态资源 + 内嵌存储（SQLite 或 bbolt），无外部运行时依赖。同类 Go 控制面（如 Traefik ~80MB RSS 量级）证明 <150MB 内存目标现实可行。需要注意 go-control-plane SnapshotCache 与 k8s informer 的内存占用，在单实例、配置规模适中（数百 route 级别）的目标场景下无压力。

## 3. 生态位可行性（竞品对照）

| 产品 | 与本项目的差异 | 是否构成直接竞争 |
|---|---|---|
| Nginx / Nginx Plus | 无免费专业控制台；配置语法古老；动态能力弱 | 否——本项目正是其开源替代目标 |
| Caddy / Traefik | 自研数据面，性能与协议能力（HTTP/3、gRPC、细粒度 L7）弱于 Envoy；Traefik 控制台只读 | 部分重叠，但数据面能力是差异点 |
| Kong / APISIX | 基于 OpenResty 自研数据面；企业版才有完整控制台/高级能力；概念面向插件生态较重 | 部分重叠；本项目以 Envoy 数据面 + 全开源控制台差异化 |
| Istio / Envoy Gateway / Contour | 强绑定 k8s CRD，脱离 k8s 不可用 | 否——恰是本项目要填补的空白 |
| Envoy 原生 + 手写配置 | 无控制面、无 UI、门槛极高 | 否——是被改善对象 |

**结论**："平台中立的 Envoy 独立控制面 + 开源专业控制台"这一生态位当前确实空缺，定位成立。主要外部风险是 Envoy Gateway 未来可能推出非 k8s 模式，但其社区路线图深度绑定 Gateway API/CRD 模型，短中期概率低；且本项目的抽象协议与控制台体验是独立价值。

## 4. 工程可行性

- **技术栈集中**：管理面 Go 单语言（xDS、k8s、校验、进程托管全部有 Go 官方/一线库），前端一套 SPA，无多语言协作成本。
- **里程碑切分合理**：M0（协议草案 + 编译器原型 + CLI 驱动）不含 UI 与 xDS，是纯库 + 命令行工程，可快速验证最大风险点（协议表达力）；M1 在 M0 的 IR 之上叠加 xDS 下发与控制台，路径递增无推翻式返工。
- **单向编译架构降低耦合**：协议、编译器、下发层、UI 可并行推进，适合 sprint 拆分。
- **测试可自动化**：编译器可做 golden file 快照测试；`envoy --mode validate` 与容器化 Envoy 可进 CI 做多版本兼容验证（对应 R5）。

## 5. 风险清单与缓解（汇总）

| # | 风险 | 等级 | 缓解 | 验证时机 |
|---|---|---|---|---|
| RK1 | 抽象协议表达力取舍失当（=需求 R1） | 高 | 80/20 原则 + escape hatch；协议专项设计文档 + 纸面演练 S1/S2；M0 编译器实证 | M0 |
| RK2 | static 模式 hot restart 协调复杂（=需求 R2） | 中 | xDS 为默认；static 首期限定"托管进程 + hot restart"单一路径并文档化语义 | M1 |
| RK3 | 部署拓扑分叉导致进程托管/状态采集设计发散（=需求 R3） | 中 | 概要设计固定 3 种标准拓扑（见架构文档 §5），其余不支持 | 概要设计（本轮） |
| RK4 | Envoy/xDS 版本演进兼容（=需求 R5） | 中 | 锁定支持版本区间（初步：跟随 Envoy 最近 2~3 个 minor）；CI 多版本 validate | M1 起持续 |
| RK5 | admin API 无鉴权带来的安全暴露 | 中 | 硬性要求 admin 绑定 localhost/uds，由管理面代理；文档与默认配置强制 | M1 |
| RK6 | 前端控制台工作量被低估 | 中 | MVP 控制台严格圈定 P0（配置管理 + 实时状态视图）；表单化编辑先覆盖抽象协议核心对象 | M1 |

## 6. 关键技术验证点（M0 PoC 清单）

M0 阶段必须用代码回答的问题：

1. 抽象协议 v0 能否完整表达 S1（多域名 TLS 反代）与 S2（API 网关：路由/重写/限流/JWT/CORS/超时重试）？——协议表达力实证。
2. 协议 → IR → Envoy static YAML 编译器：产物通过 `envoy --mode validate` 且实际跑通 S1 流量。
3. 同一 IR 喂给 go-control-plane SnapshotCache，Envoy 通过 ADS 正常拉起 S1 配置（最小 spike，可后置到 M1 初）。
4. escape hatch 机制原型：在协议中内嵌一段原生 filter 配置并正确合成到产物。

## 7. 许可证可行性

- 管理面核心依赖（go-control-plane: Apache-2.0、client-go: Apache-2.0、Envoy 本体: Apache-2.0、主流前端框架: MIT）均与 AGPL-3.0 项目**单向兼容**（宽松许可代码可被包含进 AGPL 项目）。
- 需要建立依赖许可证扫描（CI 中禁止引入 GPL-不兼容或专有许可依赖）。
- Envoy 官方二进制/镜像按 Apache-2.0 原样分发或引导用户自取，不构成传染问题。

**结论**：AGPL-3.0 选择无兼容性障碍。

## 8. 总体结论与工作方向

1. **项目可行**，立即进入概要设计与 M0 开发。
2. **总工作方向**：以"单一 IR、单向编译"为架构主轴——协议层（抽象协议 + 原生 static）→ IR → 下发层（xDS 优先、static 并行），控制台与状态采集只读旁路挂接 admin API。
3. **优先级方向**：xDS 是第一公民；static 是导出/极简场景补充；k8s 感知严格保持可选。
4. **首要攻坚点**：抽象协议设计（RK1），作为下一份专项设计文档，先于大规模编码。
5. 基础系统架构方向见配套文档 [`260715-2-overall-architecture.md`](260715-2-overall-architecture.md)。

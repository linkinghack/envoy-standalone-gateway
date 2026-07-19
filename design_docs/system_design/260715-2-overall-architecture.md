# 260715-2 总体架构概要设计（Overall Architecture）

- **项目**: envoy-standalone-gateway
- **上游文档**: [`requirements/01-initial-requirements-analysis.md`](../requirements/01-initial-requirements-analysis.md)、[`260715-1-feasibility-analysis.md`](260715-1-feasibility-analysis.md)
- **文档状态**: 初稿（Draft）
- **日期**: 2026-07-15
- **性质**: 概要设计（High-Level Design）。定下总体架构方向、模块划分、关键技术选型与标准部署拓扑；各模块的详细设计后续单独出文档。

---

## 1. 架构总纲

### 1.1 核心原则（承接需求 §4 并具体化）

| # | 原则 | 含义与推论 |
|---|---|---|
| A1 | **单一 IR，单向编译** | 所有配置来源（抽象协议 / 原生 static）编译为同一份内部 Envoy 配置对象模型（IR = go-control-plane 的 Envoy protobuf 资源集合）；下发层只消费 IR。协议演进、下发方式、UI 互相解耦 |
| A2 | **数据面零侵入** | 官方 Envoy 发行版，不 fork、不打补丁；一切控制通过 bootstrap / xDS / admin API 达成 |
| A3 | **管理面不在数据路径** | 管理面宕机不影响流量（xDS 语义天然保证）；状态采集只读 |
| A4 | **k8s 是可选增强** | k8s 能力封装在独立 discovery 模块后的接口之内，编译层与下发层对其无感知 |
| A5 | **文件为真源（file as source of truth）** | 用户配置以声明式文件形态持久化，可 git 管理；内嵌 DB 只存运行态与索引类数据（会话、版本元数据、统计缓存） |
| A6 | **单二进制、零外部依赖** | 前端资源 embed 进二进制；存储内嵌；一个进程即完整管理面 |

### 1.2 系统上下文

```
                 ┌─────────────────────────────────────────────────────┐
   浏览器 ──────►│  esgw (管理面, 单二进制 Go 进程)                       │
   REST/CLI ───► │                                                     │
                 │  ┌───────────┐   ┌───────────┐   ┌───────────────┐  │
                 │  │ API 层     │──►│ 配置域     │──►│ 编译层         │  │
                 │  │ (REST+UI) │   │(协议/版本) │   │ (→ IR + 校验)  │  │
                 │  └─────┬─────┘   └───────────┘   └──────┬────────┘  │
                 │        │                                │ IR        │
                 │        │         ┌──────────────────────┴────────┐  │
                 │        │         │ 下发层                          │  │
                 │        │         │  · xDS Server (ADS)            │  │
                 │        │         │  · static YAML 渲染 + 进程协调   │  │
                 │        │         └──────┬─────────────────────────┘  │
                 │        │                │                            │
                 │  ┌─────▼───────────┐    │    ┌────────────────────┐  │
                 │  │ 状态采集层        │    │    │ 环境感知(可选)       │  │
                 │  │ (admin API 拉取) │    │    │ (k8s discovery)     │  │
                 │  └─────┬───────────┘    │    └────────────────────┘  │
                 └────────┼────────────────┼────────────────────────────┘
                          │ admin API(只读) │ xDS(gRPC) / static yaml + hot restart
                          ▼                ▼
                 ┌─────────────────────────────────────────────────────┐
                 │              数据面: 原生 Envoy (官方发行版)             │
                 └─────────────────────────────────────────────────────┘
```

（`esgw` 为管理面二进制暂用名，正式命名后续决议。）

## 2. 关键技术选型

| 决策点 | 选型 | 理由 | 状态 |
|---|---|---|---|
| 管理面语言 | **Go** | go-control-plane（xDS）、client-go（k8s）、Envoy protobuf Go 绑定齐备；单二进制交叉编译满足 NFR-7 | 定 |
| xDS 实现 | **go-control-plane**（ADS + SotW SnapshotCache 起步） | 官方维护、生产验证充分；Delta xDS 后续按需演进 | 定 |
| IR 表示 | go-control-plane 的 Envoy v3 protobuf 资源集合（`envoy.config.*.v3`） | 不自造中间模型，编译产物即下发载荷，校验可复用 PGV | 定 |
| 配置持久化 | 声明式文件（YAML）为真源 + **SQLite**（纯 Go 驱动 modernc.org/sqlite）存版本/会话/审计等运行态 | A5 原则；SQLite 对结构化查询（版本 diff、审计）比 bbolt 友好 | 定（D2 已终审，[`260717-2-config-domain-design.md`](260717-2-config-domain-design.md) §7） |
| Web 前端 | **React + TypeScript + Vite**，组件库倾向 Ant Design；YAML 编辑器用 Monaco/CodeMirror | 表单密集型控制台的成熟生态；候选备选 Vue 由前端启动 sprint 终审 | 倾向 |
| API 风格 | REST + JSON（OpenAPI 描述），控制台与 CLI 共用同一 API | 简单通用；xDS 才需要 gRPC，管理 API 不需要 | 定 |
| 控制台鉴权 | 本地账号 + Cookie Session（首期），预留 OIDC 接口 | FR-4.6 | 定 |
| Envoy 版本策略 | 锁定支持区间：跟随 Envoy 最近 2~3 个 minor 版本，CI 多版本 `--mode validate` | NFR-6 / R5 | 定 |

## 3. 模块划分（管理面内部）

按 Go 包/领域划分，编号即后续详细设计文档的模块索引：

| 模块 | 职责 | 关键依赖 | 对应需求 |
|---|---|---|---|
| M-API | REST API、OpenAPI、鉴权与会话、静态资源服务（embed 前端） | 配置域、状态采集 | FR-4.x |
| M-CONF 配置域 | 抽象协议对象与原生 static 配置的 CRUD、版本管理（历史/diff/回滚）、发布流状态机（草稿→校验→发布→生效确认） | 存储、编译层 | FR-1、FR-2、FR-3.4、FR-4.5 |
| M-COMPILE 编译层 | 抽象协议 → IR 编译器；原生 static → IR 解析器；escape hatch 合成；三层校验（JSON Schema / PGV / envoy validate） | go-control-plane protobuf | FR-1、FR-2、FR-3.3 |
| M-DELIVER 下发层 | xDS Server（ADS，SnapshotCache 原子换版）；static 渲染器 + hot restart 协调 | 编译层 IR | FR-3.1、FR-3.2、FR-3.3 |
| M-PROC 进程托管（可选启用） | 本机 Envoy 启动/守护/hot-restart epoch 管理；也支持"仅下发"模式 | — | FR-3.5 |
| M-STATE 状态采集 | admin API 轮询/按需拉取：config_dump、clusters、stats；统计的内存时间序列缓冲；Prometheus 透传 | Envoy admin API | FR-4.3、FR-4.4、FR-5.5 |
| M-DISCO 环境感知（可选启用） | k8s 探测；Service/Endpoints List-Watch；向配置域提供 upstream 候选、向下发层提供 EDS 动态端点 | client-go | FR-6 |
| M-STORE 存储 | 配置文件真源读写、SQLite 运行态、证书/私钥加密存储 | — | FR-5.4、NFR-5 |
| M-CORE 内核 | 生命周期编排、模块装配、结构化日志、健康检查、自身 metrics | — | FR-5.5 |

模块间依赖单向：`M-API → M-CONF → M-COMPILE → M-DELIVER`；M-STATE、M-DISCO 为旁路，任何模块不得反向依赖 M-API。

## 4. 核心数据流

### 4.1 配置发布流（主链路）

```
用户提交(UI表单/YAML/原生static)
  → M-CONF 保存草稿(文件真源) 
  → M-COMPILE 校验+编译 → IR (+escape hatch 合成)
  → [可选] envoy --mode validate 终检
  → M-CONF 生成新版本号，记录 diff
  → M-DELIVER 原子切换:
       xDS: SnapshotCache.SetSnapshot(node, v_new)   ← 整体生效或不生效
       static: 渲染 YAML → M-PROC 触发 hot restart
  → M-STATE 从 config_dump 确认生效版本 → 发布流闭环(FR-4.5)
```

### 4.2 状态视图流（旁路只读）

```
M-STATE 定时/按需拉取 admin API(localhost/uds)
  → 归一化为状态模型(listeners/clusters/endpoints健康/routes/certs/stats)
  → 内存缓存 + 环形时间序列
  → M-API 提供给控制台；/metrics 透传 Prometheus
```

## 5. 标准部署拓扑（回应需求 R3，固定三种）

| 拓扑 | 形态 | 进程托管 | 适用 |
|---|---|---|---|
| T1 单机托管 | 同一主机：systemd 拉起 esgw，esgw 托管 Envoy 子进程；或 docker 单容器（esgw 为 PID 1） | M-PROC 启用 | S1/S3，Nginx 替代主场景 |
| T2 同机分离 | esgw 与 Envoy 各自为 systemd unit / compose 内两容器；xDS 走 localhost，admin 走 localhost/uds | 仅下发模式 | 希望独立管理 Envoy 生命周期的用户 |
| T3 k8s Pod | Deployment：esgw 容器 + envoy 容器同 Pod（localhost 互通），或 esgw 单独 Deployment 驱动多个 Envoy Pod（xDS over mTLS，P2 多实例） | 仅下发模式 | S4 |
| 约束 | Envoy admin 接口一律绑定 127.0.0.1 或 unix socket，仅 esgw 可达（RK5）；跨主机 xDS（远期多实例）强制 mTLS（NFR-5） | | |

M1 交付 T1、T2；T3 随 FR-5.3（P1）交付。

## 6. 抽象协议方向（占位，专项文档另出）

- 顶层对象沿用需求 FR-1.1 五名词：`Gateway` / `Listener` / `Route` / `Upstream` / `Policy`，带 `apiVersion` 版本化。
- 设计基准：对照 nginx / Caddy / Traefik / Gateway API 逐能力核对；以 S1、S2 场景纸面演练通过为出稿门槛。
- escape hatch 形态初步方向：对象级 `envoyPatch`（JSON Patch / merge patch 语义作用于编译产物）+ 顶层原生片段内嵌，两种粒度。
- 本节仅定方向，协议规范为下一份 system_design 专项文档（M0 首要任务，对应 RK1）。

## 7. 交付形态

| 形态 | 内容 | 里程碑 |
|---|---|---|
| Linux 二进制 + systemd unit + 安装脚本 | linux/amd64、linux/arm64 | M1 |
| Docker 镜像 | `esgw`（管理面）与 `esgw-all-in-one`（含 Envoy，T1 单容器） | M1 |
| Helm chart / k8s manifests | T3，不含任何 CRD | M2 |
| 协议 JSON Schema + 规范文档 | 可被第三方独立实现 | M2 |

## 8. 后续设计文档清单（按优先序）

1. **抽象协议规范 v0**（system_design，M0 前置，攻坚 RK1）——✅ [`260716-1-gateway-config-protocol-v0.md`](260716-1-gateway-config-protocol-v0.md)
2. 编译层与 IR 设计（含 escape hatch 合成、三层校验细节）——✅ [`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md)
3. 下发层设计（xDS snapshot 生命周期；static 模式 hot restart 协调协议）——✅ [`260717-1-deliver-layer-design.md`](260717-1-deliver-layer-design.md)
4. 配置域与发布流设计（版本模型、diff、回滚、文件真源布局）——✅ [`260717-2-config-domain-design.md`](260717-2-config-domain-design.md)
5. 控制台信息架构与 API 契约（OpenAPI）——✅ [`260717-3-console-api-design.md`](260717-3-console-api-design.md)
6. 状态采集与统计模型设计——✅ [`260717-4-state-collection-design.md`](260717-4-state-collection-design.md)
7. k8s 环境感知模块设计（P1 阶段）——✅ [`260717-5-k8s-disco-design.md`](260717-5-k8s-disco-design.md)

## 9. 未决事项（Open Decisions）

| # | 事项 | 计划决策时机 |
|---|---|---|
| D1 | 项目/二进制正式命名（暂用 esgw） | M0 期间 |
| D2 | SQLite vs bbolt 终审 | **已终审：SQLite**（[`260717-2-config-domain-design.md`](260717-2-config-domain-design.md) §7） |
| D3 | 前端框架终审（React 倾向 vs Vue） | 控制台 sprint 启动时 |
| D4 | xDS SotW vs Delta 演进时机 | M2 后按规模需求 |
| D5 | 证书私钥加密方案（本地密钥加密 vs 对接外部 secret） | M1 期间 |

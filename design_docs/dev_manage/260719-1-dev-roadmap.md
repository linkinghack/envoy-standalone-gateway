# 260719-1 开发路线图与冲刺规划（Dev Roadmap）

- **项目**: envoy-standalone-gateway
- **上游文档**: [`../requirements/01-initial-requirements-analysis.md`](../requirements/01-initial-requirements-analysis.md)（§9 里程碑草案）、[`../system_design/260715-2-overall-architecture.md`](../system_design/260715-2-overall-architecture.md)（模块划分与后续设计清单）
- **文档状态**: 生效（Active，随冲刺推进更新）
- **日期**: 2026-07-19

---

## 1. 设计完整度结论（开工依据）

architecture §8 计划的全部 7 份模块详细设计已完成并交叉互认，重大选型（Go / go-control-plane / SQLite / spec-first OpenAPI）均已终审。剩余未决项分三类，均不阻塞开发：

1. **编码期决策项**（C1~C4、DL1/DL4/DL6、CD1~CD3 等）：各设计文档已标注决策时机，由对应冲刺的 task 落地并回写文档；
2. **延后终审项**（D1 命名、D5 证书加密）：到达对应冲刺时终审；D3/AD1 已在 [M1 工程实施计划 §2](engineering_plan/260722-1-m1-completion-plan.md#2-终审决策) 终审为 React + TypeScript + Vite + shadcn/ui；
3. **小缺口**：M-CORE 装配层无独立设计文档、`esgw.yaml` 完整配置 schema 未定——随 S2 冲刺补齐（见 §3）。

## 2. 冲刺组织约定

- 冲刺以**范围**为边界而非固定时长：一个冲刺 = 一组可独立验收的交付物；完成验收即关闭并开启下一冲刺。
- 每个冲刺按 [`sprints/README.md`](sprints/README.md) 规范建目录：`requirements.md`、`technical_design.md`、`plan_todos_trace.md`、`tasks/`。
- 冲刺内 task 是**移交给 AI 工程师（Codex / Claude Code）的最小执行单元**：每个 task 文档必须自包含（目标、上游设计引用、执行步骤、验收标准、进展记录），使新会话零上下文即可接手。
- 沉淀性内容（ADR、补充设计、工程规范）写入 `dev_manage/dev_design/`，不留在冲刺目录。
- git 提交小步清晰，每个 task 至少一个独立 commit；不积攒巨大 commit（AGENTS.md 要求）。

## 3. 冲刺序列

| Sprint | 里程碑 | 主题 | 范围概要 | 主要设计依据 | 前置 |
|---|---|---|---|---|---|
| **S1 (260719)** | M0 | 协议与编译器 | `internal/protocol` 五对象类型/strict decode/JSON Schema；`internal/compile` F1~F6 流水线 + escape hatch；static 渲染；`esgw compile` CLI；golden file 测试 + envoy validate + S1 真实流量 e2e | [协议 v0](../system_design/260716-1-gateway-config-protocol-v0.md)、[编译层](../system_design/260716-2-compile-ir-design.md) | — ✅ 已完成（2026-07-20，A1~A8 核验见 [sprints/260719](sprints/260719/plan_todos_trace.md)） |
| **S2 (260720)** | M1 | xDS 下发与运行时骨架 | M-CORE 装配/生命周期（**补轻量设计文档 + esgw.yaml schema**）；`deliver/xds`（ADS server、FromIR、ACK/NACK）；接入 bootstrap 生成与 `esgw bootstrap` 命令；ADS 拉起 S1 配置 e2e（M0 验收第 3 项在此闭环） | [下发层](../system_design/260717-1-deliver-layer-design.md) §1~§3、§6 | S1 ✅ 已完成（2026-07-20，A1~A8 核验见 [sprints/260720](sprints/260720/plan_todos_trace.md)；远端 CI 五 job 全绿） |
| **S3 (260721)** | M1 | 配置域与发布流 | M-STORE（SQLite/迁移）；M-CONF（文件真源加载、Origin 定位 CRUD、版本快照、双视角 diff、回滚、发布流状态机、fsnotify 外部修改检测）；原生 static → IR 解析入口（FR-2.1） | [配置域](../system_design/260717-2-config-domain-design.md) | S1；✅ 已完成并于 2026-07-22 独立复核（[验收证据](sprints/260721/plan_todos_trace.md)）：T1~T6 直接测试覆盖存储、草稿/快照、发布状态机、native/diff/回滚/watch/CRUD；S4 骨架提前落地 |
| **S4 (260722)** | M1 | 状态采集与生效确认 | M-STATE（admin client 白名单/串行调度、状态归一化、归属反解、确认快速通道、时间序列环形缓冲、Prometheus 透传）；发布流 CONFIRMING→EFFECTIVE 闭环联调 | [状态采集](../system_design/260717-4-state-collection-design.md) | S2、S3；✅ 已完成并于 2026-07-22 独立复核（[验收证据](sprints/260722/plan_todos_trace.md)）：T1~T4 直接测试与真实 Envoy 门禁均通过；S5 接入 API |
| **S5 (260723)** | M1 | 管理 API | spec-first `api/openapi.yaml` + 生成一致性门禁；鉴权/会话/CSRF/引导；配置域、状态、证书库 P0 REST 端点；SPA 静态资源服务骨架 | [控制台 API](../system_design/260717-3-console-api-design.md) §3~§5、[M1 工程实施计划](engineering_plan/260722-1-m1-completion-plan.md) | S3、S4；✅ 已完成（2026-07-22，[A1~A7 验收](sprints/260723/plan_todos_trace.md)）：42 operation 全实现、生成 clean-diff、唯一 `core.App`、HTTP 发布闭环和全量质量门禁通过 |
| **S6 (260724)** | M1 | 控制台 P0 | React + TypeScript + Vite + shadcn/ui；配置五对象索引与完整 YAML 编辑、运行状态视图、简版发布流、证书库、专家模式、编译产物、系统信息 | [控制台 API](../system_design/260717-3-console-api-design.md) §2、§6、[M1 工程实施计划](engineering_plan/260722-1-m1-completion-plan.md) | S5；已完成（2026-07-22，T1~T5、A1~A7 全部通过，[任务追踪](sprints/260724/plan_todos_trace.md)） |
| **S7 (260725)** | M1 | static 模式与进程托管 | M-PROC（发现/启动/接管/崩溃退避）；hot restart 协调协议（DL1/DL4/DL6 实测定）；static Apply 路径与生效语义 | [下发层](../system_design/260717-1-deliver-layer-design.md) §4~§5、[M1 工程实施计划](engineering_plan/260722-1-m1-completion-plan.md) | S2/S6；✅ 已完成（2026-07-22，T1~T5、A1~A8 全通过，[任务追踪](sprints/260725/plan_todos_trace.md)） |
| **S8 (260726)** | M1 | 交付物与 M1 收口 | systemd unit + 安装脚本；Docker 镜像（esgw / all-in-one）；AD8 默认监听与引导参数终审；跨模块 e2e 矩阵（T1/T2 拓扑 × xds/static）；用户文档首版 | 架构 §7、需求 FR-5、[M1 工程实施计划](engineering_plan/260722-1-m1-completion-plan.md) | S6、S7；✅ 已完成（2026-07-22，T1~T5、A1~A8 全通过，[任务追踪](sprints/260726/plan_todos_trace.md)） |
| S9+ | M2 | 统计视图、版本历史/回滚 UI 完整版、k8s disco（M-DISCO + T3/Helm）、协议规范对外发布、P1 策略补全（L4/mTLS/熔断等） | 按 P1 需求与真实反馈排期，届时拆分冲刺 | [k8s disco](../system_design/260717-5-k8s-disco-design.md) 等 | M1 |

依赖说明：S3 与 S2 可并行（都只依赖 S1 的 IR 契约）；S7 与 S4~S6 可并行。单人/单 agent 顺序推进则按表序执行。

## 4. 里程碑验收锚点

| 里程碑 | 验收标准（源自需求 §9 与可行性 §6） |
|---|---|
| M0（S1 末 + S2 初） | ① S1/S2 场景 YAML 经编译器产出 static 配置，过 `envoy --mode validate` 且跑通真实流量；② escape hatch 两种形态合成正确；③ 同一 IR 经 ADS 正常拉起（S2 冲刺完成）✅ ③ 已闭环（2026-07-20，S2 e2e：真实 Envoy 以接入 bootstrap 连 ADS 拉起 S1 场景，四流量断言全过，版本一致性证据见 [sprints/260720](sprints/260720/plan_todos_trace.md) A4 行） |
| M1（S8 末） | ✅ 已完成：P0 需求全集——xDS + static 双模式下发、控制台配置管理与实时状态、本地账号鉴权、systemd/Docker 部署、FR-5.6 管理面故障不影响数据面；证据见 [M1 矩阵](sprints/260726/extra_docs/m1-requirements-matrix.md) |
| M2 | P1 全集：统计视图、版本回滚 UI、k8s 环境感知、Helm chart、协议规范文档 |

# Sprint 260720 需求（S2 / M1：xDS 下发与运行时骨架）

- **冲刺编号**: 260720（路线图中的 S2）
- **里程碑**: M1（[路线图](../../260719-1-dev-roadmap.md) §3 S2 行）；同时闭环 M0 验收第 3 项「同一 IR 经 ADS 正常拉起」
- **状态**: 已完成（2026-07-20；A1~A8 全部核验达成、远端 CI 五 job 全绿，见 [plan_todos_trace.md](plan_todos_trace.md)）

---

## 1. 要解决的问题

M0 已验证「协议 → 编译器 → static 配置 → 真实流量」链路，但配置下发只有静态文件一条腿。项目的默认主场景（路线图矩阵 ①，T1 拓扑）是 **xDS 动态下发**：变更零重启、管理面故障不影响数据面（FR-5.6）为协议原生语义。本冲刺交付 xDS 下发通道与最小运行时骨架：

1. `deliver/xds`：IR → go-control-plane Snapshot 装配（纯函数）、ADS gRPC server、ACK/NACK 跟踪；
2. M-CORE 装配层骨架 + `esgw.yaml` 配置 schema（路线图 §1 明确的两个小缺口，随本冲刺补齐为沉淀性设计文档）；
3. 接入 bootstrap 生成与 `esgw bootstrap` 导出命令（T2/T3 拓扑的交付物）；
4. ADS e2e：真实 Envoy 以接入 bootstrap 连上 esgw，跑通 S1 场景真实流量——闭环 M0 验收第 3 项。

## 2. 冲刺目标

| # | 目标 | 对应设计 |
|---|---|---|
| G1 | M-CORE 装配/生命周期轻量设计文档 + `esgw.yaml` schema（沉淀至 `dev_design/`）+ `internal/config` 加载/默认值/校验 | [下发层 §1.3/§2.7/§7](../../../system_design/260717-1-deliver-layer-design.md)、架构 §3 |
| G2 | `internal/deliver` Deliverer 接口与状态/事件模型；`deliver/xds` FromIR 纯函数装配 + Consistent 自检 | 下发层 §2.1、§6 |
| G3 | ADS gRPC server（SotW SnapshotCache，单流复用五类型）+ ACK/NACK 跟踪 + `esgw serve` 命令（M-CORE 骨架接线） | 下发层 §2.2~§2.6、§6.1 |
| G4 | 接入 bootstrap 生成（纯函数）+ `esgw bootstrap --mode xds` 导出命令 | 下发层 §2.7 |
| G5 | ADS e2e：esgw serve + 真实 Envoy（接入 bootstrap）跑通 S1 场景流量断言 | 下发层 §2、编译层 §8 |

## 3. 范围外（本冲刺不做）

- M-PROC 进程托管、hot restart、崩溃退避（S7；`proc.*` 配置键只在 schema 文档中预留，不实现）
- static 模式运行时下发（渲染落盘 + 热重启生效，S7）；`esgw serve` 本冲刺只支持 `deliver.mode: xds`
- EDS 快速通道 `UpdateEndpoints` 的真实端点更新（契约随接口定义，实现返回「未支持」错误；随 M-DISCO/k8s 冲刺落地，下发层 §3）
- M-CONF/M-STORE（文件真源 CRUD、版本快照、发布流状态机，S3）；本冲刺 serve 以 `-f <config-dir>` 直接加载配置目录，是过渡形态
- M-STATE 生效确认闭环（S4）：Apply 受理后**不**调 `ExpectVersion`，`Status.Phase` 停留在 `awaiting_confirm` 的完整语义留 S4；本冲刺 ACK 跟踪仅驱动 `nacked` 与日志/metric 计数
- 跨主机 xDS mTLS、多节点（FR-3.6，P2；非 loopback 监听无 TLS 的启动硬校验**在范围内**）
- S2 场景（jwt）的 ADS 真实流量断言（jwks 外部依赖；沿用 S1 场景即可证明下发链路）

## 4. 验收标准

| # | 标准 | 验证方式 |
|---|---|---|
| A1 | FromIR 为纯函数：IR 五类资源映射正确（命名规约 lis/rc/us/CLA/crt）、`Consistent()` 自检通过；引用不闭合的构造性反例报错 | 表驱动单测 |
| A2 | `esgw serve`（xds 模式）启动后，真实 Envoy 以接入 bootstrap 连上 ADS，拉取到 LDS/RDS/CDS/EDS/SDS 全类型资源且版本 = IR.Version | e2e（admin config_dump 断言） |
| A3 | ADS 下发下 S1 场景真实流量四断言全过：TLS 终止 + 域名分流 + HTTP→HTTPS 301 + SNI 集合外握手失败 | e2e（docker compose + curl） |
| A4 | 同一配置目录：static 产物与 ADS 下发的 IR.Version 一致（M0 验收第 3 项闭环证据） | e2e 断言（对比 golden 版本串） |
| A5 | ACK/NACK 跟踪：e2e 中全类型 ACK 可见（日志/Status 单测）；NACK 路径有单测（注入 ErrorDetail 的 DiscoveryRequest → Event{Nacked} + Status.Phase=nacked + 不自动重推） | 单测 + e2e 日志断言 |
| A6 | 幂等跳过：重复 Apply 相同 IR.Version 直接成功且不触发换版推送 | 单测 |
| A7 | `esgw bootstrap --mode xds -o <file>` 产物被真实 Envoy 接受（e2e 复用）；`deliver.xds.listen` 非 loopback 且无 tls → serve 启动报错拒绝裸奔 | e2e + 单测 |
| A8 | CI 全绿；`make build/test/lint` 与 ADS e2e 本地可复现 | CI |

## 5. 本冲刺内需落地的设计未决项

| 未决项 | 内容 | 落地 task |
|---|---|---|
| —（无强制项） | DL1/DL4/DL6 归 S7（hot restart 联调期）；CD3 归 S4（确认超时）；D5 归 M-STORE | — |

本冲刺无必须终审的上游未决项；实现期产生的决议（依赖版本、API 适配、默认值取舍）按工程基线 §4 记录于 `plan_todos_trace.md` 决议表，必要时回写上游设计文档。

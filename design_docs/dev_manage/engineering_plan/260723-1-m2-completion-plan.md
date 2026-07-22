# 260723-1 M2 / 1.0 完成计划

- **状态**：生效（Active）
- **范围**：在 M1 的双下发、配置域、状态域、控制台和交付物基础上完成需求文档全部 P1，形成可发布的 1.0。
- **上游**：[`../260719-1-dev-roadmap.md`](../260719-1-dev-roadmap.md)、[`../../requirements/01-initial-requirements-analysis.md`](../../requirements/01-initial-requirements-analysis.md)、[`../../system_design/260716-1-gateway-config-protocol-v0.md`](../../system_design/260716-1-gateway-config-protocol-v0.md)、[`../../system_design/260717-5-k8s-disco-design.md`](../../system_design/260717-5-k8s-disco-design.md)

## 1. 完成口径

M2 不是把现有 API/UI 标记为“已有”即可关闭。每条 P1 必须同时满足：协议语义不降级、static/xDS 两种产物一致、真实 Envoy 流量证据、API 与控制台主路径可操作、安装/容器/Kubernetes 交付可重复、需求—实现—测试矩阵可追溯。

原始 FR-1.6 写明 TCP/UDP，虽协议 v0 曾把 UDP 标作 P2，M2 按更严格的上游需求执行，TCP、TLS passthrough 与 UDP 一并交付。P2 的多节点、OIDC、审计、混合原生配置、HTTP/3 等不借 M2 隐式扩 scope。

## 2. 冲刺拆分

| Sprint | 主题 | 关闭的需求 |
|---|---|---|
| S9 / 260727 | P1 数据面与协议发行 | FR-1.3、FR-1.4、FR-1.6、FR-1.7；补齐 extAuth/IPAccess、限流 key、L4、下游 mTLS 与熔断证据 |
| S10 / 260728 | 版本、发布与统计控制台 | FR-3.4、FR-4.2、FR-4.4、FR-4.5；完整双向编辑、diff/校验/发布/确认、历史/回滚、统计视图 |
| S11 / 260729 | Kubernetes 与可观测交付 | FR-5.3、FR-5.5、FR-6.1、FR-6.2；in-cluster discovery、Service 候选、T3a manifests/Helm、机器采集契约 |
| S12 / 260730 | 1.0 稳定化与 M2 收口 | P1 全矩阵、迁移/兼容、资源与安全复核、真实集群/Envoy/Web e2e、1.0 发行物 |

## 3. 跨冲刺约束

1. Go 协议类型仍是 schema 单一事实来源；独立发布的 schema/spec 必须有 clean-diff 门禁。
2. 新策略不得静默忽略字段。无法无损映射时应在 load/link/build 阶段给出定位错误并修订设计，不能退化成近似行为。
3. 新 Envoy 资源进入 static/xDS 共用 IR，禁止为某下发模式维护平行编译器。
4. Kubernetes 仅通过 M-DISCO 接口进入组合根；协议、编译器和非 k8s 运行路径不得依赖 client-go 具体类型。
5. 每个 task 至少一个独立 commit；任务文档在开工、发现偏差、验收完成时同步更新。

## 4. M2 总门禁

- Go：gofumpt、build、unit、race、vet、golangci-lint、fuzz smoke；
- 协议：schema/spec clean-diff、示例 conformance、破坏性变更检查；
- Envoy：支持矩阵 validate，HTTP/L4/策略真实流量，static/xDS 等价性；
- Web：generate、typecheck、Vitest、build、桌面/移动 Playwright；
- Kubernetes：kind 双版本 smoke、Helm lint/template、RBAC 最小权限、非 k8s 零行为回归；
- 发行：四平台交叉构建、双架构 Linux 归档/镜像、许可证、SBOM、校验和和升级回滚。

# Sprint 260728 需求：版本、发布与统计控制台

## 目标

在已有配置域、发布骨架和状态采集基础上完整交付 FR-3.4、FR-4.2、FR-4.4、FR-4.5：表单与 YAML 双向编辑、双视角变更预览、可审计发布/确认、版本历史与线性回滚，以及 route/cluster 流量统计。

## 开工审计

- 已有版本快照、列表/源码/compiled/diff/rollback API，但 diff 只有源文件视角；发布运行记录不可查询，控制台没有版本或发布页面。
- `RollbackPublish` 当前通过便捷 `Publish` 立即确认，绕过真实 M-STATE 生效确认；新版本生效后旧 effective 未转为 superseded。
- 配置页只有完整 YAML 编辑器和只读对象索引，没有对象表单、创建/删除和表单↔源码往返。
- 状态采集已持有环形序列，但 API 声明 `route|cluster`、采集实际产出 `listener|upstream|global`，cluster 过滤为空；overview 只返回序列数量，没有请求量、成功率、延迟和连接口径。
- Route Builder 尚未生成逐 route 统计标识，控制台没有统计页面。

## 范围

- 修正发布确认、superseded、超时重查和回滚发布的不变量；暴露发布运行证据；
- 当前草稿相对 effective 的源文件 diff、编译资源 diff和诊断统一 preview；
- 版本历史、详情、两版本 diff、源码/compiled 检视和二次确认回滚；
- JSON Schema 驱动的协议对象表单，与完整 YAML 编辑器共享文件真源并可无损往返；
- Envoy `Route.stat_prefix`、受控 stats 采集、counter rate/reset、成功率、状态分布、近似 P50/P90/P99、连接与健康/熔断指标；
- route/cluster 统计控制台、时间窗口与 stale/采集起点语义；
- OpenAPI、Go/Web unit、真实 Envoy stats 和桌面/移动 Playwright 回归。

## 非范围

- 多账号角色、审计日志搜索、OIDC（P2）；
- 长期统计持久化、告警和内置 Prometheus（外部 Prometheus 承担长期留存）；
- 多节点聚合统计（P2）；
- Kubernetes discovery/Helm（S11）。

## 验收标准

1. 新版本只有 M-STATE 确认后才 effective，旧 effective 原子转 superseded；timeout 可重查，回滚走同一异步确认链路并记录 `rollbackOf`；
2. preview 同时返回校验诊断、规范化源码 diff 和按 Envoy 资源键归组的 added/removed/changed；发布页展示从预览到确认的完整证据；
3. 六种协议对象可在表单创建/修改/删除，切换 YAML 后再切回不丢字段；冲突使用 resourceVersion 明示；
4. 版本页支持分页历史、详情、任意两版 diff、源码/compiled 查看和二次确认一键回滚；历史保持线性；
5. route 与 cluster 真实流量生成独立序列；统计 API 对请求速率、成功率、状态段、近似延迟、连接数和 reset 断点有单测与真实证据；
6. 桌面/移动统计与发布/版本主路径 Playwright 通过，Go/Web/OpenAPI/Envoy 全门禁 clean diff。

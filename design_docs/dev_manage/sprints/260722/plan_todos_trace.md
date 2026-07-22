# Sprint 260722 任务拆分与进展追踪

| ID | 任务 | 状态 | 验收 |
|---|---|---|---|
| T1 | admin client 与只读白名单 | 已完成 | client/只读端点/Prometheus 测试 |
| T2 | 状态快照与版本确认快速通道 | 已完成 | state 单测 |
| T3 | 时间序列环形缓冲与 S4 收口 | 已完成 | 环形淘汰、基数上限、查询过滤测试 |
| T4 | S4 完整性审计与状态采集补齐 | 已完成 | 周期调度、ready/stats/certs、worker/singleflight、取消、自动确认、退避及跨阶段故障路径均已测 |

## 完成度复核（2026-07-22）

结论：**Sprint 260722（S4）已完成**。T1/T2 的“基础完成”是审计前的旧措辞；T4 已补齐其缺口，现统一改为“已完成”。

| 验收项 | 当前证据 | 结论 |
|---|---|---|
| UDS/TCP 只读 admin client、写端点拒绝、Prometheus 流式透传 | `TestHTTPClientRejectsWritePaths`、`TestHTTPClientReadOnlyEndpointsAndPrometheus`、client error/decode 测试 | 通过 |
| config_dump 版本确认与超时事件 | `TestServiceConfirmsConfigDumpVersion`、`TestServiceTimesOutVersionConfirmation`、EDS 排除测试 | 通过 |
| 状态归一化、不可达时保留快照并标记 Stale | listener/cluster/ready/stats/certs 解析测试与 `TestServiceRetainsStaleSnapshot` | 通过 |
| 有界时间序列 | `TestSeriesStoreRingAndCardinality` | 通过 |
| 调度并发与恢复 | 串行、singleflight 等待者取消、指数退避/封顶/复位测试 | 通过 |
| 发布流自动确认 | `internal/conf/publish_test.go:TestPublisherAutoConfirmsFromStateEvent` | 通过 |
| 工程/真实 Envoy 门禁 | 2026-07-22 复跑 build/test/vet 通过；commit `8e4a53a` 已记录 static/ADS e2e 与 18 组合 validate matrix 通过 | 通过 |

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-20 | S3 收口，同时提前实现 S4 状态采集骨架；完整 API/UI 联调留 S5/S6 |
| 2026-07-20 | 质量审计：发现 S4 基础骨架存在关键覆盖和功能缺口；补充 admin client、Prometheus、SeriesStore、确认超时、listener/cluster endpoint 解析测试，并修复 endpoint 健康/权重/失败标记归一化。周期调度、ready/stats/certs、串行 worker/singleflight、Publisher 自动确认联动转入 T4。 |
| 2026-07-20 | T4 持续推进：实现分级周期采集、ready/stats(histogram)/certs/routes 归一化、EDS 排除于版本确认、Publisher 自动确认事件联动；补充 S1~S3 原生 IR、回滚保护、SnapshotJSON、ValidateIR、Store active-run 查询测试。全量 test/race/lint 通过，总覆盖率约 81.4%。 |
| 2026-07-20 | T4 质量门禁收口：加入 admin 请求串行化、按端点 singleflight、指数退避和启动期 ready 探测；`make e2e` 与 `make e2e-xds` 均通过。新增跨阶段质量矩阵，仍保留 S3 RollbackPublish/watch polling/migration 故障注入和 Envoy 多版本矩阵作为后续增强。 |
| 2026-07-21 | T4 完成：补齐 singleflight 等待者取消、退避递增/封顶/复位、watch polling 取消、`RollbackPublish` 全链路和 migration failure 测试；build/test/race/vet/lint 全绿。M-API/鉴权/UI 按本 Sprint 非范围移交 S5，不再阻塞 S4 关闭。 |
| 2026-07-22 | Docker 验收补证：`make e2e` 与 `make e2e-xds` 本轮实跑全绿；`make validate-matrix` 在 Envoy 1.37.5/1.38.3/1.39.0 × 6 份 static golden 的 18 个组合全部通过。 |
| 2026-07-22 | 独立复核 S4：逐项对照验收标准与 state/conf 直接测试，复跑 build/test/vet 全绿；修正 T1/T2 旧状态措辞并补齐独立 task 文档，确认 S4 可维持“已完成”状态。 |

# Sprint 260721 任务拆分与进展追踪

| ID | 任务 | 状态 | 验收 |
|---|---|---|---|
| T1 | M-STORE SQLite migration 与基础 CRUD | 已完成 | store 单测、build/test/lint |
| T2 | M-CONF 草稿加载与 draftHash | 已完成 | conf 单测 |
| T3 | 版本快照与最小发布编排、冲刺收口 | 已完成 | 快照/发布单测、全量检查 |
| T4 | 发布运行状态机基础（publish_runs、baseHash、CONFIRMING/EFFECTIVE） | 已完成 | conf/store 单测、全量检查 |
| T5 | 原生 static → IR 解析、diff/回滚、fsnotify、Origin CRUD | 已完成 | conf 单测、全量检查 |
| T6 | S4 前置：M-STATE admin client、版本确认与状态快照骨架 | 已完成 | state 单测、全量检查 |

## 完成度复核（2026-07-22）

结论：**Sprint 260721（S3）已完成**。本结论按当前代码与测试重新核验，不只沿用历史状态。

| 验收项 | 当前证据 | 结论 |
|---|---|---|
| SQLite 初始化/迁移幂等、settings、版本序号、发布唯一约束 | `internal/store/store_test.go` 的 migration/settings/version/active publish 测试；migration failure 故障注入 | 通过 |
| protocol/native 草稿互斥、严格解析与确定性 hash | `internal/conf/draft_test.go`、`advanced_test.go:TestParseNativeStrict` | 通过 |
| 原子版本快照与可解析元数据 | `internal/conf/draft_test.go:TestSnapshot` | 通过 |
| 发布状态机、baseHash 乐观并发、生效确认 | `internal/conf/publish_test.go` 的 lifecycle/conflict/confirm/auto-confirm 测试 | 通过 |
| diff、回滚、fsnotify/polling、Origin CRUD | `internal/conf/advanced_test.go` 与 `TestPublisherRollbackPublishRestoresAndRecordsVersion` | 通过 |
| 工程门禁 | 2026-07-22 复跑 `make build`、`go test ./...`、`go vet ./...` 通过；`go test` 需允许本地临时 TCP 监听。历史 commit `f0ad077` 已记录 race/lint 收口 | 通过 |

文档一致性修正：原 `requirements.md`/`technical_design.md` 是 Sprint 开始时的第一阶段基线，实际范围随后通过 T4~T6 扩展到路线图中的完整 S3，并由 task/trace 留痕；REST API、鉴权和 UI 始终不属于本 Sprint，移交 S5/S6。

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-20 | S2 完成，按路线图进入 S3 |
| 2026-07-20 | 创建 S3 第一阶段：先实现 M-STORE/M-CONF 基础闭环，发布流与 API 后续拆分 |
| 2026-07-20 | T1 完成：`internal/store` SQLite migration、settings、版本序号/元数据、活跃发布唯一索引；引入 modernc.org/sqlite |
| 2026-07-20 | T2 完成：`internal/conf.LoadDraft`、抽象/native 互斥检测、确定性 `DraftHash` |
| 2026-07-20 | T3 完成：`conf.Snapshot` 临时目录 + rename 原子快照、`meta.json`；`make build test lint` 全绿 |
| 2026-07-20 | 补充最小 `conf.Publisher.Publish`：草稿加载→Compile→版本快照→Deliverer.Apply→effective/failed 留痕；native/diff/rollback/fsnotify 仍后续拆分 |
| 2026-07-20 | T4 完成：publish_runs 增加 version_seq/trigger_by 与读写 API；Publisher 增加 PublishWithBase 乐观并发、VALIDATING→VALIDATED→PUBLISHING→CONFIRMING→EFFECTIVE 状态推进、Confirm 生效确认；保留 Publish 兼容包装；新增冲突/生命周期单测 |
| 2026-07-20 | T5 完成：native.yaml 严格 protojson→IR/F6 校验、快照文本 diff、强制确认回滚、fsnotify（含 polling fallback）、按 Origin 文档替换/删除 CRUD；发布写入 parent_seq 与 diff_json |
| 2026-07-20 | T6 完成：新增 internal/state 只读 admin client（UDS/TCP、路径白名单、Prometheus 流式透传）、config_dump 版本确认快速通道、状态快照、listener/cluster 归属反解、有限内存 SeriesStore |
| 2026-07-22 | 独立复核 S3：逐项对照 store/conf/state 实现与直接测试，复跑 build/test/vet 全绿；补齐 T5/T6 task 文档与验收证据，确认 S3 可维持“已完成”状态 |

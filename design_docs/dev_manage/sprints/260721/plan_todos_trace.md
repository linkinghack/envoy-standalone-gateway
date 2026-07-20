# Sprint 260721 任务拆分与进展追踪

| ID | 任务 | 状态 | 验收 |
|---|---|---|---|
| T1 | M-STORE SQLite migration 与基础 CRUD | 已完成 | store 单测、build/test/lint |
| T2 | M-CONF 草稿加载与 draftHash | 已完成 | conf 单测 |
| T3 | 版本快照与最小发布编排、冲刺收口 | 已完成 | 快照/发布单测、全量检查 |

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-20 | S2 完成，按路线图进入 S3 |
| 2026-07-20 | 创建 S3 第一阶段：先实现 M-STORE/M-CONF 基础闭环，发布流与 API 后续拆分 |
| 2026-07-20 | T1 完成：`internal/store` SQLite migration、settings、版本序号/元数据、活跃发布唯一索引；引入 modernc.org/sqlite |
| 2026-07-20 | T2 完成：`internal/conf.LoadDraft`、抽象/native 互斥检测、确定性 `DraftHash` |
| 2026-07-20 | T3 完成：`conf.Snapshot` 临时目录 + rename 原子快照、`meta.json`；`make build test lint` 全绿 |
| 2026-07-20 | 补充最小 `conf.Publisher.Publish`：草稿加载→Compile→版本快照→Deliverer.Apply→effective/failed 留痕；native/diff/rollback/fsnotify 仍后续拆分 |

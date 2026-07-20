# Sprint 260721 技术设计

## 1. 模块边界

`internal/store` 只负责 SQLite 连接、迁移和运行态索引，不存配置正文。
`internal/conf` 只负责文件真源、草稿派生状态与版本快照，不直接暴露 SQL。

## 2. SQLite

- 驱动：`modernc.org/sqlite`，避免 CGO；
- 启动参数：`foreign_keys=ON`、`journal_mode=WAL`、`busy_timeout=5000`；
- migration 通过 `schema_migrations(version INTEGER PRIMARY KEY)` 管理；
- 本冲刺建立 `settings`、`versions`、`publish_runs` 及后续账号/审计/证书预留表。

## 3. 草稿与哈希

- 抽象模式扫描 `<data-dir>/config.d/*.yaml|*.yml`，排序后交给 `protocol.LoadDir`；
- `<data-dir>/native.yaml` 与 `config.d/` 非空同时存在时报错；
- `DraftHash` 输入为相对路径长度、路径、文件长度、文件内容，按路径排序后经 SHA-256 编码，避免拼接歧义；
- 加载错误原样返回，调用方可决定是否阻止发布。

## 4. 版本快照

`Snapshot` 在 `<data-dir>/tmp` 下创建临时目录，复制当前真源到 `config/`，写 `meta.json`，
随后 rename 到 `versions/%06d`。目标已存在或复制失败时不覆盖已有版本。

## 5. 冻结接口

本冲刺新增接口以测试为准，后续发布流通过这些接口组合：

- `store.Open`, `Store.GetSetting`, `Store.SetSetting`, `Store.NextVersionSeq`, `Store.InsertVersion`
- `conf.LoadDraft`, `conf.DraftHash`, `conf.Snapshot`

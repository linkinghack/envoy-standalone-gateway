# T5：配置域完整能力收口

## 目标

补齐路线图 S3 在第一阶段之外的完整配置域能力：原生 static → IR、双视角 diff、回滚、外部文件修改检测与按 Origin 的文档 CRUD。

## 执行步骤

1. 严格解析 `native.yaml` 并转换为统一 IR，执行 F6 终检；
2. 实现版本快照文本 diff 与带 `force` 保护的回滚源恢复；
3. 实现 fsnotify watcher，并提供轮询 fallback 与可取消生命周期；
4. 实现按 `Origin{File, DocIndex}` 替换/删除 YAML 文档；
5. 在发布版本元数据中记录 `parent_seq` 与 `diff_json`；
6. 用直接测试覆盖成功、冲突、取消和错误路径。

## 验收记录

- 2026-07-20：实现落地于 `internal/conf/native.go`、`diff.go`、`rollback.go`、`watch.go`、`crud.go`，commit `6e32f3c`。
- 2026-07-21：补齐完整 `RollbackPublish`、watch polling 取消和 migration failure 回归，commit `f0ad077`。
- 2026-07-22：独立复核 `internal/conf/advanced_test.go` 与 `publish_test.go` 的直接测试，`go test ./...` 通过。任务完成。

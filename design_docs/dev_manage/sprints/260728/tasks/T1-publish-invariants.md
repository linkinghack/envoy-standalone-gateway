# T1：发布与回滚状态机不变量

## 目标

修正 effective/superseded、timeout recheck 和 rollback 绕过数据面确认的问题，使所有发布只有一条事实链路。

## 步骤

1. Store 增加原子确认/旧版本 supersede；2. Publisher 超时重查；3. RollbackPublish 携带元数据走异步链路；4. 恢复/并发边界；5. unit/integration。

## 进展

- 已完成：Store 以单事务切换 publish run/version，并只在确认成功时 supersede 旧 effective 版本。
- 已完成：TIMEOUT 可针对同一不可变 IR version 重新进入 CONFIRMING，不重复下发，也不伪造确认。
- 已完成：rollback 恢复使用 `ReplaceDraft` 原子替换，兼容 abstract/native；新版本记录真实 parent 与 rollbackOf，并走异步数据面确认。
- 已完成：覆盖确认、超时复检、回滚确认、模式切换及事务关联校验。

## 验证

- `go test ./internal/store ./internal/conf -count=1`
- `CC=clang CGO_ENABLED=1 go test -race ./internal/store ./internal/conf`
- `go test ./...`
- `go vet ./...`
- `golangci-lint run ./...`

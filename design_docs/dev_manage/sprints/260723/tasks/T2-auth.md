# T2：本地账号、密码与服务端会话

## 目标

实现一次性 bootstrap、Argon2id 密码、本地 admin、可吊销 session 与登录限流。

## 步骤

1. 增量 migration 与 Store auth CRUD；
2. PHC Argon2id hash/verify；
3. bootstrap/login/logout/session/password service；
4. token hash、滑动/绝对过期与密码变更吊销；
5. IP/账号限流、并发与错误路径测试。

## 进展

- 2026-07-22：完成。新增 `internal/store/auth.go` 与 auth schema 增量 migration；新增 `internal/auth` 的 Argon2id PHC、一次性 bootstrap、服务端 session、token SHA-256、半 idle TTL 轮换、绝对过期、修改密码吊销其他会话、每 IP/账号登录限流。直接 test/race、全量 vet、golangci-lint v2.12.2 均通过。

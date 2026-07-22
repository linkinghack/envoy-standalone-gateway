# Sprint 260723 任务拆分与进展追踪

| ID | 任务 | 状态 | 验收 |
|---|---|---|---|
| T1 | OpenAPI 契约、生成边界与一致性门禁 | 已完成 | spec/operation/生成 clean-diff |
| T2 | Store auth migration 与 auth service | 已完成 | bootstrap/password/session/limit 单测 |
| T3 | API server 通用中间件、health/ready、SPA fallback | 已完成 | auth/CSRF/error/404/cache 测试 |
| T4 | 配置域 REST API | 已完成 | draft/object/validate/publish/version/rollback 集成测试 |
| T5 | state/stats/system/Prometheus 与证书库 API | 已完成 | 归一化/流式/私钥不出测试 |
| T6 | M-CORE 装配、API e2e 与 S5 收口 | 已完成 | build/test/race/vet/lint + HTTP e2e |

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-22 | S3/S4 独立复核完成；补充 M1 engineering plan 并冻结 AD1/AD2/AD5/AD7/AD8，创建 S5 全量任务结构，T1 开始 |
| 2026-07-22 | T1 契约真源已落地 42 个 operation，operation/CSRF contract test 通过；oapi-codegen 因当前网络禁用尚未生成，T1 保持进行中 |
| 2026-07-22 | T2 完成：Store auth 增量 migration/CRUD，Argon2id PHC、token hash、bootstrap/session rotation/password revoke/IP+账号限流；直接 test/race/vet/lint（golangci-lint v2.12.2）通过，T3 开始 |
| 2026-07-22 | T3 完成：42-operation 显式注册、session/CSRF/recovery/request-id/security headers、health/ready、SPA fallback/API JSON 404 与缓存边界均有 `httptest`；未装配领域 operation 明确返回 501 |
| 2026-07-22 | T4 完成：draft 整体替换、对象 CRUD/resourceVersion、schema/validate/compiled/sourceMap/diff、发布/状态/版本/回滚 HTTP 闭环通过；重复发布以 `NO_CHANGES` 拒绝 |
| 2026-07-22 | T5 完成：归一化状态/有限序列/system info、鉴权后 Envoy Prometheus 脱敏代理；证书配对、原子落盘、私钥 0600/只进不出、草稿引用删除保护及 `ref` 编译解析均通过；T3-T5 定向 test/race/vet/golangci-lint 通过，T6 开始 |
| 2026-07-22 | T1 补证完成：网络恢复后以固定 oapi-codegen v2.6.0 生成 models/std-http server interface；runtime 固定 v1.3.0；连续两次生成文件 SHA-256 均为 `33705e5d6a9204d880b2ae470f033d280776bf388275f17161d1c215ac038a8a` |
| 2026-07-22 | T6 完成：`internal/core.App` 成为唯一生产组合根，装配 Store/Auth/xDS/State/Publisher/CertStore/API；`esgw serve` 使用 `dataDir/config.d` 单一真源并同时管理 xDS/API 生命周期，支持 API/state 配置、ready 和反向优雅关闭 |
| 2026-07-22 | S5 收口：组合测试覆盖未鉴权拒绝及 bootstrap→draft replace→validate→publish→status；`make build`、`go test ./...`、`go test -race ./...`、`go vet ./...`、golangci-lint v2.12.2 全绿（0 issues） |

## 验收核验

| ID | 状态 | 证据 |
|---|---|---|
| A1 OpenAPI/实现一致 | 通过 | 42 operation 一一对应 contract test；oapi-codegen v2.6.0 生成 interface 编译通过且重复生成哈希一致；组合根断言无 unimplemented operation |
| A2 auth/session/CSRF | 通过 | `internal/auth/*_test.go`、`internal/api/server_test.go` |
| A3 配置发布 HTTP 闭环 | 通过 | `internal/api/config_handlers_test.go`、`internal/core/app_test.go` |
| A4 state/stats/system | 通过 | `internal/api/state_handlers_test.go`、既有 `internal/state/*_test.go` |
| A5 证书私钥安全 | 通过 | `internal/certstore/certstore_test.go`、`internal/api/certificate_handlers_test.go`、`internal/compile/managed_certificate_test.go` |
| A6 SPA/API 路由边界 | 通过 | `internal/api/server_test.go` |
| A7 工程质量门禁 | 通过 | 2026-07-22：build、全量 test/race/vet、golangci-lint v2.12.2 均通过，0 issues |

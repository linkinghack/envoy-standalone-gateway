# T4：配置域 REST API

## 目标

把文件真源、编译校验、发布状态机、版本/diff/回滚以 OpenAPI 契约暴露。

## 步骤

1. draft 整体读写与 resourceVersion 并发保护；
2. 对象 list/get/upsert/delete 与 schema；
3. validate/compiled/diff；
4. publish/status/versions/rollback；
5. fake deliver/state 与 HTTP 组合测试。

## 进展

- 已完成。配置 API 已覆盖 draft/object/schema/validate/compiled/diff/publish/status/version/rollback；所有写操作复用文件真源与 durable publish 状态机。
- `internal/api/config_handlers_test.go` 覆盖 resourceVersion 冲突、无变化发布拒绝、发布确认、版本内容/diff 与受保护回滚。

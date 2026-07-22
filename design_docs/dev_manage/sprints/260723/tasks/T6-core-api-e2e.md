# T6：M-CORE API 装配与 S5 收口

## 目标

建立唯一生产组合根，跑通从 HTTP 管理请求到配置发布/状态确认的 S5 闭环。

## 步骤

1. 扩展 `esgw.yaml` 的 API/state 配置；
2. 组装 Store/Auth/Deliver/State/Conf/API 生命周期；
3. 接入 `esgw serve`，实现优雅关停与 ready；
4. HTTP e2e 覆盖 bootstrap→CRUD→validate→publish→status；
5. 逐项核验 A1~A7，更新 roadmap/index 并收口。

## 进展

- 已完成。新增 `internal/core.App` 唯一生产组合根，按 Store→Deliver/State/Conf→API 顺序构造并反向关闭；启动前完成初始 IR 编译/Apply，随后同时监听 xDS 与管理 API。
- `esgw.yaml` 已增加 `api.listen/topology` 与 state polling intervals；默认 API 为 `127.0.0.1:8080`，显式非 loopback 地址记录警告；`ESGW_INITIAL_ADMIN_PASSWORD` 可在监听前完成一次性 admin 初始化。
- `internal/core/app_test.go` 走通未认证拒绝、bootstrap、draft 替换、validate、publish 与 status；既有真实 ADS smoke 已迁移到 `dataDir/config.d` 单一真源和新组合根。
- build、全量 test/race/vet、golangci-lint v2.12.2 全绿，S5 收口。

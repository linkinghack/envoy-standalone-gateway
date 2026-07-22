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

- 待开始。

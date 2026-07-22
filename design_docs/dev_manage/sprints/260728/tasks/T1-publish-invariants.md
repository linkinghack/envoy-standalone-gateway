# T1：发布与回滚状态机不变量

## 目标

修正 effective/superseded、timeout recheck 和 rollback 绕过数据面确认的问题，使所有发布只有一条事实链路。

## 步骤

1. Store 增加原子确认/旧版本 supersede；2. Publisher 超时重查；3. RollbackPublish 携带元数据走异步链路；4. 恢复/并发边界；5. unit/integration。

## 进展

- 待开始。

# T4：四组合装配与恢复

## 目标

使 xDS/static × 托管/仅下发均通过唯一 `internal/core.App` 装配，并保持清晰的权限与受理语义。

## 步骤

1. 模式无关 Deliverer 字段与条件监听；
2. state/proc/deliver 启动顺序；
3. 初始 IR 按当前模式编译；
4. 仅下发零进程动作断言；
5. 管理面重启接管和关闭不杀 Envoy 测试。

## 进展

- 待开始。

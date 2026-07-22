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

- 2026-07-22：完成。`internal/core.App` 持有模式无关 Deliverer，仅 xDS 模式打开 ADS；static 模式初始渲染后仅服务管理 API。
- `proc.enabled=false` 不执行 Discover/Runner/record；启用时 xDS 原子生成接入 bootstrap，static 将 supervisor 作为 narrow Restarter 注入。M-STATE 实现唯一 admin consumer 的 LIVE/epoch Probe。
- 四组合构造测试覆盖 deliverer/supervisor/artifact；static 仅下发确认窗口为 10min。全仓 build/test/race/vet/lint 通过。

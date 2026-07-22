# T3：T1/T2 × xDS/static 真实矩阵

## 目标

用真实 Envoy 验证四种 M1 标准组合的启动、流量、生效边界和管理面故障隔离。

## 步骤

1. 复用 fixtures 与统一断言库；
2. T1 xDS/static 托管场景；
3. T2 xDS/static 外部进程场景；
4. 管理面停止继续流量、错误配置 last-good；
5. Make/CI 入口、trace 与独立提交。

## 进展

- 待开始。

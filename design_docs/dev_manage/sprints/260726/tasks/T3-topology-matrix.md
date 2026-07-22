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

- 已完成：新增无 bind-mount、固定 Envoy v1.39.0 的四组合真实矩阵，统一使用两个真实后端与连续代理探针。
- T1 xDS/static 在管理进程退出后由 loop 拉起新管理面，并断言 Envoy PID 被原位接管、100 次连续流量零失败。
- T2 xDS/static 使用外部 Envoy，管理进程退出/重连期间同样保持零失败；xDS 复用 sidecar netns，static 复用持久 volume。
- file-only static 额外断言非法源不改 last-good artifact；合法文件更新后外部 Envoy 重启前仍走旧后端，显式重启后才切换新后端。
- `make e2e-topology-matrix` 通过；S7 `make e2e-static-managed` 继续覆盖 static 发布时的 hot restart epoch 与回滚细节。

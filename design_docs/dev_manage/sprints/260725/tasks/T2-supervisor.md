# T2：进程记录、接管与退避

## 目标

实现不会因管理面重启误杀数据面的 M-PROC supervisor，并对运行期崩溃提供有界恢复。

## 步骤

1. proc.json 原子记录与 PID 存活/身份检查；
2. Runner/Process 生产与 fake 实现；
3. epoch 0 spawn、record 一致接管和 keep/restart 分支；
4. 退出分类、指数退避、稳定窗口和 degraded；
5. fault tests 与事件证据。

## 进展

- 2026-07-22：完成。`proc.json` 以 `0600` 原子持久化，PID 必须同时通过 `/proc/<pid>/exe` 身份校验才可接管/显式 signal；默认 keep 在探测不一致时 degraded。
- OS runner 使用独立进程组、完整 hot restart 参数、异步 wait/reap 和 4KiB stderr tail；epoch 0 崩溃执行指数退避/稳定重置/10 分钟阈值，epoch>0 异常按官方边界安全降级。
- `go test`、`go vet`、golangci-lint（proc package）通过。

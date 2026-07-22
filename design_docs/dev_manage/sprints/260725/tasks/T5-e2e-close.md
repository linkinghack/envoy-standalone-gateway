# T5：真实 Envoy e2e 与收口

## 目标

用真实 Envoy 证明 static epoch 热重启和流量连续性，并完成 DL 决议、全量质量门禁与路线图回写。

## 步骤

1. static 托管 e2e 拓扑和可重复 fixtures；
2. epoch 0→1、流量不中断、坏配置 last-good；
3. 管理面重启接管与崩溃退避实测；
4. build/test/race/vet/lint 和现有回归；
5. trace/tasks/roadmap/index 收口。

## 进展

- 已完成（2026-07-22）。
- 新增 `e2e/static-managed` 与 `make e2e-static-managed`，无 bind mount 构建测试镜像，使用官方 Envoy v1.39.0 和真实 HTTP backend。
- 验收证据：epoch 0→1；发布窗口 120 次单次请求探测零失败；管理面 SIGTERM 后新进程接管相同 epoch 1/PID；非法 listener port 返回 `VALIDATION_FAILED`，live artifact 哈希、epoch、PID 和流量均不变。
- 实测修复：Docker overlayfs 对镜像层目录 rename 返回 `EXDEV`，草稿事务增加完整复制后删除回退；M-STATE 改读真实 `command_line_options.restart_epoch`；M-PROC 通过独立 launcher 保持 Envoy 父进程跨管理面重启存活。
- 本地 `make build`、`go test ./...`、`go test -race ./...`、`go vet ./...`、`golangci-lint run ./...` 全绿；远端 CI 待推送后补证。

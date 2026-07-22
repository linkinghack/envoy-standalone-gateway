# T3：static 原子下发与 hot restart

## 目标

保证 static Apply 在磁盘和进程两个层面都不暴露半生效状态，热重启失败不影响旧 epoch。

## 步骤

1. 同目录 temp/fsync/rename/fsync-dir writer；
2. last-good 备份和失败恢复；
3. epoch 单调分配与 spawn 参数；
4. M-STATE LIVE/epoch 探测；
5. 超时/退出只回杀新进程、stderr 尾部与事件测试。

## 进展

- 2026-07-22：完成。Writer 在 output 同目录执行 `0600 temp → fsync → rename → fsync(dir)`，故障注入证明 rename 前失败不改变 last-good。
- static Deliverer 仅下发时同版本仍重写自愈；托管时写后执行 N→N+1，失败只 kill child 并恢复 last-good。`nextEpoch` 在 spawn 前持久化，失败序号不复用。
- 自定义运行时 admin UDS 只在 Render 深拷贝上生效，不改变 IR/版本哈希；proc/static test、vet、lint 通过。

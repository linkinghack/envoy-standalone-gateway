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

- 待开始。

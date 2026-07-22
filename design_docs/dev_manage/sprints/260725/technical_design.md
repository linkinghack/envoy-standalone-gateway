# Sprint 260725 技术设计

## 1. 决策冻结

| 决策 | 结论 |
|---|---|
| DL1 | 生产默认保留安全值：`liveTimeout=30s`、`drainTime=600s`、`parentShutdownTime=900s`；测试显式缩短。强制 `liveTimeout < parentShutdownTime` 且后者不小于 120s。 |
| DL4 | `adoptPolicy=keep` 为默认；记录存在但身份/epoch 无法确认时进入 degraded 并告警，绝不猜测性杀进程。`restart` 仅在显式配置且已确认记录 PID 身份后使用；不增加“新 base-id 并行”第三档。 |
| DL6 | epoch 只增不减，失败 epoch 写入记录后也不复用；失败回杀后保留 last-good 文件副本并恢复当前输出，下一次用 N+2 重试。admin 短暂不可达由 M-STATE stale 表达。 |
| 托管默认 | `proc.enabled` 采用显式布尔，缺省为 `false` 以兼容既有“仅下发”部署并遵守最小权限；T1/all-in-one 交付配置在 S8 显式置 `true`。 |

## 2. 包与接口

- `internal/proc/discover.go`：发现顺序 `ESGW_ENVOY_PATH` → `proc.envoyPath` → PATH，执行 `--version` 并解析支持区间；
- `internal/proc/record.go`：`run/proc.json` 原子记录 `{pid,baseID,epoch,configPath,startedAt,envoyVersion,state}`；Linux `kill(pid,0)` 只作存活线索；
- `internal/proc/supervisor.go`：Runner/Process 抽象、spawn/wait/signal、接管、退避和事件；生产实现基于 `os/exec`，测试使用 fake；
- `internal/proc/hotrestart.go`：在锁内分配新 epoch、spawn、轮询 `Probe`，失败回杀新进程，永不 signal 旧进程；
- `internal/deliver/static/writer.go`：Render 后在 output 同目录创建临时文件，`fsync(file) → rename → fsync(dir)`；
- `internal/deliver/static/server.go`：实现统一 Deliverer；仅下发到 rename 即受理，托管则继续 hot restart。

M-PROC 不直接访问 Envoy admin。`Probe` 由 M-STATE 提供，只暴露 `Ready + HotRestartEpoch` 的只读快照。

## 3. 生命周期与恢复

组合根先建立 Store/IR/State，再构造模式对应 Deliverer。xDS 只在 xDS 模式监听；static 不打开无意义的 ADS 端口。`proc.enabled=true` 时，初次配置落盘后 supervisor 依记录选择接管或 epoch 0 启动；`false` 时不构造任何可执行进程动作。

管理面关闭只停止自己的 HTTP/ADS/采集 goroutine，不向 Envoy 发送退出信号。托管进程可跨 esgw 重启继续服务；新管理面通过 record + M-STATE epoch 重新接管。

## 4. static Apply 事务

1. `static.Render(IR)` 在内存完成；
2. 保存当前 output 为 last-good（若存在）；
3. 原子替换 output；
4. 仅下发：返回 `awaiting_confirm`；
5. 托管：分配 N+1 并 spawn；M-STATE 在 `liveTimeout` 内观测 LIVE/epoch；
6. 成功记录 N+1 并返回；失败 SIGKILL 新 epoch、恢复 last-good、记录失败 epoch，返回带 stderr 尾部的 `stage=hot_restart`。

同版本：托管组合幂等跳过；仅下发组合仍重写以修复外部删改。

## 5. 测试

表驱动测试覆盖严格配置、发现/版本、原子写故障、接管分支、epoch 成功/超时/退出、退避/degraded 和四组合装配。真实 e2e 使用官方 Envoy 镜像或本机二进制验证 epoch 0→1、流量连续和坏配置保护；运行条件不足时记录缺口，不用 mock 冒充真实证据。

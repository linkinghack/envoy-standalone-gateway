# 260720-1 M-CORE 装配骨架与 esgw.yaml schema（M-CORE Assembly）

- **性质**: 沉淀性设计（dev_design），随 M-CORE 演进更新；由 Sprint 260720 / T1 首次落地
- **日期**: 2026-07-20
- **上游文档**: [`260717-1-deliver-layer-design.md`](../../system_design/260717-1-deliver-layer-design.md) §1.3/§2.7/§7、[`260715-2-overall-architecture.md`](../../system_design/260715-2-overall-architecture.md) §3、Sprint 260720 [`technical_design.md`](../sprints/260720/technical_design.md) SD2/SD4/SD5

---

## 1. M-CORE 职责

M-CORE 是进程级装配层，自身不含业务语义，职责仅两条：

1. **装配各模块**：按架构 §3 的依赖单向性（`api → conf → compile → deliver`，`state/disco` 旁路）构造各模块实例并接线；
2. **驱动启动序与优雅退出**：保证「数据面配置先于 xDS 端口就绪」，进程收到终止信号时按序关停。

对应代码：`internal/core`（S2/T3 落地 `RunServe`），配置加载为 `internal/config`（本冲刺 T1 交付，见 §3）。

## 2. S2 形态的启动序与关停

### 2.1 启动序（`esgw serve -c <esgw.yaml> -f <config-dir>`）

S2 无 M-CONF 发布流与版本仓库（S3 落地），配置真源过渡为 `-f <config-dir>` 配置目录。启动序对齐下发层 §2.2/§7 的启动链路，把「M-CONF 恢复 effective 版本 → 重编译」替换为「直接加载配置目录 → 编译」：

```
1. 加载 esgw.yaml（internal/config.LoadFile：strict decode + defaults + 校验，§3）
2. 加载配置目录（protocol.LoadDir）→ 编译（compile.Compile，mode=xds）产出 IR
3. 下发装配：xds.FromIR → Consistent() 自检 → SnapshotCache.SetSnapshot(IR.Version)
4. 上述全部成功后才开始 accept xDS gRPC 连接（先装配 snapshot 再 listen 语义由
   「SetSnapshot 先于 serve 主循环返回就绪」保证；Envoy 提前连接时请求按 SotW 语义挂起，
   snapshot 就位后即得响应，见下发层 §2.6）
```

关键不变量（下发层 §2.2）：**xDS 端口对外可服务之前，首版 snapshot 必须已装配**，Envoy 任何时刻连上都能拿到完整配置；`deliver.mode: static` 在步骤 1 之后即报「未实现（S7）」错误退出（SD2，static 运行时骨架归 S7，`esgw compile` 的纯导出路径不受影响）。

### 2.2 关停

- 收到 `SIGTERM` / `SIGINT` → 取消 serve 上下文 → xDS gRPC server `GracefulStop()`（等待在途流排空）→ 进程退出。
- esgw 停机不影响数据面：Envoy 按最后有效配置继续服务（xDS 原生语义，FR-5.6 / 下发层 §2.6）；esgw 重启后经确定性编译得到相同 `IR.Version`，Envoy 重连比对版本一致，零推送零扰动。
- S2 无 M-PROC，esgw 不管理 Envoy 进程生命周期，关停时不对 Envoy 发任何信号。

### 2.3 S2 骨架边界与演进方向

| 维度 | S2 现状 | 各模块就位后的演进 |
|---|---|---|
| 配置真源 | `-f <config-dir>` 一次性加载（过渡形态） | S3 起 M-CONF/M-STORE 接管：版本仓库 + 发布流驱动 Apply |
| 生效确认 | 仅 ACK/NACK 跟踪（下发层 §2.5） | S4 起 M-STATE config_dump 观测为权威确认通道（260717-4 §4.3） |
| 进程托管 | 无 M-PROC，Envoy 由外部（e2e 为 compose）管理 | M-PROC 落地后接入 hot restart / 接管 / 退避（下发层 §5） |
| 下发模式 | 仅 xds；`deliver.mode: static` 报未实现 | S7 static 运行时骨架（渲染落盘 + hot restart 协调） |
| 观测 | 结构化日志 | `esgw_*` metrics 随 M-CORE 完善补充（下发层 §7） |

启动序的演进：M-CONF 就位后，步骤 2 前插「恢复 current effective 版本」（下发层 §2.2）；M-STATE/M-PROC 就位后追加「M-STATE 就绪后 M-PROC 才接受热重启请求」（下发层 §5.1 硬边界）。上述插入点已按序预留，装配代码的模块构造顺序即启动序。

## 3. esgw.yaml schema

esgw.yaml 是 esgw **进程自身**的配置文件（区别于 `internal/protocol` 承载的网关配置协议对象）。约定：

- 单文档 YAML；strict decode（yaml.v3 Node → JSON + `DisallowUnknownFields`，与协议加载同栈，SD4），**未知字段一律报错**——后续冲刺的保留键在当前版本同样报错，防止拼写错误静默生效；
- `esgw serve` 要求显式配置文件：文件读不到即报错；**空文档合法，等价全默认值**；
- 键名 camelCase；时长为 Go duration 字符串（`15s`、`1m30s`）。

### 3.1 本冲刺（S2）生效的键

```yaml
dataDir: /var/lib/esgw        # 数据目录（托管 bootstrap、proc.json、tmp/ 等的根；默认 /var/lib/esgw）
deliver:
  mode: xds                   # xds | static（默认 xds；static 为合法枚举，serve 启动即报未实现（S7），SD2）
  xds:
    listen: 127.0.0.1:18000   # ADS gRPC 监听地址（默认 127.0.0.1:18000）
    nodeID: esgw-node         # 写入接入 bootstrap 的 node.id（默认 esgw-node，下发层 §2.3）
    nodeCluster: esgw         # node.cluster（默认 esgw，下发层 §2.3）
    adminAddress: unix:///var/run/esgw/envoy-admin.sock
                              # 接入 bootstrap 的 Envoy admin 地址（SD5）；
                              # 仅接受 unix:///<path> 或 <host>:<port> 两种形态
    ackTimeout: 15s           # ACK 观察窗口（默认 15s；S2 仅作保留字段，超时告警逻辑归 S4 随 M-STATE 做，SD7）
```

校验规则（`internal/config` 实现，A7 硬校验的管理面侧）：

1. `deliver.xds.listen` 必须可解析为 `host:port`，且 host 必须是 loopback：IP 字面量按 `netip.Addr.IsLoopback()` 判定（含 `127.0.0.0/8` 全段与 `::1`）；`localhost` 按 loopback 接受（取舍：本机解析语义稳定，拒绝它对同机部署不友好；其余主机名无法静态判定，一律拒绝）。`0.0.0.0`、`::`、空 host（`:18000`）及其他非 loopback 地址**一律拒绝**，错误信息指明「非 loopback 监听需配置 tls，tls 为 P2 预留」（下发层 §2.7：tls 本冲刺不存在，故一切非 loopback listen 均拒绝）。
2. `deliver.mode` 仅接受 `xds | static`。
3. `deliver.xds.adminAddress` 仅接受 `unix:///<绝对路径>` 或 `<host>:<port>`（端口 1~65535）。
4. `ackTimeout` 必须为正时长。

### 3.2 后续冲刺保留键（当前加载报未知字段错误）

以下键名已在对应设计文档中约定，当前版本**不接受**，列入保留清单以防占用冲突：

| 键 | 归属 | 落地冲刺 |
|---|---|---|
| `deliver.xds.tls`（certFile/keyFile/clientCAFile） | 跨主机 xDS mTLS（下发层 §2.7，NFR-5） | P2（FR-3.6 专项，DL3） |
| `deliver.static.*`（outputPath 等） | static 通道（下发层 §4） | S7 |
| `proc.*`（enabled/envoyPath/baseID/liveTimeout/drainTime/parentShutdownTime/restartBackoff/adoptPolicy） | M-PROC 进程托管（下发层 §1.3/§5） | M-PROC 冲刺 |
| `state.*`（admin 探测档位、confirm.timeout 等） | M-STATE（260717-4） | S4 |
| `api.*`（控制台 API 监听等） | M-API（260717-3） | S5 |
| `store.*`（sqlite 路径等） | M-STORE（260717-2） | S3 |
| `disco.*`（kubeconfig 等） | M-DISCO（260717-5） | k8s disco 冲刺 |

## 4. 与上游设计的关系

- 本文是下发层 §7「M-CORE 启动序」契约在 S2 骨架形态下的具体化；骨架完整形态（含 M-CONF/M-STATE/M-PROC 的完整启动序）随各模块冲刺在本文件持续补充。
- esgw.yaml 键草案的权威出处仍是下发层 §1.3；本文 §3 是与代码对齐的**生效真源**（schema 与 `internal/config` 严格一致，偏差即 bug）。

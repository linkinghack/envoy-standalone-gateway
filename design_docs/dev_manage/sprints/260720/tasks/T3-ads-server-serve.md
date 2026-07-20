# T3：Deliverer 接口 + ADS server + ACK/NACK + esgw serve

- **状态**: 未开始
- **依赖**: T1（config）、T2（FromIR）
- **验收锚点**: requirements A2、A5、A6

## 目标

交付 xDS 运行时通道与 M-CORE 骨架：`internal/deliver` 的 Deliverer 接口与状态/事件模型（下发层 §6）、`deliver/xds` 的 ADS gRPC server（SotW SnapshotCache + callbacks ACK/NACK 跟踪）、`internal/core` 装配骨架与 `esgw serve` 命令。

## 上游设计引用

- [下发层设计](../../../../system_design/260717-1-deliver-layer-design.md) §2.2（Snapshot 生命周期）、§2.3（node id 与单节点假设）、§2.4（原子换版四层语义）、§2.5（ACK/NACK）、§2.6（断连重连）、§6.1（Apply 语义）、§6.2（状态与事件模型）、§7（M-CORE 启动序）
- 冲刺 [technical_design.md](../technical_design.md) SD2（serve 骨架）、SD3（UpdateEndpoints 防御）、SD7（ACK 判定口径）、§3 冻结签名
- 现有代码：`cmd/esgw/main.go` + `compile.go`（子命令分发模式）、`internal/compile`（Compile 入口）、T1 的 `internal/config`、T2 的 `xds.FromIR`

## 执行步骤

1. `internal/deliver/deliver.go`：Deliverer 接口、Status/Phase/Event（冻结签名见 technical_design §3）。Phase 枚举全量定义（`confirmed` 本冲刺无人驱动到该态，注释说明 S4 接入）；Event.Kind 先实现 `Applied`/`Nacked`，`HotRestartFailed`/`SupervisorDegraded` 枚举值预留并注释 S7 使用。`ErrEndpointsUnsupported` 哨兵错误（SD3）。
2. `internal/deliver/xds/server.go`：
   - `Server` 持有 `cache.SnapshotCache`（ads=true，node 哈希取 node.Id）、grpc.Server、互斥锁（单写者串行，§6.1 规则 3）、当前 Status、事件订阅集（多订阅者 fan-out，缓冲通道，慢消费者丢弃并计数——取舍写进展记录）。
   - `Apply(ctx, ir)`：锁内 → `ir.Version == 当前 Version` 幂等跳过直接成功（§6.1 规则 1；Status 不变但发 `Applied` 事件与否需取舍并记录）→ FromIR → `SetSnapshot(nodeID, snap)` → 更新 Status{Version, awaiting_confirm} → 发 `Applied` 事件。任何一步失败 → Status.Phase=failed + 同步 error（错误信息带 `stage=assemble|set_snapshot`，§6.3 表）。
   - callbacks（§2.5 + SD7）：`OnStreamRequest` 按 `(node, type_url)` 跟踪；`ErrorDetail != nil` → 结构化记录（type_url、response_nonce、error_detail 原文）→ Status.Phase=nacked + Detail → `Event{Nacked}` → 日志；**不自动重推**（snapshot 保持）。ACK（version_info == 当前 version 且无 ErrorDetail）记日志（debug 级即可）。未知 node id 的连接：不装配 snapshot + warning 日志（§2.3）。
   - 生命周期：`Run/Serve(ctx, lis)` 阻塞服务、ctx 取消时 GracefulStop；未知类型/异常流防御。
3. `internal/core/serve.go`：`RunServe(ctx, cfg, configDir, log)`——加载 configDir（protocol.LoadDir）→ Compile(ModeXDS)（compile 错误逐条日志并返回错误）→ 构造 xds.Server → Apply 首版 → net.Listen(cfg.Deliver.XDS.Listen) → 先 SetSnapshot 再 Accept（§2.2 启动序）→ 阻塞至 ctx 取消 → GracefulStop。启动日志输出 listen 地址、nodeID、IR.Version。
4. `cmd/esgw/serve.go`：`esgw serve -c <esgw.yaml> -f <config-dir>`（两者必填；`-c` 暂无默认值，演进方向记 dev_design 文档）。接线 config.LoadFile → core.RunServe；信号处理（SIGINT/SIGTERM → cancel）。`deliver.mode: static` → 明确报「static 运行时下发未实现（S7）」退出码 1。usage 文案更新。
5. 单测（真实 `127.0.0.1:0` 端口，SD6）：
   - Apply 正路径：编译 testdata/s1 → Apply → 用真实 xDS client（go-control-plane client 或裸 gRPC ADS 流）以 node.id=esgw-node 建流，断言收到五类型响应且 version_info == IR.Version（A2 的单测层）；
   - 幂等跳过：重复 Apply 同 IR → 成功、客户端收不到新推送（断言 nonce 不前进或响应计数不变）；
   - NACK：客户端回带 ErrorDetail 的 DiscoveryRequest → Event{Nacked}（Detail 含 type_url+nonce+原文）、Status.Phase=nacked、且 snapshot 未被替换（后续请求仍收到原 version）；
   - 未知 node id：连接挂起无响应 + warning 日志（断言日志或 Status 无变化）；
   - static mode serve 报错；esgw.yaml 缺失/非法时 serve 退出码。
6. `make build test lint` 全绿。commit 建议拆分：`deliver: Deliverer 接口与状态事件模型 (T3)`、`xds: ADS server 与 ACK/NACK 跟踪 (T3)`、`core/cmd: esgw serve 命令 (T3)`。

## 验收标准

- A2 单测层达成（真实 client 拉全五类型，version 一致）；
- A5 单测层达成（NACK → 事件/Status/不重推）；
- A6 幂等跳过单测达成；
- `esgw serve` 可启动并服务（手工或测试验证），usage 更新。

## 进展记录

| 日期 | 记录 |
|---|---|
| 2026-07-20 | task 创建 |

# T1：M-CORE 轻量设计文档 + esgw.yaml schema + internal/config

- **状态**: 未开始
- **依赖**: 无
- **验收锚点**: requirements A7（硬校验部分）、G1

## 目标

补齐路线图 §1 的两个小缺口：M-CORE 装配层轻量设计文档 + `esgw.yaml` 完整 schema（沉淀至 `dev_design/`），并实现 `internal/config` 包承载 esgw.yaml 的加载、默认值与校验。本 task 只实现 S2 需要的键；后续冲刺的键在文档中预留。

## 上游设计引用

- [下发层设计](../../../../system_design/260717-1-deliver-layer-design.md) §1.3（esgw.yaml 键草案）、§2.7（监听安全硬校验）、§7（M-CORE 启动序）
- [总体架构](../../../../system_design/260715-2-overall-architecture.md) §3（模块划分与依赖单向性）
- 冲刺 [technical_design.md](../technical_design.md) SD2（serve 骨架）、SD4（strict decode 栈）、SD5（adminAddress）
- S1 工程基线 [dev_design/260719-1](../../../dev_design/260719-1-engineering-baseline.md) §1（布局）、§2（工具链）

## 执行步骤

1. 写 `design_docs/dev_manage/dev_design/260720-1-mcore-assembly.md`，内容要点：
   - M-CORE 职责：装配各模块、驱动启动序与优雅退出；S2 形态的启动序（加载 esgw.yaml → LoadDir → Compile → FromIR → SetSnapshot → accept xDS）与关停（SIGTERM/SIGINT → gRPC GracefulStop）；
   - 明确 S2 骨架边界（无 M-CONF/M-STORE/M-STATE/M-PROC，`-f <config-dir>` 为过渡真源，`deliver.mode: static` 报未实现）与各模块就位后的演进方向；
   - `esgw.yaml` schema：本冲刺生效的键（见下）+ 后续冲刺保留键清单（`proc.*`、`deliver.static.*`、`deliver.xds.tls`、`state.*`、`api.*`、`store.*` 等，当前加载时报未知字段错误）。
2. 实现 `internal/config`（package config）：

   ```go
   type Config struct {
       DataDir string          // 默认 /var/lib/esgw
       Deliver DeliverConfig
   }
   type DeliverConfig struct {
       Mode string             // xds | static；默认 xds；static → serve 启动报未实现（SD2）
       XDS  XDSConfig
   }
   type XDSConfig struct {
       Listen       string            // 默认 127.0.0.1:18000
       NodeID       string            // 默认 esgw-node（下发层 §2.3）
       NodeCluster  string            // 默认 esgw
       AdminAddress string            // 默认 unix:///var/run/esgw/envoy-admin.sock（SD5）
       AckTimeout   protocol.Duration // 默认 15s（复用 internal/protocol.Duration，SD4 注释说明）
       // TLS 字段预留：文档列出，代码不加（加了就要实现，P2）
   }
   func LoadFile(path string) (*Config, error)
   ```

   - strict decode：复用 S1 确立的 yaml.v3 Node→JSON + `json.Decoder.DisallowUnknownFields` 路径（参见 `internal/protocol/load.go` 的做法；YAML 栈理由见 260719 technical_design SD2）。esgw.yaml 为单文档。
   - 校验：`Listen` 可解析且**非 loopback 即报错**（下发层 §2.7 硬校验：「未配置 tls 拒绝裸奔」，tls 本冲刺不存在，故一切非 loopback listen 均拒绝，错误信息指明 tls 为 P2 预留）；`Mode` 枚举校验；`AdminAddress` 仅接受 `unix:///<path>` 或 `<host>:<port>` 两种形态。
   - 零值文件/缺省文件行为：`LoadFile` 读不到文件时返回错误（serve 要求显式配置）；空文档 = 全默认，合法。
3. 表驱动单测：默认值、全字段、未知字段报错、非 loopback 拒绝（`0.0.0.0:18000`、具体外网 IP）、loopback 边界（`127.0.0.1`、`::1`、`localhost` 解析语义——注意 `localhost` 是否按 loopback 接受需在进展记录中写明取舍）、mode 非法、adminAddress 形态非法。
4. `make build test lint` 全绿后提交。commit scope：`config` / `docs(dev_design)`。

## 验收标准

- dev_design 文档落地，schema 与代码一致；
- `internal/config` 上述行为全部有单测；
- 非 loopback listen 无 tls → 明确报错（A7 的 serve 侧在 T3 接线后由 T5/单测复核）。

## 进展记录

| 日期 | 记录 |
|---|---|
| 2026-07-20 | task 创建 |

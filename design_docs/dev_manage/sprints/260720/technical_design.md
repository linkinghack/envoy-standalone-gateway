# Sprint 260720 技术设计（S2 / M1：xDS 下发与运行时骨架）

本冲刺不重复系统设计内容——xDS 通道/接入 bootstrap/ACK-NACK/Apply 语义见 [`260717-1 §1~§3、§6、§7`](../../../system_design/260717-1-deliver-layer-design.md)。本文只补充冲刺级实现决策与任务间接口冻结点。M-CORE 装配与 `esgw.yaml` schema 的沉淀性设计由 T1 产出至 `dev_design/`（路线图 §1 小缺口）。

---

## 1. 冲刺内包结构与任务映射

```
internal/config/          # T1：esgw.yaml 加载/defaults/校验（strict decode）
internal/deliver/
    deliver.go            # T3：Deliverer 接口、Status/Phase/Event（下发层 §6）
    xds/snapshot.go       # T2：FromIR 纯函数装配 + Consistent 自检（§2.1）
    xds/server.go         # T3：ADS server + SnapshotCache 生命周期 + callbacks（§2.2~§2.6）
    xds/bootstrap.go      # T4：接入 bootstrap 渲染纯函数（§2.7）
internal/core/            # T3：M-CORE 装配骨架（启动序、信号处理、serve 主循环）
cmd/esgw/                 # T3：serve 子命令；T4：bootstrap 子命令
e2e/                      # T5：ADS e2e（compose + 断言脚本），与既有 static e2e 并存
design_docs/dev_manage/dev_design/260720-1-mcore-assembly.md   # T1：M-CORE + esgw.yaml schema 沉淀文档
```

任务依赖：

```
T1 ──► T2 ──► T3 ──► T4 ──► T5
        （T2 只依赖 IR 契约，与 T1 可并行；serve 接线在 T3 消费 T1 的 config）
```

## 2. 冲刺级实现决策

| # | 决策 | 说明 |
|---|---|---|
| SD1 | 新增直接依赖 `github.com/envoyproxy/go-control-plane`（根模块，**v0.14.0**）提供 `pkg/cache/v3`、`pkg/server/v3`、`pkg/resource/v3`；`envoy` 子模块维持 v1.37.0（MVS 共存，根模块声明的 v1.36.0 被提升）。`google.golang.org/grpc` 随根模块引入 | 依赖引入单独 commit（工程基线 §5） |
| SD2 | `esgw serve` 本冲刺为 M-CORE **骨架**：`esgw serve -c <esgw.yaml> -f <config-dir>`，启动序 = 加载 esgw.yaml → LoadDir → Compile(mode=xds) → FromIR → SetSnapshot → 再 accept xDS 连接（下发层 §2.2 启动链路）。`deliver.mode: static` 启动即报「未实现（S7）」错误 | 配置真源 CRUD/发布流归 S3，本冲刺不做 |
| SD3 | `UpdateEndpoints` 按下发层 §6.1 定义进 Deliverer 接口，xds 实现返回 `ErrEndpointsUnsupported`（M-DISCO 未落地，防御性契约） | 下发层 §3 落地时机归 k8s disco 冲刺 |
| SD4 | `internal/config` 复用 T2 确立的 YAML 栈（yaml.v3 Node→JSON + `DisallowUnknownFields`，SD2/260719）：esgw.yaml 单文档、strict decode、未知字段报错。`proc.*` 等后续冲刺的键在 schema 文档中列为**保留名**，当前不接受 | 防止拼写错误静默生效 |
| SD5 | 接入 bootstrap 的 admin 地址取自 esgw.yaml `deliver.xds.adminAddress`，默认 `unix:///var/run/esgw/envoy-admin.sock`（下发层 §2.7）。仅支持 `unix:` pipe 与 `host:port` 两种形态；e2e 用可写路径覆盖。static 渲染的 admin 默认（/tmp/esgw-admin.sock，S1 T6 决议）本冲刺不动，S7 统一 | 与 M-STATE admin 白名单设计（260717-4 §1.3）对齐 |
| SD6 | ADS server 测试用真实 `127.0.0.1:0` 端口（不引 bufconn 额外抽象）；e2e 用 docker compose 独立网络，esgw 以宿主编排/容器两态运行（容器态与 static e2e 的既有模式对齐） | 与 T7(260719) e2e 设施同构 |
| SD7 | ACK 判定按下发层 §2.5：`version_info == 当前 snapshot version 且无 ErrorDetail`；NACK 只记录/上报（Event + Status + 日志），不自动重推不回滚。`ackTimeout` 仅作观察窗口字段保留在 config，本冲刺不实现超时告警逻辑（S4 随 M-STATE 一并做） | 受理语义不阻塞（§6.1） |

## 3. 任务间关键接口（冻结点）

以下签名在对应 task 完成时冻结，后续 task 不得破坏（改动需回到本文件记录）：

```go
// internal/config（T1）
type Config struct { DataDir string; Deliver DeliverConfig /* ... 见 T1 task 文档 */ }
func LoadFile(path string) (*Config, error)   // strict decode + defaults + 校验（含 loopback 硬校验）

// internal/deliver（T3；下发层 §6.1/§6.2）
type Deliverer interface {
    Apply(ctx context.Context, ir *ir.IR) error
    UpdateEndpoints(ctx context.Context, updates map[string]*endpointv3.ClusterLoadAssignment) error
    Status() Status
    Events() (<-chan Event, cancel func())
}
type Status struct { Version string; Phase Phase; Detail string; UpdatedAt time.Time }
type Phase  // idle | delivering | awaiting_confirm | confirmed | nacked | failed
type Event  // Kind: Applied | Nacked（HotRestartFailed/SupervisorDegraded 枚举预留，S7 使用）

// internal/deliver/xds（T2 冻结 FromIR，T3 冻结 Server，T4 冻结 RenderBootstrap）
func FromIR(i *ir.IR) (*cache.Snapshot, error)                  // 纯函数；含 Consistent() 自检
func RenderBootstrap(opts BootstrapOpts) ([]byte, error)        // 纯函数；确定性 YAML（同 static.Render 发射器）

// internal/core（T3）
func RunServe(ctx context.Context, cfg *config.Config, configDir string, log *slog.Logger) error
```

## 4. 风险与对策

| 风险 | 对策 |
|---|---|
| go-control-plane v0.14 `cache.Snapshot`/`server.Callbacks` API 与设计文档示例（v0.13 时代）有出入 | T2 开工先核对实际 API；出入点记录 task 进展，语义（纯函数装配 + Consistent 自检 + ACK/NACK 回调）不得偏离 |
| grpc + go-control-plane 根模块引入传递依赖膨胀、licenses 检查翻车 | T2 依赖引入单独 commit，立即跑 `make lint` 与 go-licenses 检查；有问题当步解决不积压 |
| ADS 回调中 `OnStreamRequest` 对 ADS 单流多类型的语义（每类型独立 version_info/nonce） | T3 单测覆盖「按 type_url 分别跟踪」；e2e 断言五类型均 ACK |
| e2e 中 esgw 与 envoy 启动时序（envoy 先连时 ADS 未就绪） | 设计已覆盖（SnapshotCache 常驻、重连语义 §2.6）；compose 用 healthcheck/重试收敛，不靠 sleep 硬等 |
| NACK 单测需要真实 SnapshotCache 行为 | 用真实 server + 构造含 ErrorDetail 的 DiscoveryRequest 直接调 callbacks（单元级），不依赖真实 Envoy NACK（e2e 不强制 NACK 场景） |

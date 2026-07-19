# Sprint 260719 技术设计（M0：协议与编译器）

本冲刺不重复系统设计内容——协议规格见 [`260716-1`](../../../system_design/260716-1-gateway-config-protocol-v0.md)，编译流水线/IR/测试策略见 [`260716-2`](../../../system_design/260716-2-compile-ir-design.md)，static 渲染契约见 [`260717-1 §4.1`](../../../system_design/260717-1-deliver-layer-design.md)。本文只补充冲刺级实现决策与任务间接口。工程基线（目录结构、工具链、CI）沉淀在 [`dev_design/260719-1-engineering-baseline.md`](../../dev_design/260719-1-engineering-baseline.md)。

---

## 1. 冲刺内包结构与任务映射

```
cmd/esgw/                 # CLI 入口（T6）：compile 子命令
internal/protocol/        # T2：信封/五对象类型、strict decode、loader(Origin)、defaults、schema 生成
internal/compile/         # T3(F2 link) T4(F3 build) T5(F4 patch/F5 materialize/F6 validate)
    envoycheck/           # T6：F7 `envoy --mode validate` exec 封装（独立子包可禁用）
internal/ir/              # T5：IR 类型、SourceMap、确定性哈希
internal/deliver/static/  # T6：Render(ir) 纯函数（M0 只实现 render，不含 writer/热重启）
testdata/                 # T7：golden 用例（编译层 §8.1 布局）
e2e/                      # T7：docker 冒烟脚本
```

任务依赖（可并行性对 AI 工程师同样成立——不同会话领不同 task 时必须按此顺序）：

```
T1 ──► T2 ──► T3 ──► T4 ──► T5 ──► T6 ──► T7(e2e)
              └────────────────────────► T7(golden 可随 T4/T5 增量补)
```

## 2. 冲刺级实现决策

| # | 决策 | 说明 |
|---|---|---|
| SD1 | Go ≥ 1.22；module 名 `github.com/linkinghack/envoy-standalone-gateway`（D1 命名未决，代码内二进制/常量统一用 `esgw`，集中在 `internal/version` 便于日后改名） | 基线文档 §2 |
| SD2 | YAML 解析（T2 落地后修订）：`gopkg.in/yaml.v3`（YAML 1.2 core schema）自实现 Node→JSON 转换 + `encoding/json` `DisallowUnknownFields` 实现 strict decode（协议 P6）；多文档经 yaml.v3 Decoder 逐文档拆分，Origin 记录 `(file, docIndex)`。**修订原因**：原定的 `sigs.k8s.io/yaml` 底层是 yaml.v2（YAML 1.1），会把裸键 `on:` 解析为布尔 `true`，与协议 §3.3 `retry.on` 冲突（T2 实测复现） | YAML→JSON→strict decode 架构不变，仍不引入第三方 strict 方案 |
| SD3 | 时长字段统一 `type Duration`（包装 `time.Duration`，JSON/YAML 编解码 Go duration 字符串），协议 §4 约定 | protocol 包内 |
| SD4 | protobuf 依赖锁定 `github.com/envoyproxy/go-control-plane` 当前稳定版；Envoy 支持区间初定**最近 3 个 minor**（写入 `internal/version/envoy.go` 常量，CI 矩阵同源） | RK4/NFR-6 |
| SD5 | 编译器主体（F2~F6）**不 import** envoycheck 与任何 IO——`Compile()` 保持纯函数可测（编译层 §6 Options 注入） | 架构 A1 |
| SD6 | golden file 序列化：static 产物用确定性 YAML 渲染；xds 产物用 protojson + `json.Indent` 后按 key 排序输出，保证快照稳定 | 编译层 §5/§8 |
| SD7 | e2e 用 `docker compose`：envoy 官方镜像 + 两个 `hashicorp/http-echo` 后端模拟 S1；证书用测试自签（`testdata/certs/` 提交仓库，CN/SAN 固定） | 编译层 §8.4 |
| SD8 | S2 场景 e2e 只做 `--mode validate` 级验证（jwt/jwks 需外部依赖，真实流量断言留 S2 冲刺 ADS e2e 一并做） | 控制风险面 |

## 3. 任务间关键接口（冻结点）

以下签名在 T2/T3 完成时冻结，后续 task 不得破坏（改动需回到本文件记录）：

```go
// internal/protocol
type ConfigSet struct { Gateway *Gateway; Listeners []*Listener; Routes []*Route;
    Upstreams []*Upstream; Policies []*Policy; EnvoyResources []*EnvoyResources }
func LoadDir(dir string) (*ConfigSet, []LoadError)      // strict decode + Origin + 重名检测
func Schemas() ([]byte, error)                          // JSON Schema bundle（C3 决议工具生成）

// internal/compile（编译层 §6 公共入口，M0 全量实现）
func Compile(cs *protocol.ConfigSet, opts Options) (*ir.IR, []CompileError)
type Options struct { Mode Mode /* ModeXDS | ModeStatic */
    EndpointSource EndpointSource  // M0 恒 nil；kubernetesService 出现即报编译错误
    EnvoyValidate *EnvoyValidateOpts }                  // nil = 跳过 F7

// internal/deliver/static
func Render(i *ir.IR) ([]byte, error)                   // 纯函数；含文件头注释与 node.metadata 版本注入
```

## 4. 风险与对策

| 风险 | 对策 |
|---|---|
| PGV Validate 对手工构造 proto 的报错信息晦涩 | CompileError 包装时附资源名与 SourceMap 回指；F6 错误必须可读（A5 验收） |
| SNI filter chain 生成（证书 SAN 提取）边界多（通配、多 SAN、CN 回退） | T4 对 ListenerBuilder 单独出表驱动用例：单证书/多证书/通配 SAN/无 SAN 有 CN/SAN 重叠报错 |
| jwt requirement_map per-route 装配复杂（协议"就近覆盖"语义） | T4 中 policy 归一化先独立成纯函数（输入四级挂接，输出每 rule 最终生效策略集），单测穷举覆盖后再接 Envoy 装配 |
| golden 快照因依赖升级抖动 | protojson 输出经确定性后处理（SD6）；go-control-plane 版本升级必须单独 commit 并刷新快照 |

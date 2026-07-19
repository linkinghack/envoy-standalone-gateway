# T2 internal/protocol：协议类型与加载

- **状态**: 已完成
- **依赖**: T1
- **设计依据**: [协议 v0 全文](../../../../system_design/260716-1-gateway-config-protocol-v0.md)（§2 信封、§3 五对象规格、§4 通用约定、§6 版本化、§7 escape hatch 字段形态）、[编译层 §2](../../../../system_design/260716-2-compile-ir-design.md)（ConfigSet）、冲刺技术设计 SD2/SD3、接口冻结点 §3

## 目标

协议 v0 的 Go 类型体系 + strict decode + 目录加载（Origin/重名检测）+ defaults 常量 + JSON Schema 生成（落地 C3）。**本 task 不做跨对象语义校验**（那是 T3/F2），只做单文档结构层。

## 执行步骤

1. **信封与类型**：`Envelope{APIVersion, Kind, Metadata{Name, Labels}, Spec}`；五对象 + `EnvoyResources` 的 spec 结构体，严格按协议 §3 字段与默认值注释逐字段实现（字段名 camelCase JSON tag）。协议文档是唯一规格来源，实现中发现的歧义记录到"进展记录"并在 PR 中提出，不得擅自增删字段。
2. **通用类型**：`Duration`（SD3）、`name` 正则校验（`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`）、枚举字符串常量与合法值表。
3. **strict decode**：单 YAML 文档 → Envelope → 按 kind 分发到具体 spec 类型；未知字段报错（SD2）；apiVersion 仅接受 `esgw/v1alpha1`（版本转换层留空壳，协议 §6 多版本共存在后续版本才需要）。
4. **LoadDir**（冻结接口）：扫描 `*.yaml|*.yml` 按文件名字典序、`---` 多文档拆分、记录 `Origin{File, DocIndex}`、同 kind 重名报错（报两处 Origin）；错误类型 `LoadError{Origin, Message}` 收集不中断。
5. **defaults.go**：协议声明的全部默认值集中此文件（编译层 F2 消费；本 task 只定义常量与 `ApplyDefaults` 函数，调用时机归 T3）。
6. **JSON Schema 生成**（C3 决议）：评估 invopop/jsonschema 对现有类型的输出质量（枚举、oneOf 三选一、Duration 格式）；不满足则退回手写 schema + 类型一致性测试。决议写入 plan_todos_trace 并回写编译层文档 C3。`Schemas()` 返回 bundle。
7. **单元测试**：每对象合法/非法样例；未知字段报错；重名检测；S1/S2 演练 YAML（协议 §8 原文）能被 LoadDir 完整解析——这是 A1 的输入侧验收。

## 验收

- [x] 协议 §8.1/§8.2 YAML 原文解析通过，对象数量与字段值断言正确
- [x] 未知字段、坏枚举、坏 name、重名均报错且带 Origin
- [x] `Schemas()` 输出可被标准 JSON Schema 校验器加载
- [x] C3 决议已记录并回写

## 进展记录

- 2026-07-19（会话 1）：T2 完成。
  - **完成内容**：信封/五对象/EnvoyResources 全部类型（camelCase JSON tag、默认值注释）；`Duration`（SD3）；name 正则与全部枚举；strict decode（encoding/json DisallowUnknownFields）；`LoadDir`（文件名字典序、yaml.v3 多文档拆分、Origin{File, DocIndex 0 起}、同 kind 重名报错带两处 Origin、错误收集不中断）；`defaults.go` 全部默认值常量 + `ApplyDefaults`（未接线，调用归 T3）；`Schemas()` JSON Schema bundle。冻结接口 `ConfigSet/LoadDir/Schemas/LoadError` 与 technical_design §3 一致。测试 57 个用例全绿；`make build && make test && make lint` 全绿（lint 0 issues）。commits：`6b76692`（类型）、`39facc0`（loader+testdata）、`4f0ba48`（defaults）、`2b4e4de`（schema）。
  - **结构层校验范围**（单文档内，非跨对象）：枚举值、name 正则、证书 oneOf（ref xor certFile+keyFile）、path/header 匹配三选一、rule 动作三选一、Upstream 端点来源三选一、Policy 类型键唯一、extAuth/jwks 形态、CIDR、端口范围、Gateway name 必须为 default。Route 的 rules/forward 互斥按任务要求留给 T3。
  - **与设计文档的偏离（已记录，未静默）**：SD2 原拟 `sigs.k8s.io/yaml` 做 YAML→JSON，但其底层 yaml.v2 是 YAML 1.1——`on:` 键会被解析为布尔 `true`，协议 §3.3 的 `retry.on` 必然踩坑。改用 `gopkg.in/yaml.v3`（YAML 1.2 core schema，`on` 保持字符串）自实现 Node→JSON 转换，「YAML→JSON→DisallowUnknownFields」的 SD2 架构不变。建议后续修订 SD2 条文。
  - **C3 决议**：采用 invopop/jsonschema 由 Go 类型生成（协议 §6 要求「Schema 由协议 Go 类型生成，单一事实来源」）。对 union/特殊类型用 `JSONSchema()` 钩子补足：`PolicyAttachment`（oneOf 字符串|PolicySpec）、`Duration`（string+pattern）、`RawJSON`（任意）、全部枚举（enum 值）；后置注入每文档 apiVersion/kind 常量与 metadata.name pattern。bundle = 顶层 oneOf 六文档 + 共享 $defs，additionalProperties:false 与 strict decode 对齐。测试用 santhosh-tekuri/jsonschema/v6 加载 bundle 并校验 S1/S2 全部文档通过、5 类坏文档被拒。已回写 plan_todos_trace 与编译层文档 §9。
  - **歧义记录（不阻塞，留后续版本）**：① `basicAuth.users` 形态（htpasswd 文件路径或内联内容）按 string 实现，编译期再区分语义；② Listener `tls` 在 protocol=HTTPS/TLS 时的必填校验属条件性语义校验，留 T3；③ jwt 未声明 optional 时 issuer/jwks 是否必填规格未明说，加载期未强制。
  - **下一步**：T3（F2 链接与语义校验）消费 ConfigSet/ApplyDefaults。

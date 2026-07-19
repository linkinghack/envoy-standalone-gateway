# T3 internal/compile F2：链接与语义校验

- **状态**: 已完成
- **依赖**: T2
- **设计依据**: [编译层 §3/F2、§4 错误模型](../../../../system_design/260716-2-compile-ir-design.md)、协议 §3 各对象约束

## 目标

实现编译流水线 F2：引用解析、跨对象语义校验、默认值填充；建立整个编译层共用的 `CompileError{Stage, Source, Message, Severity}` 错误模型（含 SourceRef 到 YAML 路径）。

## 执行步骤

1. **CompileError 与 SourceRef**：`SourceRef{File, Kind, Name, Path}`，Path 为 `spec.rules[2].retry.on` 形态；实现从 protocol Origin + 字段路径构造的辅助函数。这是全冲刺错误质量的地基，先行冻结。
2. **引用解析**：Route→Listener、Route.backends→Upstream、policies 字符串引用→Policy 建立指针；悬空引用报错。
3. **跨对象约束**（编译层 F2 清单逐条实现，每条一个可单测的规则函数）：
   - Listener `address:port` 唯一；
   - Route 形态与 Listener 协议匹配（HTTP rules vs L4 forward 互斥，协议 §3.3.5）；
   - 同 Listener 下 hostname 同精度冲突检测（协议 §3.3 要点 1）；
   - Policy spec 单类型键；策略挂接层级合法性；
   - 证书文件存在性与密钥配对（openssl 语义：cert/key 匹配校验）；`ref:` 形态在 M0 报"证书库未实现"明确错误；
   - TLS 必配/禁配规则（HTTPS/TLS 必须有 tls，HTTP 不得有）；
   - `kubernetesService` 端点来源在 M0（EndpointSource == nil）报"当前环境未启用 k8s 服务发现"；
   - Gateway 显式声明时 name 必须为 `default`；
   - rule 动作三选一（backends/redirect/directResponse）互斥且必选其一。
4. **默认值填充**：调用 T2 `ApplyDefaults`，注意"HTTPS 的 http2 默认 true"等条件默认值。
5. **收集不中断**：同阶段全部错误一次返回；阶段间短路语义在 `Compile()` 骨架中实现（本 task 先搭 F1→F2 骨架，F3+ 占位）。
6. **表驱动测试**：上述每条规则至少一个反例 + 一个恰好合法的边界例；断言 Stage/SourceRef.Path/Message（A5 验收核心）。

## 验收

- [x] F2 规则清单全部实现且各有反例测试
- [x] 错误全部带 SourceRef 且 Path 精确到字段
- [x] S1/S2 演练配置过 F2 零错误

## 进展记录

### 2026-07-19 完成（commits `86333d5` / `2b3714a` / `feda68a`）

**落地内容**（`internal/compile`）：

- `errors.go`：`CompileError{Stage, Source, Message, Severity}` 错误模型；`SourceRef{File, Kind, Name, Path}`（Path 形如 `spec.rules[2].retry.on`）；Stage 枚举 `schema|link|build|patch|validate|envoy`；`srcRef()` 从 protocol.Origin + kind/name + 字段路径构造。
- `options.go`：`Options{Mode, EndpointSource, EnvoyValidateOpts}`（technical_design §3 冻结签名）；`Mode` 枚举 `ModeXDS|ModeStatic`；`EndpointSource` 接口体本次定义（冻结点只约束 Options 字段）：`InitialEndpoints(protocol.KubernetesServiceSource) ([]protocol.Endpoint, error)`。
- `link.go`：F2 `link()`——先 `protocol.ApplyDefaults`（须在 address:port 唯一性等依赖默认值的规则之前），再逐条规则函数，同阶段收集不中断：
  - `resolveReferences`：Route→Listener、rules[].backends[].upstream / forward.upstream→Upstream、Gateway/Listener/Route/rule 四级 policies 字符串引用→Policy；悬空引用 Path 精确到元素下标（如 `spec.rules[0].backends[0].upstream`）。
  - `checkListenerAddressUnique`（address:port 唯一，IPv6 经 `net.JoinHostPort` 归一）。
  - `checkRouteForm`：rules↔forward 互斥且必选其一；HTTP 类（HTTP/HTTPS）↔ L4 类（TCP/TLS/UDP）形态匹配；`forward.sniHosts` 仅 protocol: TLS 可用。
  - `checkHostnameConflicts`：同 Listener 下同精度 hostname 重复报错（省略 = 兜底 `*`；大小写不敏感）。
  - `checkPolicyAttachmentLevels`：ipAccess 不可挂 rule 级。
  - `checkTLSRules`：HTTPS/TLS 必配 spec.tls，HTTP/TCP/UDP 禁配。
  - `checkCertificates`：`ref:` 形态报"证书库 M0 未实现"；cert/key 存在性+配对；clientCA 存在+可解析。
  - `checkKubernetesService`：`EndpointSource==nil` 时报"未启用 k8s 服务发现"。
  - loader 已做的校验（枚举/name/oneOf/三选一/Policy 单类型键/Gateway name=default/同 kind 重名）未重复实现。
- `cert.go`：`certVerifier` 抽象（默认实现 = `crypto/tls.LoadX509KeyPair` 做存在性+PEM 解析+公钥配对，即 openssl cert/key match 语义；不 exec openssl）；`link()` 接受 verifier 参数，测试注入假实现，Compile 主体保持可测（SD5）。
- `compile.go`：`Compile()` 骨架——F1（loader 已完成）→F2，Error 级错误阶段间短路；F3+ 占位返回一条 build 阶段未实现错误。注意：Compile 会经 ApplyDefaults 原地填充 cs 默认值（F2 职责，protocol 包既定设计）。
- `internal/ir/ir.go`：最小占位 `type IR struct{}`（T5 落地完整定义）。
- 测试（33 个用例全绿）：每条规则至少一个反例 + 一个恰好合法边界例，断言 Stage/SourceRef(Path/Kind/Name)/Message 子串；`TestS1PassF2`/`TestS2PassF2` 用 `t.TempDir()` 动态生成配对自签证书（不提交私钥）跑真实 `defaultCertVerifier`。

**与设计的偏差/澄清（非静默偏离）**：

1. 编译层文档原文"ipAccess 不可挂 rule 级以下"——rule 已是最低挂接层级，按"ipAccess 不可挂 rule 级"实现（允许 Gateway/Listener/Route 级）；协议 §3.5 未定义完整层级矩阵，v0 仅此一条限制。
2. 协议 §3.3.5 只写"rules 与 forward 互斥"；两者皆空的 Route 无意义，本层补充报"必选其一"错误（Path `spec`）。
3. hostname 冲突比较做大小写归一（DNS 语义），协议未明确，按不敏感实现。
4. 证书文件检查本质是 IO：`Compile()` 使用默认 os 实现（早失败语义属 F2 职责，编译层 §3 F2 明确"此处早失败"），但 `link()` 以 `certVerifier` 函数参数注入，测试不依赖真实文件，符合 SD5"主体可测"的意图。
5. TLS passthrough（protocol: TLS）按协议 §3.2"HTTPS/TLS 时必填"要求 spec.tls 且证书条目同样做存在性/配对校验。
6. S2 演练 YAML（protocol testdata）按协议 §8.2 原文省略了 https Listener 与 user-svc/user-svc-canary/auth-svc 三个 Upstream；验收测试补齐同构对象后过 F2 零错误。S1 证书路径替换为测试生成的临时证书。

`make build && make test && make lint` 全绿（gofumpt 已格式化）。

# T5 internal/compile F4/F5/F6 + IR

- **状态**: 已完成
- **依赖**: T4
- **设计依据**: [编译层 §1 IR、§3 F4/F5/F6、§5 确定性与版本、协议 §7 escape hatch](../../../../system_design/260716-2-compile-ir-design.md)

## 目标

escape hatch 合成（F4）、下发形态化（F5）、PGV 校验（F6）、`internal/ir` 类型（IR/SourceMap/Version 哈希），打通 `Compile()` 全流水线。

## 执行步骤

1. **internal/ir**：IR 结构（编译层 §1 定义）、`ResourceKey/SourceRef` 与 SourceMap、确定性序列化 + SHA-256 前 12 位版本哈希。哈希实现注意：proto 确定性序列化用 `proto.MarshalOptions{Deterministic: true}` 且按资源类型+名称排序拼接。
2. **F4 envoyPatch**：
   - **C1 决议**：target 合法取值全集按对象 kind 定表（Listener→listener/secret；Route→virtualHost/route；Upstream→cluster/endpoints；Gateway→bootstrap 不允许，C2 留 M1）；rule 级定位采用**给 rule 加可选 `name` 字段**（倾向方案），无 name 时不可被 rule 级 patch 定位。决议回写协议与编译层文档。
   - `merge`（RFC 7386，protojson→merge→protojson 回转）与 `jsonPatch`（RFC 6902）两种 op；回转失败报编译错误并回指 patch 位置；同对象多 patch 按书写顺序。
3. **F4 EnvoyResources 合并**：`@type` 分发入 IR 集合；重名默认报错、`allowOverride: true` 整体替换并更新 SourceMap 归属。
4. **F5 形态化**：`ModeXDS`——Listener 引 RDS/SDS、证书进 `IR.Secrets`、动态端点引 EDS；`ModeStatic`——route_config 内联、证书文件路径直落 transport socket、端点内联。这是编译层唯一感知模式的阶段，实现为独立纯函数便于两模式对拍。
5. **F6 校验**：全资源 PGV `Validate()`；跨资源引用闭合（RDS/EDS/SDS 名、route→cluster，含 EnvoyResources 引入的资源）；错误包装带资源名与 SourceMap 回指。
6. **测试**：
   - escape hatch 正例：协议 §7.1/§7.2 两个文档示例逐字实现为用例；
   - 坏 patch 反例：字段不存在、类型不符、patch 出非法资源被 F6 拦截（"要么编译错误要么产物过校验，无第三态"）；
   - patch 模糊测试（编译层 §8.5）：随机 JSON Patch 断言无第三态（可标 `-short` 跳过）；
   - 确定性：同一输入两次 Compile，`IR.Version` 与序列化产物字节一致（A6）。

## 验收

- [x] `Compile()` F1~F6 全流水线对 S1/S2 + escape hatch 用例产出合法 IR
- [x] 坏 patch 全部被拦截且错误回指正确
- [x] 确定性测试通过
- [x] C1 决议已记录并回写协议/编译层文档

## 进展记录

### 2026-07-19 完成（commits `a649447` / `eda144b` / `e1b0cdf` / `bc0b5e6` / `191f708`）

**交付**：
- `internal/ir`：IR 全结构（五类资源 map + Bootstrap + Version + SourceMap）、
  ResourceKey/SourceRef、`MarshalDeterministic`（类别固定序 + 名排序 +
  `proto.MarshalOptions{Deterministic:true}` 长度前缀拼接）与
  `ComputeVersion`（SHA-256 前 12 位 hex）。
- F4（`patch.go`）：C1 定表定位 + merge/jsonPatch 两种 op +
  EnvoyResources @type 分发/重名/allowOverride；F5（`materialize.go`）两模式
  形态化（纯函数可对拍）；F6（`validate.go`）PGV + Any typed_config 深度校验 +
  RDS/EDS/SDS/route→cluster 引用闭合；`Compile()` 接线 F2→F3→F4→F5→F6。
- C1 决议落地：protocol Rule 加可选 `name`（Route 内唯一、字符集同
  metadata.name，重名/非法名 loader 报错）；rule 级定位语法
  `target: route/<ruleName>`；无 name 的 rule 不可定位。已回写协议 §3.3
  要点 4、§7.1 target 定表、编译层 §9。

**测试**：§7.1/§7.2 文档示例逐字正例（两模式）；坏 patch 反例 7 类
（回转失败×2、缺失路径、PGV 拦截、引用闭合拦截、非法 target、Gateway 禁止、
无 name rule）；rule/secret/virtualHost patch 正例；EnvoyResources 重名/
override；两模式对拍（`proto.Equal`）；确定性（Version+字节，A6）；
patch 模糊 300 轮固定种子无第三态（`-short` 跳过）；S1/S2 两模式全流水线。
`make build && make test && make lint` 全绿。

**实现决策/与设计的偏差（均已核对不冲突或记录在案）**：
1. `SourceRef` 定义移至 `internal/ir`（IR.SourceMap 与 CompileError 共用），
   compile 包别名复用——设计 §1/§4 本就同一概念，避免循环依赖。
2. patch 域 protojson 用 `UseProtoNames + EmitDefaultValues`：缺省字段默认
   省略会导致 jsonPatch `replace` 目标键不存在（§7.1 示例
   `replace /dns_lookup_family` 正是此写法），EmitDefaultValues 是文档示例
   逐字可跑的前提。
3. Listener→`secret` target：多证书时同一 patch 依书写顺序施加于该 Listener
   的**每一张**证书 Secret（`crt/<listener>/<n>`）；按单张证书定位（如
   `secret/<n>`）留待有真实需求再加。
4. F5 xds 仅把 `STATIC + 内联 CLA` 的 Cluster 转 EDS；LOGICAL/STRICT_DNS 的
   CLA 是解析机制本身，两模式均保持内联（对设计 §1「Cluster 引 EDS」的细化）。
5. static 模式 `IR.Routes` 只保留 EnvoyResources 引入的 RouteConfiguration
   （编译产物 rc 全部内联进 HCM）；`IR.Endpoints`/`IR.Secrets` 同理只保留
   用户提供的部分。T6 渲染器据此决定 static_resources 内容。
6. Bootstrap 骨架 = node(`esgw`) + admin(127.0.0.1:9901)；xds 的
   `dynamic_resources`（ADS 端点）属下发层配置，T6 渲染/装配时接入。
7. F5 暴露的错误（patch 删证书/删 rc 等）归入 `patch` 阶段——错误模型
   （§4）无 materialize stage，F5 失败皆由 F4 patch 后果引起。
8. F6 对 Any 深度校验时跳过 protoregistry 未注册的扩展类型（用户可经
   EnvoyResources 引入本库未链接的扩展，PGV 无从校验，表达力上限不受限）。
9. `Options.Mode` 非法取值报 build 阶段错误（冻结签名无返回值可改）。
10. jwt-jwks/* 抓取集群 SourceMap 不回指单一 Policy（多策略可共享，C4）；
    其 F6 错误退化为裸资源名回指。

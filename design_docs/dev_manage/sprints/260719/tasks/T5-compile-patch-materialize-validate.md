# T5 internal/compile F4/F5/F6 + IR

- **状态**: 未开始
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

- [ ] `Compile()` F1~F6 全流水线对 S1/S2 + escape hatch 用例产出合法 IR
- [ ] 坏 patch 全部被拦截且错误回指正确
- [ ] 确定性测试通过
- [ ] C1 决议已记录并回写协议/编译层文档

## 进展记录

（接手会话在此追加）

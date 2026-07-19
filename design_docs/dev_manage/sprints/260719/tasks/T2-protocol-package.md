# T2 internal/protocol：协议类型与加载

- **状态**: 未开始
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

- [ ] 协议 §8.1/§8.2 YAML 原文解析通过，对象数量与字段值断言正确
- [ ] 未知字段、坏枚举、坏 name、重名均报错且带 Origin
- [ ] `Schemas()` 输出可被标准 JSON Schema 校验器加载
- [ ] C3 决议已记录并回写

## 进展记录

（接手会话在此追加）

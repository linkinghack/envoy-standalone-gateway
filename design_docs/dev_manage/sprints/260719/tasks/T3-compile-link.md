# T3 internal/compile F2：链接与语义校验

- **状态**: 未开始
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

- [ ] F2 规则清单全部实现且各有反例测试
- [ ] 错误全部带 SourceRef 且 Path 精确到字段
- [ ] S1/S2 演练配置过 F2 零错误

## 进展记录

（接手会话在此追加）

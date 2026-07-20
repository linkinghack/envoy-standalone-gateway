# Sprint 260720 任务拆分与进展追踪

> **给接手会话的指引（Codex / Claude Code 必读）**
> 1. 先读本文件确定当前进行到哪个 task；再读 `tasks/<ID>.md` 获取该 task 的详细步骤与已记录进展。
> 2. 开工前把对应 task 状态改为 `进行中`，完成后改为 `已完成` 并填写完成摘要；**每次会话结束前必须回写进展**（做到哪一步、遇到什么问题、下一步是什么）。
> 3. 遵守 [`technical_design.md`](technical_design.md) §3 的接口冻结点；需要变更接口时先更新该文件并在此处记录。
> 4. 每个 task 至少一个独立 commit，提交信息格式见[工程基线](../../dev_design/260719-1-engineering-baseline.md) §5。
> 5. 实现与设计冲突时不得静默偏离：在 task 进展记录中写明并提出修订建议。

## 任务总览

| ID | 任务 | 状态 | 依赖 | 验收锚点 |
|---|---|---|---|---|
| T1 | M-CORE 轻量设计文档 + esgw.yaml schema + internal/config | 已完成 | — | A7（硬校验部分）、G1 |
| T2 | deliver/xds FromIR 纯函数装配 + 依赖引入 | 已完成 | T1（可并行，只依赖 IR 契约） | A1 |
| T3 | Deliverer 接口 + ADS server + ACK/NACK + esgw serve | 已完成 | T1、T2 | A2、A5、A6 |
| T4 | 接入 bootstrap 渲染 + esgw bootstrap 命令 | 未开始 | T1（T3 后可独立做） | A7 |
| T5 | ADS e2e + CI 集成 + 收口验收 | 未开始 | T3、T4 | A2~A4、A5（日志）、A7、A8 |

状态枚举：`未开始` / `进行中` / `已完成` / `受阻(附原因)`

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-20 | 冲刺创建：需求、技术设计、5 个 task 文档就绪；依赖决策 SD1（go-control-plane 根模块 v0.14.0 + envoy v1.37.0）已实测 MVS 兼容；等待 T1 开工 |
| 2026-07-20 | T1 完成：dev_design/260720-1-mcore-assembly.md + internal/config（LoadFile strict decode + defaults + 校验，含非 loopback listen 硬校验）；commit `ad9f052`（docs）、`10bcacb`（config）；`make build test lint` 全绿 |
| 2026-07-20 | T2 完成：internal/deliver/xds FromIR 纯函数装配（Consistent + SDS 闭合补检）+ go-control-plane v0.14.0 依赖引入；commit `7c0fe64`（deps）、`bbf1fbd`（xds）；`make build test lint` 全绿，go-licenses 通过 |
| 2026-07-20 | T3 完成：internal/deliver Deliverer 接口与状态事件模型、xds ADS server（ACK/NACK 跟踪、事件 fan-out）、internal/core RunServe 与 esgw serve 命令；commit `3987962`（deliver）、`ccd30e9`（xds）、`3051d49`（core,cmd）；`make build test lint` 全绿，deliver/xds 与 core 过 -race |

## 决议记录（冲刺内产生的设计决策）

| 未决项 | 决议 | 日期 | 已回写文档 |
|---|---|---|---|
| `deliver.xds.listen` 的 host 为 `localhost` 时是否算 loopback（任务书要求自定取舍） | 按 loopback 接受；其余非 IP 字面量主机名一律拒绝（不做运行期 DNS 解析，保证校验确定性） | 2026-07-20 | dev_design/260720-1-mcore-assembly.md §3.1、T1 进展记录 |
| go-control-plane v0.14 `Snapshot.Consistent()` 不覆盖 SDS 引用闭合（实际 API 与设计文档 §2.1 注释的出入） | `FromIR` 保留 `Consistent()` 原语义（EDS/RDS），另以 `checkSDSClosure` 补齐 SDS 闭合检查；「纯函数装配 + 引用闭合自检、失败即 stage=assemble 报错」语义不偏离 | 2026-07-20 | T2 进展记录、internal/deliver/xds/snapshot.go 注释 |
| 幂等跳过时是否重发 `Applied` 事件（§6.1 规则 1 未明确） | 不重发、Status 不变：事件语义是「发生了一次换版受理」，跳过无换版动作，重发会让消费方误判新发布；跳过由 Apply 同步 nil 返回表达 | 2026-07-20 | T3 进展记录、internal/deliver/xds/server.go Apply 注释 |
| 事件 fan-out 慢消费者处置（§6.2 未明确） | 每订阅者容量 16 缓冲通道，非阻塞发送，慢消费者丢弃新事件并计数（dropped + Warn 日志）：事件流是观察通道，权威状态是 Status，不允许阻塞 Apply | 2026-07-20 | T3 进展记录、internal/deliver/xds/server.go |
| ACK 日志级别（任务书要求自定） | Debug 级且仅在 ACK 版本前进时记录（按 type_url 跟踪）：ACK 是每类型每次换版的常态路径，Info 级会产生五倍类型数的常态噪音；NACK 用 Error、误接 node 用 Warn | 2026-07-20 | T3 进展记录、internal/deliver/xds/server.go onStreamRequest 注释 |
| 项目日志方案（此前各包无日志约定） | 引入 stdlib `log/slog`：零新依赖、结构化；go-control-plane `pkg/log.Logger` 经桥接适配到 slog | 2026-07-20 | T3 进展记录、internal/core/serve.go 包注释 |
| 冻结签名 `Events() (<-chan Event, cancel func())` 按字面是 Go 语法错误 | 落地为 `(ch <-chan Event, cancel func())`（结果列表同命名），签名语义不变 | 2026-07-20 | T3 进展记录、commit `3987962` |

## 验收核验（requirements §4 A1~A8）

| # | 标准 | 结论 | 证据 |
|---|---|---|---|
| A1 | FromIR 纯函数 + Consistent 自检 + 反例 | 已核验（T2） | internal/deliver/xds/snapshot.go + snapshot_test.go：正例 TestFromIR_S1（真实 IR、逐类型断言、Consistent 通过）、反例 TestFromIR_Inconsistent（RDS/EDS/SDS 三类不闭合）；commit `bbf1fbd` |
| A2 | esgw serve + 真实 Envoy ADS 全类型拉取、版本=IR.Version | 单测层已核验（T3）；真实 Envoy 层待 T5 e2e | internal/deliver/xds/server_test.go TestApplyServesAllTypes：真实 127.0.0.1:0 端口 + 真实 ADS gRPC client（node.id=esgw-node）拉全 LDS/RDS/CDS/EDS/SDS 五类型，逐类断言 version_info==IR.Version；commit `ccd30e9` |
| A3 | ADS 下 S1 流量四断言 | 待核验 | |
| A4 | static 产物与 ADS 下发 IR.Version 一致（M0 第 3 项闭环） | 待核验 | |
| A5 | ACK 可见 + NACK 单测（不重推） | 已核验（T3 单测层） | internal/deliver/xds/server_test.go TestNACK：ErrorDetail 请求 → Event{Nacked}（Detail 含 type_url+nonce+原文）+ Phase=nacked + 重请求仍收原 version（snapshot 未替换、不自动重推）；ACK 日志路径由 onStreamRequest 覆盖（Debug 级，版本前进才记录）；commit `ccd30e9` |
| A6 | 幂等跳过 | 已核验（T3 单测层） | internal/deliver/xds/server_test.go TestApplyIdempotentSkip：重复 Apply 同 IR 成功、客户端 500ms 内无新推送、Status 不变、无重复 Applied 事件；commit `ccd30e9` |
| A7 | bootstrap 导出被真实 Envoy 接受 + 非 loopback 硬校验 | 待核验 | |
| A8 | CI 全绿、本地可复现 | 待核验 | |

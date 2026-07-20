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
| T3 | Deliverer 接口 + ADS server + ACK/NACK + esgw serve | 未开始 | T1、T2 | A2、A5、A6 |
| T4 | 接入 bootstrap 渲染 + esgw bootstrap 命令 | 未开始 | T1（T3 后可独立做） | A7 |
| T5 | ADS e2e + CI 集成 + 收口验收 | 未开始 | T3、T4 | A2~A4、A5（日志）、A7、A8 |

状态枚举：`未开始` / `进行中` / `已完成` / `受阻(附原因)`

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-20 | 冲刺创建：需求、技术设计、5 个 task 文档就绪；依赖决策 SD1（go-control-plane 根模块 v0.14.0 + envoy v1.37.0）已实测 MVS 兼容；等待 T1 开工 |
| 2026-07-20 | T1 完成：dev_design/260720-1-mcore-assembly.md + internal/config（LoadFile strict decode + defaults + 校验，含非 loopback listen 硬校验）；commit `ad9f052`（docs）、`10bcacb`（config）；`make build test lint` 全绿 |
| 2026-07-20 | T2 完成：internal/deliver/xds FromIR 纯函数装配（Consistent + SDS 闭合补检）+ go-control-plane v0.14.0 依赖引入；commit `7c0fe64`（deps）、`bbf1fbd`（xds）；`make build test lint` 全绿，go-licenses 通过 |

## 决议记录（冲刺内产生的设计决策）

| 未决项 | 决议 | 日期 | 已回写文档 |
|---|---|---|---|
| `deliver.xds.listen` 的 host 为 `localhost` 时是否算 loopback（任务书要求自定取舍） | 按 loopback 接受；其余非 IP 字面量主机名一律拒绝（不做运行期 DNS 解析，保证校验确定性） | 2026-07-20 | dev_design/260720-1-mcore-assembly.md §3.1、T1 进展记录 |
| go-control-plane v0.14 `Snapshot.Consistent()` 不覆盖 SDS 引用闭合（实际 API 与设计文档 §2.1 注释的出入） | `FromIR` 保留 `Consistent()` 原语义（EDS/RDS），另以 `checkSDSClosure` 补齐 SDS 闭合检查；「纯函数装配 + 引用闭合自检、失败即 stage=assemble 报错」语义不偏离 | 2026-07-20 | T2 进展记录、internal/deliver/xds/snapshot.go 注释 |

## 验收核验（requirements §4 A1~A8）

| # | 标准 | 结论 | 证据 |
|---|---|---|---|
| A1 | FromIR 纯函数 + Consistent 自检 + 反例 | 已核验（T2） | internal/deliver/xds/snapshot.go + snapshot_test.go：正例 TestFromIR_S1（真实 IR、逐类型断言、Consistent 通过）、反例 TestFromIR_Inconsistent（RDS/EDS/SDS 三类不闭合）；commit `bbf1fbd` |
| A2 | esgw serve + 真实 Envoy ADS 全类型拉取、版本=IR.Version | 待核验 | |
| A3 | ADS 下 S1 流量四断言 | 待核验 | |
| A4 | static 产物与 ADS 下发 IR.Version 一致（M0 第 3 项闭环） | 待核验 | |
| A5 | ACK 可见 + NACK 单测（不重推） | 待核验 | |
| A6 | 幂等跳过 | 待核验 | |
| A7 | bootstrap 导出被真实 Envoy 接受 + 非 loopback 硬校验 | 待核验 | |
| A8 | CI 全绿、本地可复现 | 待核验 | |

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
| T1 | M-CORE 轻量设计文档 + esgw.yaml schema + internal/config | 未开始 | — | A7（硬校验部分）、G1 |
| T2 | deliver/xds FromIR 纯函数装配 + 依赖引入 | 未开始 | T1（可并行，只依赖 IR 契约） | A1 |
| T3 | Deliverer 接口 + ADS server + ACK/NACK + esgw serve | 未开始 | T1、T2 | A2、A5、A6 |
| T4 | 接入 bootstrap 渲染 + esgw bootstrap 命令 | 未开始 | T1（T3 后可独立做） | A7 |
| T5 | ADS e2e + CI 集成 + 收口验收 | 未开始 | T3、T4 | A2~A4、A5（日志）、A7、A8 |

状态枚举：`未开始` / `进行中` / `已完成` / `受阻(附原因)`

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-20 | 冲刺创建：需求、技术设计、5 个 task 文档就绪；依赖决策 SD1（go-control-plane 根模块 v0.14.0 + envoy v1.37.0）已实测 MVS 兼容；等待 T1 开工 |

## 决议记录（冲刺内产生的设计决策）

| 未决项 | 决议 | 日期 | 已回写文档 |
|---|---|---|---|
| （待产生） | | | |

## 验收核验（requirements §4 A1~A8）

| # | 标准 | 结论 | 证据 |
|---|---|---|---|
| A1 | FromIR 纯函数 + Consistent 自检 + 反例 | 待核验 | |
| A2 | esgw serve + 真实 Envoy ADS 全类型拉取、版本=IR.Version | 待核验 | |
| A3 | ADS 下 S1 流量四断言 | 待核验 | |
| A4 | static 产物与 ADS 下发 IR.Version 一致（M0 第 3 项闭环） | 待核验 | |
| A5 | ACK 可见 + NACK 单测（不重推） | 待核验 | |
| A6 | 幂等跳过 | 待核验 | |
| A7 | bootstrap 导出被真实 Envoy 接受 + 非 loopback 硬校验 | 待核验 | |
| A8 | CI 全绿、本地可复现 | 待核验 | |

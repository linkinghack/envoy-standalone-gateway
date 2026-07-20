# Sprint 260719 需求（M0：协议与编译器）

- **冲刺编号**: 260719
- **里程碑**: M0 概念验证（[路线图](../../260719-1-dev-roadmap.md) S1）
- **状态**: 已完成（待验收）——T1~T7 全部完成，A1~A8 逐项核验均为「达成」，核验表与证据位置见 [plan_todos_trace.md](plan_todos_trace.md)「验收核验」节（2026-07-20 T7 收口）；A8 远端 CI 绿以推送后 GitHub Actions 实际运行确认为准

---

## 1. 要解决的问题

本项目最大设计风险是抽象协议表达力（RK1）。M0 的目的就是**用真实代码验证协议 v0**：把 [`260716-1-gateway-config-protocol-v0.md`](../../../system_design/260716-1-gateway-config-protocol-v0.md) 定义的五对象协议实现为 Go 类型与编译器，产出能被真实 Envoy 加载并承载流量的 static 配置。同时交付整个项目的工程基线（仓库结构、CI、测试设施），后续全部冲刺在其上叠加。

## 2. 冲刺目标

| # | 目标 | 对应设计 |
|---|---|---|
| G1 | 工程基线：Go module、目录结构、Makefile、lint、CI（build/test/lint + envoy validate 矩阵） | [工程基线](../../dev_design/260719-1-engineering-baseline.md) |
| G2 | `internal/protocol`：信封 + 五对象 + EnvoyResources 的 Go 类型、strict decode、目录加载（含 Origin）、defaults、JSON Schema 生成 | 协议 v0 全文 |
| G3 | `internal/compile`：F1~F6 编译流水线（链接校验、Builder、escape hatch 合成、形态化、PGV 校验）、CompileError 错误模型、IR 与确定性版本哈希、SourceMap | [编译层](../../../system_design/260716-2-compile-ir-design.md) |
| G4 | static 渲染（`deliver/static.Render` 纯函数）+ F7 envoycheck + `esgw compile` CLI | 编译层 §6、下发层 §4.1 |
| G5 | 测试设施：golden file 快照、错误用例表驱动、多版本 envoy validate、S1 场景真实流量 e2e 冒烟 | 编译层 §8 |

## 3. 范围外（本冲刺不做）

- xDS server 与 ADS e2e（S2 冲刺；M0 验收第 3 项"ADS 拉起同一 IR"按可行性 §6.3 后置到 S2 初）
- 原生 static → IR 解析入口（FR-2.1，随 S3 配置域冲刺）
- 任何服务端常驻进程、API、UI、存储
- L4（TCP/UDP）与 P1 策略中的 `extAuth`/`basicAuth`/`ipAccess`（协议标注 P1/P2；本冲刺实现 P0 必需 + S2 场景所需的 `headerModifier`/`cors`/`rateLimit`/`jwt`）

## 4. 验收标准

| # | 标准 | 验证方式 |
|---|---|---|
| A1 | 协议文档 §8.1（S1 多域名 TLS 反代）与 §8.2（S2 API 网关）两组 YAML 原文可被编译，产出 static YAML | golden file 测试 |
| A2 | S1/S2 产物通过 `envoy --mode validate`（支持区间内 ≥2 个 Envoy minor 版本） | CI 矩阵 |
| A3 | S1 产物由 docker 中真实 Envoy 加载，curl 断言 TLS 终止 + 域名分流 + HTTP→HTTPS 跳转全部正确 | e2e 冒烟脚本 |
| A4 | escape hatch 两种形态（对象级 `envoyPatch`、顶层 `EnvoyResources`）各有用例，合成正确且坏 patch 被校验拦截 | golden + 错误用例 |
| A5 | 编译层 F2 每条语义规则至少一个反例，且所有错误带 SourceRef（file/kind/name/path） | 表驱动测试 |
| A6 | 同一输入两次编译产物字节级一致（确定性 §5）；`IR.Version` 稳定 | 测试断言 |
| A7 | `esgw compile -f <dir> --mode static -o envoy.yaml` 可用；`--mode xds` 输出 snapshot protojson | CLI 集成测试 |
| A8 | CI 全绿；`make test`/`make lint`/`make e2e` 本地可复现 | CI |

## 5. 本冲刺内需落地的设计未决项

| 未决项 | 内容 | 落地 task |
|---|---|---|
| C1 | `envoyPatch` target 取值全集与 per-rule 定位（倾向 rule 加可选 `name` 字段） | T5 |
| C3 | JSON Schema 生成工具（倾向 invopop/jsonschema） | T2 |
| C4 | jwt providers 聚合去重 key 规则 | T4 |

决议结果回写对应设计文档的未决事项表，并在 `plan_todos_trace.md` 记录。

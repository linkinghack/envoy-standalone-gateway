# Sprint 260719 任务拆分与进展追踪

> **给接手会话的指引（Codex / Claude Code 必读）**
> 1. 先读本文件确定当前进行到哪个 task；再读 `tasks/<ID>.md` 获取该 task 的详细步骤与已记录进展。
> 2. 开工前把对应 task 状态改为 `进行中`，完成后改为 `已完成` 并填写完成摘要；**每次会话结束前必须回写进展**（做到哪一步、遇到什么问题、下一步是什么）。
> 3. 遵守 [`technical_design.md`](technical_design.md) §3 的接口冻结点；需要变更接口时先更新该文件并在此处记录。
> 4. 每个 task 至少一个独立 commit，提交信息格式见[工程基线](../../dev_design/260719-1-engineering-baseline.md) §5。
> 5. 冲刺内设计决议（C1/C3/C4）落地后，回写上游设计文档的未决事项表。

## 任务总览

| ID | 任务 | 状态 | 依赖 | 验收锚点 |
|---|---|---|---|---|
| T1 | 仓库工程基线（module/目录/Makefile/lint/CI） | 已完成 | — | A8 |
| T2 | internal/protocol：类型、strict decode、loader、defaults、JSON Schema | 已完成 | T1 | A1(输入侧)、C3 |
| T3 | internal/compile F2：链接与语义校验 + CompileError 模型 | 已完成 | T2 | A5 |
| T4 | internal/compile F3：四类 Builder 与策略映射 | 已完成 | T3 | A1、C4 |
| T5 | internal/compile F4/F5/F6 + IR/哈希/SourceMap | 已完成 | T4 | A4、A6、C1 |
| T6 | static 渲染 + F7 envoycheck + `esgw compile` CLI | 已完成 | T5 | A2、A7 |
| T7 | golden/e2e 测试设施与 CI 集成收口 | 已完成 | T6（golden 可提前随 T4/T5 增量补） | A1~A3、A8 |

状态枚举：`未开始` / `进行中` / `已完成` / `受阻(附原因)`

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-19 | 冲刺创建：需求、技术设计、7 个 task 文档就绪；等待 T1 开工 |
| 2026-07-19 | T1 完成（commit `2adb7b1`）：Go module/骨架/Makefile/golangci-lint/CI/go-licenses；Envoy 支持区间定 1.37~1.39；T2 开工 |
| 2026-07-19 | T2 完成（commits `6b76692`/`39facc0`/`4f0ba48`/`2b4e4de`）：internal/protocol 全部类型+strict decode loader+defaults+JSON Schema；57 用例全绿；C3 决议落地（见决议记录与 T2 进展记录，含 SD2 YAML 库偏离说明）；T3 开工 |
| 2026-07-19 | T3 完成（commits `86333d5`/`2b3714a`/`feda68a`）：CompileError/SourceRef 错误模型、F2 链接与语义校验全规则、Compile() 骨架（F3+ 占位）、EndpointSource 接口定义；33 用例全绿，S1/S2 过 F2 零错误；ipAccess 层级等 6 条设计澄清见 T3 进展记录；T4 开工 |
| 2026-07-19 | T4 完成（commits `a9d354b`/`40d2705`/`79aa56b`）：go-control-plane/envoy v1.37.0 依赖、F3 四类 Builder（Listener/Route/Upstream/Policy）与策略归一化、C4 决议落地；S1/S2 主干 + 各 Builder 边界例全绿，两次构建 protojson 逐字节相同（确定性锁定），产物全量过 PGV 自检；rateLimit.key 等 11 条实现决策/偏离见 T4 进展记录；T5 开工 |
| 2026-07-19 | T5 完成（commits `a649447`/`eda144b`/`e1b0cdf`/`bc0b5e6`/`191f708`）：internal/ir 落地（IR/SourceMap/确定性哈希）、F4 escape hatch 合成（C1 决议落地：rule 可选 name + target 定表）、F5 两模式形态化、F6 PGV+引用闭合，Compile() F1~F6 全流水线打通；§7.1/§7.2 逐字正例、7 类坏 patch 反例、两模式对拍、确定性与 300 轮 patch 模糊测试全绿；SourceRef 移入 ir 等 10 条实现决策见 T5 进展记录；T6 开工 |
| 2026-07-20 | T6 完成（commits `ebfbfa7`/`c98b540`/`1ef55de`）：internal/deliver/static.Render 纯函数落地（文件头版本注释、node.metadata 渲染期注入 esgw.config_version、admin 覆盖 M0 默认 uds /tmp/esgw-admin.sock、protojson→map→确定性 YAML 发射器）；deliver.SnapshotJSON 定 xds 产物形态；envoycheck 封装 F7（发现顺序 显式路径→ESGW_ENVOY_PATH→PATH、超时与 stderr 尾部捕获、无二进制降级 Warning）；`esgw compile -f <dir> --mode static|xds -o <file> [--envoy-validate[=<path>]]` 交付，F7 由 CLI 层编排（Compile 保持纯函数），错误按 `error|warning: file:kind/name:path: message` 逐行输出、Error exit 1 / 仅 Warning exit 0 / 用法 exit 2；渲染/CLI 集成测试全绿，make build/test/lint 全绿；admin uds 路径取值等 6 条实现决策/偏离见 T6 进展记录；T7 开工 |
| 2026-07-20 | T7 完成（commits `fe005b2`/`ea53b09`/`ebe2f94`/`28c2f32`/`b03b85c`/`fc2e218`）：golden 体系落地（6 正例 + 6 错误例，`internal/golden` + `-update`）；测试证书 `testdata/certs/`（自签 CA + 4 证书 + 生成脚本）；validate 矩阵 `scripts/validate-matrix.sh`（版本同源 internal/version 常量，本地 1.37.5/1.38.3/1.39.0 × 6 产物全过）；S1 e2e（docker compose + 四断言全过）；CI 启用 validate-matrix/e2e job。**测试设施捕获两个真实编译器缺陷并修复**：① anypb.New 非确定性内层序列化致 IR.Version 抖动（A6 红线，marshalAny 统一替换）；② HTTPS listener 缺 tls_inspector 致 SNI 分流全灭（仅真实流量可暴露），并补 HCM strip_any_host_port。决策/偏离（chdir 约定、sed 弥合容器路径、compose 固定 IP、CI 单 job 循环矩阵）见 T7 进展记录；A1~A8 逐项核验见下表，冲刺收口 |

## 决议记录（冲刺内产生的设计决策）

| 未决项 | 决议 | 日期 | 已回写文档 |
|---|---|---|---|
| C1 | **已决议（2026-07-19，Sprint 260719 T5）**：target 按对象 kind 定表——Listener→`listener`/`secret`；Route→`virtualHost`/`route`（rule 级写作 `route/<ruleName>`）；Upstream→`cluster`/`endpoints`；Gateway→`bootstrap` 不允许（C2 留 M1）。rule 级定位采用给 rule 加可选 `name` 字段（Route 内唯一、字符集同 metadata.name）；无 `name` 的 rule 不可被 rule 级 patch 定位。实现见 `internal/compile/patch.go` 与 `internal/protocol/route.go` | 2026-07-19 | plan_todos_trace（本表）、protocol §3.3 要点 4 + §7.1、compile-ir-design §9、T5 进展记录 |
| C3 | 采用 invopop/jsonschema 由 Go 类型生成：协议 §6 要求「Schema 由协议 Go 类型生成，单一事实来源」，反射输出对枚举/oneOf/Duration 经 `JSONSchema()` 类型钩子（PolicyAttachment union、Duration pattern、RawJSON 任意、枚举值）与后置注入（每文档 apiVersion/kind 常量、name pattern）后质量达标，bundle = oneOf 六文档 + 共享 $defs；用 santhosh-tekuri/jsonschema/v6 测试加载与正/反校验。手写方案被淘汰：双份真源难维护 | 2026-07-19 | plan_todos_trace（本表）、compile-ir-design §9、T2 进展记录 |
| C4 | **已决议（2026-07-19，Sprint 260719 T4）**：去重键 = `issuer + "|" + 规范化 jwks 来源`（`uri:` + TrimSpace(uri) 或 `file:` + file）；audiences 不参与去重——同 issuer+jwks 视为同一 provider，不同受众经 requirement_map 的 `provider_and_audiences` 表达。provider 名内容寻址 `jwt/<sha256(key) 前 8 位 hex>`（与收集顺序无关）；`jwt.optional=true` → requirement `allow-missing`。实现见 `internal/compile/build_policy.go` | 2026-07-19 | plan_todos_trace（本表）、compile-ir-design §9、T4 进展记录 |

## 验收核验（M0，requirements §4 A1~A8，2026-07-20 T7 收口）

| # | 标准 | 结论 | 证据 |
|---|---|---|---|
| A1 | 协议 §8.1/§8.2 YAML 原文可编译产出 static YAML | ✅ 达成 | golden 用例 `testdata/s1`（§8.1 原文仅替换证书路径，替换表见 s1/README）、`testdata/s2`（§8.2 原文+同构补全）；`go test ./internal/golden` 全绿 |
| A2 | S1/S2 产物过 `envoy --mode validate`（≥2 个 minor） | ✅ 达成 | `make validate-matrix` 本地实测 1.37.5/1.38.3/1.39.0（3 个 minor）× 6 产物全 OK；CI `validate-matrix` job 同源启用 |
| A3 | S1 产物真实流量：TLS 终止 + 域名分流 + 301 跳转 | ✅ 达成 | `make e2e` 四断言全过：www→www-backend、blog→blog-backend、http→301 https、SNI 集合外域名握手失败 |
| A4 | escape hatch 两形态有用例，合成正确且坏 patch 被拦截 | ✅ 达成 | golden 正例 `patch-merge`/`patch-jsonpatch`（§7.1）、`envoy-resources`/`envoy-resources-override`（§7.2 两态）；反例 `testdata/errors/patch-bad-jsonpatch`、`envoyresources-conflict`（patch 阶段拦截并回指位置） |
| A5 | F2 每条语义规则至少一个反例，错误均带 SourceRef | ✅ 达成 | T3 表驱动（`internal/compile/link_test.go`）+ 6 个错误 golden（`testdata/errors/*/want-errors.json` 每条含 file/kind/name/path） |
| A6 | 同一输入两次编译字节级一致；IR.Version 稳定 | ✅ 达成（T7 修复一个红线缺陷后） | `internal/compile/determinism_test.go` + golden 连续 3 轮无 diff；T7 修复 anypb.New 内层非确定性序列化导致的 IR.Version 抖动（commit `fe005b2`，修复后 s2 digest 15 轮进程级一致） |
| A7 | `esgw compile --mode static/xds` 可用 | ✅ 达成 | `cmd/esgw/compile_test.go` 集成测试 + 手工验证：`bin/esgw compile -f testdata/s1/input`（static/xds）产物与 golden 字节级一致 |
| A8 | CI 全绿；make test/lint/e2e 本地可复现 | ✅ 达成（本地全绿；远端 CI 四 job 已确认全绿） | 本地 `make build/test/lint` 全绿（golangci-lint 0 issues）、`make e2e`、`make validate-matrix` 通过；推送后修复 licenses job（go-licenses 误判项目本体 AGPL-3.0 为 forbidden 依赖，`--ignore` 排除本体，commit `880ab4f`），GitHub Actions run 29714180533 四个 job（build-test-lint/licenses/validate-matrix/e2e）全部 success |

未达项：无。

## 冲刺关闭（2026-07-20）

- 主会话独立复验：`make validate-matrix`（1.37.5/1.38.3/1.39.0 × 6 产物全 OK）与 `make e2e`（四断言全过）二次确认通过；`make build/test/lint` 全绿。
- 索引与路线图已更新：sprints/README 状态「已完成（待验收）」，260719-1-dev-roadmap S1 行标记完成。
- M0 验收第 3 项「同一 IR 经 ADS 正常拉起」按既定规划留 S2 冲刺闭环（requirements §3 范围外）。
- 下一冲刺：S2（xDS 下发与运行时骨架），依赖设计 [260717-1 §1~§3、§6](../../../system_design/260717-1-deliver-layer-design.md)，另需补 M-CORE 装配轻量设计与 esgw.yaml schema（路线图 §1 小缺口）。

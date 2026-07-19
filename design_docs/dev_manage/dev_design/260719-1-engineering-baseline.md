# 260719-1 工程基线与开发规范（Engineering Baseline）

- **性质**: 沉淀性规范（dev_design），随工程演进更新；由 Sprint 260719 / T1 首次落地
- **日期**: 2026-07-19

---

## 1. 仓库布局

```
cmd/esgw/                 # 唯一二进制入口；子命令随冲刺增加（compile → serve/bootstrap → ...）
internal/protocol/        # 协议类型与加载（架构模块映射见下表）
internal/compile/         # M-COMPILE（envoycheck 为独立子包）
internal/ir/              # IR 类型与哈希
internal/deliver/         # M-DELIVER（static/ xds/），M1 起扩展
internal/conf/ internal/store/ internal/state/ internal/proc/ internal/disco/ internal/api/ internal/core/
                          # 对应架构模块，随冲刺创建，不预建空壳
internal/version/         # 版本、二进制名（esgw，D1 未决时的唯一改名点）、Envoy 支持区间常量
api/                      # openapi.yaml（S5 起）
web/                      # 前端独立 npm 工程（S6 起）
testdata/  e2e/           # 测试资产
design_docs/              # 设计与研发管理文档（已有）
```

模块 → 包映射与依赖单向性（架构 §3）：`api → conf → compile → deliver`；`state/disco` 旁路；**CI import 约束**：`compile/deliver/conf` 禁止 import `client-go`（disco 专属）、编译器主体禁止 import envoycheck。

## 2. 工具链

| 项 | 约定 |
|---|---|
| Go | ≥ 1.22；`gofumpt` 格式化 |
| Lint | golangci-lint（govet/staticcheck/errcheck/revive/gofumpt） |
| 核心依赖 | go-control-plane（xDS/proto）、sigs.k8s.io/yaml、modernc.org/sqlite（S3 起）；新增依赖需在 PR/commit 说明理由，AGPL 兼容性经 `go-licenses` CI 检查 |
| Make 目标 | `build / test / lint / fmt / e2e / golden-update / validate-matrix` |
| CI | GitHub Actions：build+test+lint 必须；envoy validate 矩阵与 e2e 独立 job |

## 3. 测试约定

- 单测与被测代码同包同目录；表驱动优先；golden file 用 `-update` flag 刷新且 diff 必须人工/评审确认。
- 确定性是硬约束：任何产物生成路径禁止时间/随机/环境读取（编译层 §5），发现即 bug。
- e2e 依赖 docker，标记 build tag `e2e`，默认 `go test ./...` 不触发。

## 4. AI 工程师工作流（Codex / Claude Code）

1. 入口永远是当前冲刺的 `plan_todos_trace.md` → 领取 task → 读 task 文档与其引用的设计文档节。
2. 设计文档是规格真源：实现与设计冲突时**不得静默偏离**，在 task 进展记录中写明并提出修订建议。
3. 每次会话结束回写 task 进展（完成内容/问题/下一步），保证任意新会话可无损接续。
4. 冲刺内决议（未决项落地）必须回写上游设计文档的未决事项表。

## 5. git 约定

- 小步提交，每 task ≥1 个独立 commit；禁止巨型 commit（AGENTS.md）。
- Commit message：`<scope>: <动词开头摘要>`，scope 取包名或 task ID（如 `protocol: add strict envelope decoding (T2)`）；正文可写决策理由。
- 依赖升级（尤其 go-control-plane）单独 commit，附 golden 快照刷新。

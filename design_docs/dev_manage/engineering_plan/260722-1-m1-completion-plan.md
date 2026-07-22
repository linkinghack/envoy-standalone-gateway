# 260722-1 M1 剩余工程实施计划（S5–S8）

- **状态**：已完成（2026-07-22，S5–S8 全部验收，M1 收口）
- **范围**：在 S1–S4 已完成的协议、编译、xDS、配置域和状态域之上，完成管理 API、P0 控制台、static/进程托管与 M1 交付物。
- **上游**：[`../260719-1-dev-roadmap.md`](../260719-1-dev-roadmap.md)、[`../../system_design/260717-3-console-api-design.md`](../../system_design/260717-3-console-api-design.md)、[`../../system_design/260717-1-deliver-layer-design.md`](../../system_design/260717-1-deliver-layer-design.md)

## 1. 为什么需要本计划

system_design 已足以说明各模块目标，但 S5–S8 同时跨越 HTTP 契约、会话安全、文件真源写入、前端构建、进程生命周期与发行工程；制定本计划前，AD1/AD2/AD8 尚未终审。若直接逐端点实现，容易形成 API 与 UI 双写、M-CORE 多套启动路径和交付拓扑不一致。因此补充本工程计划，终审关键决策并冻结跨 Sprint 的装配顺序与质量门禁；每个 Sprint 的详细设计仍写入各自 `technical_design.md`。

## 2. 终审决策

| 决策 | 结论 | 理由/约束 |
|---|---|---|
| AD1 前端 | React + TypeScript + Vite；shadcn/ui；TanStack Query；YAML 编辑采用 Monaco + monaco-yaml | 终审采用 shadcn/ui 的可组合、可定制组件体系，并通过 design tokens 统一视觉规范；领域状态不放入 UI 组件层 |
| AD2 API | OpenAPI 3.0.3 spec-first；`api/openapi.yaml` 是契约真源；固定 oapi-codegen v2.6.0，提交生成的 Go boundary types/server interface，并以 `go generate` + clean-diff 作一致性门禁 | oapi-codegen 稳定线对 3.0 支持成熟，3.1 支持仍在演进；handler 不自定义平行 DTO；生成器仅是构建工具 |
| HTTP 路由 | Go 1.26 `net/http.ServeMux` 方法/通配符路由 + 标准中间件 | P0 路由规模可控，无需为路由引入框架；减少供应链和二进制体积 |
| 会话安全 | Argon2id（64MiB/t=3/p=2）、服务端 session、HttpOnly/SameSite=Lax cookie、写请求 `X-ESGW-Request: 1`、无 CORS | 严格执行控制台 API 设计 §4 |
| AD8 监听 | 裸机默认 `127.0.0.1:8080`；容器显式 `0.0.0.0:8080`；非 loopback 引导态强警告；支持 `ESGW_INITIAL_ADMIN_PASSWORD` | 安全默认与容器可达性均由明确配置表达，不暗猜部署环境 |
| AD5 回滚 | API 默认 `publish=false`；直接重发布必须显式 `publish=true`，覆盖未发布草稿必须 `force=true` | 默认保留人工检查机会，沿用配置域已实现的保护语义 |
| AD7 证书 | P0 同时保留文件路径与托管 `ref:`；托管私钥文件权限 0600，API 永不返回私钥；静态加密密钥接入推迟到 M1 收口前风险复核 | 不破坏现有协议；满足“只进不出”，并明确 NFR-5 剩余风险 |

## 3. 装配边界

只保留一个生产启动组合根 `internal/core.App`：负责配置加载、Store、Conf、Deliverer、State、API、Proc 的构造和反向顺序关闭。领域包不 import API，API 仅依赖小接口；前端只调用 OpenAPI 中的端点。

启动阶段固定为：

1. 加载/校验 `esgw.yaml`，创建数据目录；
2. 打开 SQLite 并执行 migration；
3. 初始化 deliver/state/conf，恢复最后有效版本；
4. 按配置决定启动外部 Envoy 连接或 M-PROC；
5. 启动管理 HTTP server，最后宣告 ready；
6. 关停时先停止接收 HTTP 写请求，再停 collector/xDS/proc，最后关闭 Store。

## 4. Sprint 交付顺序

### S5（260723）：管理 API

- T1：OpenAPI 契约、生成边界、契约一致性门禁；
- T2：Store 用户/session migration 与 auth service（bootstrap/login/logout/password/session）；
- T3：API server 通用错误、鉴权、CSRF、限流、health/ready、SPA fallback；
- T4：配置 draft/object/schema/validate/compiled/diff/publish/status/version/rollback 端点；
- T5：state/stats/system/Prometheus 端点与证书库；
- T6：M-CORE 生产装配、API e2e 与 Sprint 收口。

验收锚点：OpenAPI 中全部 P0 操作有实现；无会话不可读取管理数据；写请求缺 CSRF 头被拒绝；首次引导不可重复；私钥不出 API；配置发布可由 HTTP 走到确认状态；`/api/*` 404 不返回 SPA。

### S6（260724）：控制台 P0

- T1：web 工程基线、design tokens、响应式 shell、登录/引导；
- T2：Dashboard、配置对象列表与表单/YAML 编辑；
- T3：校验/发布/状态闭环、运行状态视图；
- T4：证书库、专家模式、编译产物、系统信息；
- T5：可访问性/错误态/空态/窄屏审计、Go embed 和浏览器 e2e。

验收锚点：P0 用户无需命令行可完成首次引导、创建配置、校验、发布并查看 Envoy 实际状态；键盘可完成主路径；前端生成客户端与 OpenAPI 一致；单二进制可服务 SPA。

### S7（260725）：static 与 M-PROC

- T1：进程发现/版本校验、外部进程与 pid/epoch 模型；
- T2：spawn/接管、信号、退出分类、指数退避和稳定窗口；
- T3：static 原子落盘与 hot restart epoch 协调；
- T4：托管/仅下发两组合恢复语义、状态确认与故障测试；
- T5：真实 Envoy static hot restart e2e 与收口。

验收锚点：管理面重启/崩溃不杀死已服务的数据面；坏配置不替换最后有效文件；连续崩溃受退避控制；仅下发模式不越权管理外部进程。

### S8（260726）：交付与 M1 收口

- T1：systemd unit、安装/升级/卸载脚本与目录权限；
- T2：esgw 与 all-in-one 镜像、compose 示例、健康检查；
- T3：T1/T2 拓扑 × xDS/static 跨模块 e2e；
- T4：用户快速开始、运维/备份/恢复/升级/安全文档；
- T5：M1 需求矩阵、资源目标、许可证/SBOM、发布产物与最终收口。

验收锚点：从空主机/空 Docker 环境在 10 分钟内完成首个代理；四组合 e2e；管理面停止后最后有效配置继续服务；M1 每条 P0 需求有直接证据。

## 5. 跨 Sprint 质量门禁

每个 task 至少独立提交一次，task 文档实时记录。每个 Sprint 收口至少执行：

```text
gofumpt -w .
make build
go test ./...
go test -race ./...
go vet ./...
golangci-lint run ./...
```

涉及 OpenAPI 时追加 spec lint、生成 clean-diff 和 HTTP contract tests；涉及前端时追加 typecheck/unit/build/browser smoke；涉及 Envoy 时追加对应真实 e2e 与 validate matrix。执行环境无法运行某门禁时只能记录“待补证”，不得写成通过。

## 6. 非 M1 边界

S9+（M2）仍承载完整统计 UI、版本历史/回滚 UI 增强、k8s disco/Helm、协议对外规范和 P1 策略。S5 可以交付支撑 P0 页面的有限 stats/config version API，但不得借此宣称 M2 完成。

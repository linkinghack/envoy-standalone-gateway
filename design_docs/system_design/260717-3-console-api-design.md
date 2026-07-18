# 260717-3 控制台信息架构与 API 契约（M-API / Console Design）

- **项目**: envoy-standalone-gateway
- **上游文档**: [`260715-2-overall-architecture.md`](260715-2-overall-architecture.md)（M-API 模块、§2 选型、RK5/RK6）、[`260716-1-gateway-config-protocol-v0.md`](260716-1-gateway-config-protocol-v0.md)（对象规格、证书 `ref:` 形态）、[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md)（CompileError 错误模型、SourceMap、确定性、M-API 契约）
- **文档状态**: 初稿（Draft）
- **日期**: 2026-07-17
- **性质**: 模块详细设计。覆盖架构 §8 清单第 5 项：控制台信息架构（IA）、REST API 契约与 OpenAPI 策略、鉴权与会话、静态资源服务与前端工程组织。边界约定：草稿/版本/发布状态机的**语义**归 [`260717-2-config-domain-design.md`](260717-2-config-domain-design.md)，下发行为归 [`260717-1-deliver-layer-design.md`](260717-1-deliver-layer-design.md)，status/stats 数据模型归 [`260717-4-state-collection-design.md`](260717-4-state-collection-design.md)，k8s 候选数据归 [`260717-5-k8s-disco-design.md`](260717-5-k8s-disco-design.md)；本文只定义 HTTP 契约、页面结构、鉴权与前端组织。

---

## 1. 范围与原则

| # | 原则 | 含义与推论 |
|---|---|---|
| U1 | **API 即产品契约** | 控制台、CLI（远期）、第三方脚本同为 API 消费者（架构 §2"控制台与 CLI 共用同一 API"）；UI 的任何能力必须可由纯 API 完成，禁止 UI 专用后门端点。推论：API 形态一次性定稿、按契约评审，避免 UI 先行导致的接口腐烂 |
| U2 | **P0 范围克制（RK6）** | 首屏交付：配置管理（五对象 CRUD + 表单/只读 YAML）+ 运行状态视图（FR-4.3）+ 简版发布流（校验 → 发布 → 生效状态）；diff 预览、版本历史/回滚（FR-3.4）、统计（FR-4.4）、表单↔YAML 完整双向（FR-4.2）、OIDC 按需求优先级排期。页面结构一次到位，P0 页面内功能分期点亮 |
| U3 | **语义归领域文档，本文只管 HTTP 契约** | 本文不复述发布状态机、下发模式、状态采集模型的内部语义，只定义路径、表示（representation）、错误与鉴权；相邻文档语义变更时 API 形态尽量稳定 |
| U4 | **安全默认** | 除登录/引导/健康检查外全部端点要求会话；**不提供到 Envoy admin API 的透明代理**（RK5：admin 面不间接暴露，FR-4.3 全部由归一化 status 端点覆盖）；证书私钥只进不出（NFR-5）；CORS 默认全禁 |
| U5 | **表单不丢配置** | UI 对任何合法协议 YAML 都能无损展示：无法结构化表达的字段（escape hatch 等）在表单中降级为只读保留，而非吞掉（详见 §6.2） |
| U6 | **复用编译层确定性** | diff、变更预览、编译产物导出全部基于编译层确定性渲染（[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §5），UI 侧不做二次序列化，保证"看到的就是要下发的" |

## 2. 信息架构（IA）

### 2.1 导航树

```
├── 概览 Dashboard
├── 配置
│   ├── Gateway（全局设置）
│   ├── Listener
│   ├── Route
│   ├── Upstream
│   └── Policy
├── 发布中心
│   ├── 变更预览（diff + 校验结果）
│   ├── 版本历史（diff / 回滚）
│   └── 发布状态（生效确认）
├── 运行状态
│   ├── Listener
│   ├── Cluster 与 Endpoint
│   ├── Route
│   └── 证书
├── 统计
└── 系统
    ├── 账号
    ├── 证书库
    ├── 专家模式（原生 static）
    ├── 编译产物
    └── 系统信息
```

一级入口与需求 §3.2 场景的对应：S1/S2 用户的日常路径是"配置 → 发布中心 → 运行状态"；S5 专家走"系统 → 专家模式/编译产物"；S6 排障走"运行状态/统计"。导航不暴露 Envoy 概念层级（filter chain/HCM 等不出现在 IA 中），运行状态各页虽按 Envoy 资源命名，但那是" Envoy 真实生效状态"的刻意直译（FR-4.3 的诉求就是对照真实）。

### 2.2 页面清单

| 页面 | 内容与关键交互 | 需求 | 主要 API | 里程碑 |
|---|---|---|---|---|
| 概览 Dashboard | 发布/生效状态卡（published vs effective）、对象计数、Endpoint 健康比例、待办提示（草稿有未发布变更/校验失败）；流量摘要卡 | FR-4.3/4.4/4.5 | `config/status`、`status/summary`、`stats/overview` | P0 简版；流量卡 P1 |
| 配置：对象列表页（×5） | 表格（name、kind 摘要列、labels、操作）；新建（表单或 YAML 起步）；删除前引用检查提示 | FR-4.1 | `config/objects?kind=` | P0 |
| 配置：对象详情/编辑页 | **表单视图 ↔ YAML 源码视图**（§6.2）；行内校验标注（SourceRef → Monaco marker）；escape hatch 使用提示 | FR-4.1/4.2、FR-1.4 | `config/objects/{kind}/{name}`、`config/schemas` | P0：表单 + YAML 编辑；P1：完整双向体验（FR-4.2） |
| 配置：Upstream 编辑增强 | k8s 环境下表单内嵌 Service 候选搜索选择、选中自动填充（`kubernetesService` 形态） | FR-6.2 | `disco/k8s/services` | P1 |
| 发布中心：变更预览 | 草稿 vs 当前版本的 unified diff（side-by-side）+ 对象级增删改摘要徽标；validate 结果列表（点击跳到 YAML 对应行）；发布（填 message） | FR-4.5、FR-3.3 | `config/draft/diff`、`config/validate`、`config/publish` | P1（FR-4.5 为 P1） |
| 发布中心：版本历史 | 版本列表（id/作者/时间/message/生效标记）；任意两版 diff；回滚 | FR-3.4 | `config/versions/*` | P1 |
| 发布中心：发布状态 | published 与 effective 版本对照、生效确认时间、下发模式与状态；static 模式显示 hot restart 协调状态 | FR-4.5 | `config/status` | P0（简版发布流：校验 → 发布 → 看状态） |
| 运行状态（×5 视图） | 当前生效的 listeners/clusters/endpoints(含健康)/routes/certs 表格 + 详情抽屉（归一化 JSON 原文） | FR-4.3 | `status/*` | P0 |
| 统计 | 概览（RPS、成功率、延迟分位、连接数，时间序列）+ 按 route/cluster 维度表 | FR-4.4 | `stats/*` | P1 |
| 系统：账号 | 修改密码、当前会话信息；多用户管理预留 | FR-4.6 | `auth/*` | P0 |
| 系统：证书库 | 上传 PEM 证书+私钥、列表（SAN/过期时间/被引用 Listener）、删除（被引用则拦截） | FR-1.2（证书托管 `ref:`） | `certs` | P0 |
| 系统：专家模式 | 整份原生 static YAML 编辑（Monaco）→ 草稿 `sourceType=native`；与抽象协议混用经 `EnvoyResources` 对象 | FR-2.1/2.2 | `config/draft`、`config/validate` | P0 |
| 系统：编译产物 | 按协议对象分组的 IR 树（SourceMap）；protojson 查看；导出 static YAML / xDS snapshot JSON | FR-2.3 | `config/compiled`、`config/versions/{id}/compiled` | P0 |
| 系统：系统信息 | esgw 版本、Envoy 版本与拓扑（T1/T2/T3）、下发模式、构建信息 | FR-5.x | `system/info` | P0 |

P0 合计 8 类页面，符合 RK6 的克制要求；P1 项在 IA 中已占位，避免后续导航重构。

## 3. REST API 契约

### 3.1 风格约定

| 约定 | 规则 |
|---|---|
| 前缀 | 全部管理 API 挂在 `/api/v1` 下；`/healthz`、`/readyz`、`/metrics` 在根路径（运维惯例，不带鉴权前缀） |
| 表示 | REST + JSON（`application/json; charset=utf-8`）为规范表示；配置对象与草稿端点额外支持 YAML 表示（`Accept: application/yaml` / `Content-Type: application/yaml`），供 CLI 与粘贴工作流，JSON 为唯一规范形态、YAML 为等价投影 |
| 资源语义 | `/api/v1/config/**` 默认操作**草稿工作副本**；已发布内容不可变，只读经 `config/versions/**` 访问 |
| 命名 | 路径全小写复数名词；`kind` 路径参数用协议原名（`Gateway`/`Listener`/`Route`/`Upstream`/`Policy`/`EnvoyResources`，大小写敏感）；`name` 规则同协议信封 §2.1 |
| 时间 | RFC3339 UTC 字符串 |
| 幂等 | `PUT objects/{kind}/{name}` 为 upsert（幂等）；重复发布无变更草稿返回 `409 NO_CHANGES`（不产生空版本，复用编译层"配置未变则不触发下发"判断） |
| 乐观并发 | 草稿带单调递增 `resourceVersion`；对象写操作可携带 `expectedResourceVersion`，不匹配返回 `409 CONFLICT` 由调用方重取——防止两个会话互相覆盖草稿 |
| 分页/过滤 | 列表端点统一 `?limit=`（默认 100，上限 1000）+ `?offset=`，响应包络 `{"items": [...], "total": n}`；版本列表默认 `-createdAt` 排序；配置对象、status 等有界列表接受参数但通常一页返回，包络保持一致 |
| 写操作通用 | 变更草稿的响应携带新的 `draftResourceVersion`，便于前端维持并发基线 |

### 3.2 统一错误模型

所有错误响应同一信封；`details` 仅在校验类错误时出现，元素即编译层 `CompileError` 的 JSON 投影（stage/source/message/severity，见 [`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §4），前端据此做行内标注：

```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "草稿编译失败：1 个错误",
    "details": [
      {
        "stage": "link",
        "source": {"file": "route.yaml", "kind": "Route", "name": "api",
                   "path": "spec.rules[0].backends[0].upstream"},
        "message": "引用的 upstream \"user-svc\" 不存在",
        "severity": "error"
      }
    ]
  }
}
```

| HTTP | code | 场景 |
|---|---|---|
| 400 | `INVALID_ARGUMENT` | 请求体解析失败、参数非法、YAML 语法错误、信封字段非法 |
| 401 | `UNAUTHENTICATED` | 未登录、会话过期 |
| 403 | `FORBIDDEN` | 角色不足（RBAC 预留）、CSRF 头校验失败 |
| 404 | `NOT_FOUND` | 对象/版本/证书不存在 |
| 409 | `CONFLICT` | `expectedResourceVersion` 不匹配、同 kind 重名、删除被引用证书 |
| 409 | `NO_CHANGES` | 发布无变更草稿 |
| 422 | `VALIDATION_FAILED` | 编译/语义/终检失败，`details` 携带全部 CompileError |
| 429 | `RATE_LIMITED` | 登录限流（§4.4） |
| 500 | `INTERNAL` | 未分类服务端错误（message 不泄露内部细节） |

### 3.3 资源路径总表

认证与引导（§4）：

| 方法 | 路径 | 职责 |
|---|---|---|
| GET | `/api/v1/auth/bootstrap` | 查询是否需要首次引导（无任何账号） |
| POST | `/api/v1/auth/bootstrap` | 首次引导：创建 admin 并建立会话；已有账号后恒 409 |
| POST | `/api/v1/auth/login` | 登录，Set-Cookie 建立会话 |
| POST | `/api/v1/auth/logout` | 登出，销毁会话 |
| GET | `/api/v1/auth/session` | 当前用户与角色（前端路由守卫） |
| POST | `/api/v1/auth/password` | 修改本人密码（要求旧密码；成功后其他会话失效） |
| GET | `/api/v1/auth/methods` | 可用认证方式列表（OIDC 预留，§4.6） |

配置域（草稿与发布；语义归 [`260717-2-config-domain-design.md`](260717-2-config-domain-design.md)）：

| 方法 | 路径 | 职责 |
|---|---|---|
| GET | `/api/v1/config/draft` | 读取草稿：`{sourceType, resourceVersion, updatedAt, content}`（YAML 全文；`sourceType=protocol|native`） |
| PUT | `/api/v1/config/draft` | 整体替换草稿（CLI/粘贴/专家模式工作流；`expectedResourceVersion` 可选） |
| GET | `/api/v1/config/objects?kind=` | 草稿对象列表（省略 kind = 全部五对象 + EnvoyResources） |
| GET | `/api/v1/config/objects/{kind}/{name}` | 读取单个对象（信封 JSON/YAML） |
| PUT | `/api/v1/config/objects/{kind}/{name}` | 创建/替换对象（upsert；路径与 body 的 kind/name 必须一致） |
| DELETE | `/api/v1/config/objects/{kind}/{name}` | 删除对象；被引用对象允许删除、由 validate 暴露悬空引用 |
| GET | `/api/v1/config/schemas` | 协议 JSON Schema bundle（CLI/编辑器集成用） |
| POST | `/api/v1/config/validate` | 编译校验草稿（F1–F6，可选 F7 终检）；`?mode=xds|static` 指定形态化目标 |
| GET | `/api/v1/config/compiled?mode=` | 草稿编译产物（IR protojson，按 SourceMap 分组；FR-2.3） |
| GET | `/api/v1/config/draft/diff?against=current\|{versionId}&format=unified\|summary` | 变更预览：unified diff 或对象级变更摘要 |
| POST | `/api/v1/config/publish` | 发布草稿 → 新版本 + 原子下发（FR-3.3） |
| GET | `/api/v1/config/status` | 发布/生效状态闭环：published vs effective（FR-4.5） |
| GET | `/api/v1/config/versions` | 版本列表（FR-3.4） |
| GET | `/api/v1/config/versions/{id}` | 版本元数据（作者/时间/message/IR.Version/生效标记） |
| GET | `/api/v1/config/versions/{id}/config` | 该版本配置 YAML 全文（可下载） |
| GET | `/api/v1/config/versions/{id}/compiled?mode=&format=` | 该版本编译产物；`format=static-yaml` 导出可直接使用的 static 配置文件 |
| GET | `/api/v1/config/versions/{id}/diff?against=` | 版本间 diff |
| POST | `/api/v1/config/versions/{id}/rollback` | 回滚：`{"publish": false}` 载入草稿（默认）；`true` 直接重发布（语义终审见 AD5） |

运行状态（数据源与模型归 [`260717-4-state-collection-design.md`](260717-4-state-collection-design.md)，此处仅登记路径归属）：

| 方法 | 路径 | 职责 |
|---|---|---|
| GET | `/api/v1/status/summary` | 概览卡聚合数据 |
| GET | `/api/v1/status/listeners` | 生效 listeners（FR-4.3） |
| GET | `/api/v1/status/clusters` | 生效 clusters |
| GET | `/api/v1/status/clusters/{name}/endpoints` | endpoints 与健康状态 |
| GET | `/api/v1/status/routes` | 生效 routes |
| GET | `/api/v1/status/certs` | 数据面证书链与过期时间 |

统计（P1，模型归 260717-4）：

| 方法 | 路径 | 职责 |
|---|---|---|
| GET | `/api/v1/stats/overview?window=` | 全局 RPS/成功率/延迟分位/连接数时间序列（FR-4.4） |
| GET | `/api/v1/stats/series?dimension=route\|cluster&name=&metric=&window=` | 按 route/cluster 维度的指标序列 |

证书库（托管证书，协议 `ref:` 形态的支撑）：

| 方法 | 路径 | 职责 |
|---|---|---|
| GET | `/api/v1/certs` | 列表：name、SAN/CN、过期时间、被引用 Listener |
| POST | `/api/v1/certs` | 上传 PEM 证书链+私钥（multipart 或 JSON PEM 字段）；服务端解析校验配对 |
| GET | `/api/v1/certs/{name}` | 元数据与证书链详情；**永不返回私钥**（NFR-5） |
| DELETE | `/api/v1/certs/{name}` | 删除；仍被草稿 Listener 引用时 409 并附引用列表 |

环境感知（候选数据归 [`260717-5-k8s-disco-design.md`](260717-5-k8s-disco-design.md)）：

| 方法 | 路径 | 职责 |
|---|---|---|
| GET | `/api/v1/disco/environment` | 环境探测结果（`{kubernetes: bool, ...}`；FR-6.1），前端据此显隐 k8s 增强 |
| GET | `/api/v1/disco/k8s/services?namespace=&q=` | Service 候选搜索（FR-6.2，仅 k8s 环境可用，否则 404） |

系统与运维：

| 方法 | 路径 | 职责 |
|---|---|---|
| GET | `/api/v1/system/info` | esgw 版本、Envoy 版本/拓扑、下发模式、构建信息 |
| GET | `/healthz`、`/readyz` | 存活/就绪（FR-5.5；无鉴权，不含敏感信息） |
| GET | `/metrics` | Prometheus 透传（M-STATE 职责，鉴权策略归 260717-4） |

### 3.4 OpenAPI 维护策略

**决策：spec-first。** `api/openapi.yaml` 手写维护，是 API 的唯一契约真源；Go 侧请求/响应类型与路由接口由代码生成（倾向 [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) strict-server 模式，工具终审见 AD2）；CI 两道防线：

1. 重新生成代码并与仓库内容 diff，不一致即失败（防止手写绕过 spec）；
2. spec lint（vacuum/spectral）+ 校验示例请求/响应与 schema 一致。

理由：

- **契约评审单元是 spec diff 而非 Go diff**——U1 要求 API 作为产品对待，评审者（含未来的 CLI 与第三方集成方）直接读 spec；
- swaggo 式"代码注释生成 spec"对复杂错误模型与多表示（JSON/YAML 双形态）描述力弱，且注释与实现容易腐烂脱节；
- 生成类型消除"spec 与 handler 双写"的漂移成本。

**协议对象 schema 的双源问题**：五对象信封的 JSON Schema 已由 protocol Go 类型生成（[`260716-1-gateway-config-protocol-v0.md`](260716-1-gateway-config-protocol-v0.md) §6"单一事实来源"）。openapi.yaml 中 `config/objects` 端点的 body 不手写重复定义，由构建脚本将生成产物嵌入 spec 的 `components`，CI 校验嵌入副本与生成产物一致——Go 类型仍是底层唯一真源，spec 是其发布形态。

### 3.5 关键端点示例

**① 保存配置对象**（`PUT /api/v1/config/objects/Route/api`）——信封即协议对象（[`260716-1-gateway-config-protocol-v0.md`](260716-1-gateway-config-protocol-v0.md) §2.1）：

```json
// 请求
{
  "apiVersion": "esgw/v1alpha1",
  "kind": "Route",
  "metadata": {"name": "api", "labels": {"team": "core"}},
  "spec": {
    "listeners": ["web-https"],
    "hostnames": ["api.example.com"],
    "rules": [
      {"match": {"path": {"prefix": "/users/"}},
       "rewrite": {"pathPrefix": "/"},
       "backends": [{"upstream": "user-svc"}]}
    ]
  }
}
// 200 响应
{
  "object": { ...同上，含默认值填充前的原文... },
  "draftResourceVersion": 18
}
```

**② 校验草稿**（`POST /api/v1/config/validate?mode=xds`）：

```json
// 请求
{"envoyValidate": true}
// 200 响应（业务校验失败也是 200 之外的选择：校验请求本身合法，
//  编译结果用 ok 表达；errors 详情走 VALIDATION_FAILED/422 仅当请求形态非法。
//  ——决策：validate 是"查询校验结果"语义，恒 200 + results）
{
  "ok": false,
  "mode": "xds",
  "irVersion": null,
  "results": [
    {"stage": "link", "source": {"kind": "Route", "name": "api", "path": "spec.listeners[0]"},
     "message": "引用的 listener \"web-https\" 不存在", "severity": "error"},
    {"stage": "envoy", "source": {},
     "message": "未找到 envoy 二进制，跳过 F7 终检", "severity": "warning"}
  ]
}
```

**③ 发布与生效确认**（`POST /api/v1/config/publish` → `GET /api/v1/config/status`）：

```json
// POST /api/v1/config/publish  请求
{"message": "新增 blog 域名"}
// 200 响应
{
  "version": {"id": 42, "irVersion": "3f9a1c2e8b01",
              "createdAt": "2026-07-17T08:30:00Z", "author": "admin",
              "message": "新增 blog 域名"},
  "delivery": {"mode": "xds", "state": "delivering"}
}

// GET /api/v1/config/status  响应（生效确认闭环，FR-4.5）
{
  "published": {"id": 42, "irVersion": "3f9a1c2e8b01", "publishedAt": "2026-07-17T08:30:00Z"},
  "effective": {"irVersion": "3f9a1c2e8b01", "confirmedAt": "2026-07-17T08:30:02Z",
                "source": "config_dump"},
  "delivery": {"mode": "xds", "state": "effective"}
}
```

`delivery.state` 枚举（`idle|delivering|effective|degraded`）与 static 模式的 hot restart 状态语义归 [`260717-1-deliver-layer-design.md`](260717-1-deliver-layer-design.md)；effective 确认机制（config_dump 版本比对）归 [`260717-4-state-collection-design.md`](260717-4-state-collection-design.md)。前端发布流即轮询本端点直至 `effective` 或超时降级提示。

**④ 变更预览**（`GET /api/v1/config/draft/diff?against=current&format=summary`）：

```json
{
  "base": {"type": "version", "id": 41, "irVersion": "9c04e77aa2f1"},
  "format": "summary",
  "changes": [
    {"op": "create", "kind": "Route", "name": "blog"},
    {"op": "modify", "kind": "Listener", "name": "web-https"},
    {"op": "delete", "kind": "Upstream", "name": "legacy"}
  ]
}
```

`format=unified`（默认）返回 `{"diff": "--- a/config.yaml\n+++ b/config.yaml\n..."}`，diff 基线为两版确定性渲染 YAML（U6），保证所见即所发。

**⑤ 登录**（`POST /api/v1/auth/login`）：

```json
// 请求
{"username": "admin", "password": "············"}
// 200 响应（Set-Cookie: esgw_session=<token>; Path=/; HttpOnly; SameSite=Lax）
{"user": {"name": "admin", "roles": ["admin"]},
 "expiresAt": "2026-07-18T08:30:00Z"}
// 失败：401 {"error": {"code": "UNAUTHENTICATED", "message": "用户名或密码错误"}}
//       （不区分"用户不存在"与"密码错误"，防枚举）
```

## 4. 鉴权与会话

### 4.1 账号模型与首次引导

- 首期**单 admin 账号**，用户名在引导时由用户设定；账号记录存 SQLite（表结构归 [`260717-2-config-domain-design.md`](260717-2-config-domain-design.md)，本文只定接口语义）。
- 首次启动（无任何账号）：`GET /api/v1/auth/bootstrap` 返回 `{"required": true}`，控制台强制跳转 `/setup`；`POST /api/v1/auth/bootstrap` 创建 admin 并直接建立会话（免于二次登录）。esgw 启动日志打印控制台地址提示完成引导，**不生成也不打印临时密码/令牌**（避免日志泄密的反模式）。
- 密码策略：最少 12 字符，不强制字符类组合（长度优先；不引入强度库依赖）。

### 4.2 密码散列

**决策：Argon2id**（`golang.org/x/crypto/argon2`），参数 m=64MiB / t=3 / p=2，salt 16B、key 32B，以 PHC string 格式存储（参数随哈希落库，后续调参兼容旧记录）。

理由：内存硬度抵御离线字典攻击，是 OWASP 当前首选；bcrypt 为次优备选（72 字节截断问题需预散列 workaround）；64MiB 峰值内存仅在登录瞬间占用，与 NFR-2（<150MB 常驻）不冲突。

### 4.3 Cookie Session

| 属性 | 决策 |
|---|---|
| 凭证 | 32B `crypto/rand`，base64url 编码；库中只存 SHA-256(token)（泄库不等于泄会话） |
| Cookie | `esgw_session`；`HttpOnly`、`Path=/`、`SameSite=Lax`；请求经 HTTPS 到达时置 `Secure` |
| 过期 | 空闲 24h 滑动续期（剩余寿命过半时换新 token 重发 Cookie）；绝对上限 7 天强制重登 |
| 存储 | SQLite 会话表：token 哈希、用户名、创建/过期/最后活跃时间、UA 摘要；表结构归 260717-2 |
| 失效 | 登出删除记录；修改密码使该用户其他会话全部失效 |

决策说明：选服务端会话而非 JWT——管理面是单进程、会话量极小，服务端会话换来即时吊销与登出语义，JWT 的无状态优势在此无收益而吊销成本真实存在。

### 4.4 CSRF 与登录限流

- **CSRF**：双层——① `SameSite=Lax` 阻断跨站携带 Cookie 的写请求；② 所有非 GET/HEAD/OPTIONS 的 `/api` 请求必须携带自定义头 `X-ESGW-Request: 1`（跨站表单无法附加自定义头，附加即触发 CORS 预检而被默认 CORS 策略拒绝）；缺失返回 403。不引入独立 CSRF token 发放流程，SPA 全局注入该头即可。
- **CORS**：默认不放行任何跨域 Origin；前端开发期经 Vite dev server 代理保持同源。
- **登录限流**：每 IP 10 次/分钟 + 同账号连续失败 5 次锁定 5 分钟（进程内存计数器，重启清零——单实例场景可接受，返回 429）。

### 4.5 角色模型与 RBAC 预留

首期单一角色 `admin`（全权）。`auth/session` 与 login 响应返回 `roles` 数组、鉴权中间件按"路由 → 所需角色"表检查——为后续 RBAC（`viewer` 只读 / `operator` 可发布不可管账号等）预留接口形态，但**首版不实现多角色与多用户管理端点**（FR-4.6 P2 范畴），避免无需求支撑的权限矩阵先行腐烂。

### 4.6 OIDC 预留形态（FR-4.6 P2）

- `GET /api/v1/auth/methods` 返回 `[{"type": "local"}]`；接入 OIDC 后增加 `{"type": "oidc", "displayName": "...", "loginUrl": "/api/v1/auth/oidc/default/login"}`，登录页据此渲染"使用 SSO 登录"入口。
- 流程端点（预留，不实现）：`GET /api/v1/auth/oidc/{provider}/login`（302 至 IdP）、`GET /api/v1/auth/oidc/{provider}/callback`（换取身份后建立会话）。
- 关键设计点：**认证源对会话层透明**——OIDC 登录成功后发放的仍是 §4.3 的 `esgw_session`，会话、CSRF、角色检查全部复用，无第二套凭证体系。
- provider 配置（issuer/clientID/secret）走 esgw 配置文件而非 API，避免通过管理面改写自身认证来源的提权路径。详细契约（claims → 用户/角色映射）P2 立项时定（AD4）。

## 5. 静态资源服务

前端 SPA 经 `go:embed` 打进二进制（A6），由 M-API 直接服务：

| 路径 | 行为 | 缓存策略 |
|---|---|---|
| `/assets/*`（Vite 产物，文件名带内容哈希） | 直接服务 | `Cache-Control: public, max-age=31536000, immutable` |
| `/` 及所有非 `/api`、非静态资源的未知路径 | fallback 到 `index.html`（SPA history 路由） | `Cache-Control: no-cache`（每次协商，保证发版即生效） |
| `/api/*` 未知路径 | JSON 404，**绝不 fallback 到 index.html**（防止 API 调用方把 HTML 当响应解析） | — |

要点：版本指纹完全由 Vite 文件名哈希承担，服务端不做资源改写与运行时注入（运行时信息经 `system/info` 获取）；文本资源（html/js/css）经 gzip/br 压缩中间件；embed 使"下载单二进制即得完整控制台"成立（NFR-1/FR-5.1）。

## 6. 前端工程组织

### 6.1 技术栈

| 决策点 | 选型 | 说明 |
|---|---|---|
| 框架 | **React + TypeScript + Vite**（架构 §2 倾向；D3 终审权在控制台 sprint） | 表单密集型控制台成熟生态 |
| 组件库 | **Ant Design** | 表格/表单/ diff 展示组件齐备 |
| YAML 编辑器 | **Monaco** + monaco-yaml（JSON Schema 驱动的语法高亮与行内校验） | 与 VS Code 同内核，FR-4.2 的 schema 提示直接复用 §3.3 的 schema bundle |
| 工程位置 | `web/`（独立 npm 工程），产物 `web/dist` 由 Go embed | 构建管线与 Go 解耦，发版时合并 |
| 数据请求/状态 | 随 D3 终审一并选定（候选 TanStack Query） | 不在本文锁定 |

### 6.2 表单 ↔ YAML 双向同步策略（FR-4.2 / U5）

**决策：以 YAML 文本为底，表单是结构化投影。** 编辑会话的单一事实来源是该对象的 YAML 文本（与草稿文件内容一致）；表单不做独立状态持久化，每次进入由 YAML 解析投影而来。

三种编辑态（保证任何合法配置不丢）：

| 状态 | 判定 | 表单行为 |
|---|---|---|
| S1 完全可结构化 | YAML 可解析且 `spec` 可被 schema 驱动表单完整表达 | 表单可读写；变更经 **YAML AST 级回写**（只触碰对应节点，保注释、保字段顺序），YAML 视图实时同步 |
| S2 部分可结构化 | 含 `envoyPatch` / `EnvoyResources` 等无法表单化字段 | 结构化部分正常编辑；高级字段以只读 YAML 片段内嵌展示并标注"高级配置（escape hatch）"；保存时该部分原文并回 |
| S3 解析失败 | YAML/信封非法 | 表单禁用 + 错误行内标注；仅 YAML 视图可编辑 |

兜底规则：**无法安全完成 AST 回写时，拒绝切换并停留在 YAML 视图提示用户**，绝不整文档重生成（那会丢注释与顺序）。AST 编辑库候选 eemeli/yaml 的 Document API，终审随 D3。

依据：协议有 escape hatch 与严格解析（P6），表单表达能力注定是 schema 的子集；"以 YAML 为底 + 降级只读"使 FR-4.2 的双向切换对 100% 合法配置安全，而非仅对表单认识的字段安全——这是 U5 的落地。内联匿名 Policy（协议 §3.5）按一等表单能力支持，不触发降级。

### 6.3 Schema 与校验来源

- 前端构建期将 protocol 包生成的 JSON Schema bundle 打包进产物（版本与后端同源，CI 保证同步）；Monaco YAML diagnostics 与表单渲染元数据均由其驱动。`GET /api/v1/config/schemas` 作为运行时获取入口，供 CLI 与第三方编辑器集成。
- 两级校验：编辑期本地 schema 即时校验（拦截结构错误）；保存/发布前调 `config/validate` 跑 F1–F6(–F7) 全量校验，`CompileError.source` 映射为 Monaco 行内 marker（编译层"全部错误必须带 SourceRef"是硬性验收，此处兑现为 UI 能力）。

## 7. 未决事项

| # | 事项 | 计划决策时机 |
|---|---|---|
| AD1 | 前端框架与数据请求库终审（React + AntD + Monaco 倾向 vs Vue 备选）——即上游 D3 在本文范围的落地 | 控制台 sprint 启动时 |
| AD2 | OpenAPI 代码生成工具终审（oapi-codegen 倾向）与 spec lint 工具选定 | 控制台 sprint 启动时 |
| AD3 | 管理面 admin server 自身 TLS：证书来源（用户提供 / 自动自签 / 复用证书库）与 `Secure` Cookie 的代理场景判定（可信 `X-Forwarded-Proto`） | M1 控制台 sprint 期间 |
| AD4 | OIDC 详细契约：claims → 用户/角色映射、多 provider、与本地账号的并存策略 | P2 OIDC 立项时 |
| AD5 | 回滚默认语义：`publish=false`（载入草稿）与 `true`（直接重发布）的产品默认值；语义终审归 260717-2，本文 API 形态已兼容两者 | 控制台 sprint + 配置域文档联合 |
| AD6 | 统计 API 的窗口/序列粒度契约（window 枚举、点密度上限） | 与 [`260717-4-state-collection-design.md`](260717-4-state-collection-design.md) 对齐时（P1） |
| AD7 | 证书托管 `ref:` 与文件路径双形态保留的产品确认（协议 PD3；本文已按双形态设计证书库 API） | M1 控制台 sprint，与 260717-2 联合确认 |

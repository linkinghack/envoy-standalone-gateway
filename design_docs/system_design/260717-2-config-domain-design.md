# 260717-2 配置域与发布流设计（M-CONF / M-STORE Design）

- **项目**: envoy-standalone-gateway
- **上游文档**: [`260715-2-overall-architecture.md`](260715-2-overall-architecture.md)（A5 文件真源、M-CONF/M-STORE 模块划分、未决项 D2）、[`260716-1-gateway-config-protocol-v0.md`](260716-1-gateway-config-protocol-v0.md)（五对象与 EnvoyResources 载体）、[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md)（ConfigSet 输入、F1-F7 流水线、IR.Version）
- **文档状态**: 初稿（Draft）
- **日期**: 2026-07-17
- **性质**: 模块详细设计。覆盖架构 §8 清单第 4 项：配置域数据模型、版本管理与 diff、发布流状态机、文件真源布局，含 M-STORE 存储设计并对 D2（SQLite vs bbolt）给出终审结论。发布下发细节见 [`260717-1-deliver-layer-design.md`](260717-1-deliver-layer-design.md)，API 形态见 [`260717-3-console-api-design.md`](260717-3-console-api-design.md)。

---

## 1. 职责总览

M-CONF（配置域）负责抽象协议对象与原生 static 配置的 **CRUD、版本管理（历史/diff/回滚，FR-3.4）、发布流状态机（FR-4.5）**；M-STORE（存储）负责其下的持久化原语。两者按架构原则 A5 切分职责：

| 持久化介质 | 存什么 | 为什么 |
|---|---|---|
| **文件（真源）** | 用户配置（`config.d/*.yaml` 或 `native.yaml`）、历史版本快照、托管证书文件 | A5：配置可 git 管理、可读、可手工编辑；脱离 esgw 也可复活 |
| **SQLite（运行态与索引）** | 版本元数据、发布运行记录、会话/用户、审计（P2 预留）、设置项、证书索引 | A5：DB 只存运行态与索引；删掉 `esgw.db` 可从文件重建配置面 |

由此得出两条硬规则：

1. **无独立草稿副本**：草稿（Draft）即 `config.d/` 当前磁盘内容，DB 只存其派生状态（内容哈希、最近校验结果）。不做"DB 草稿 + 发布时落盘"的双写模型——双写必然出现真源分叉，违背 A5。
2. **一切生效配置必来自版本快照**：Envoy 上跑的内容永远对应某个 `versions/000NNN/` 快照，而非"某时刻的磁盘"。这保证回滚、审计、"当前生效的是什么"三个问题都有确定答案。

发布闭环（与架构 §4.1 一致，本文档细化状态机与失败语义）：

```
草稿(config.d) → 编译校验(F1-F6, 可选F7) → 生成版本快照 → M-DELIVER 下发
             → M-STATE 从 config_dump 确认 IR.Version 生效 → 发布闭环
```

## 2. 核心数据模型

### 2.1 ConfigSet（全量配置集）

与编译层输入同构（[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §2），配置域在其上增加**来源信息**——每个对象记录来源文件与文档序号，用于错误回指与按文件重写：

```go
// internal/conf
type ConfigSet struct {
    Mode           Mode                  // ModeAbstract | ModeNative（§2.4）
    Gateway        *proto.Gateway        // 可为 nil（隐式默认）
    Listeners      []*proto.Listener     // 每个对象内嵌 Origin
    Routes         []*proto.Route
    Upstreams      []*proto.Upstream
    Policies       []*proto.Policy
    EnvoyResources []*proto.EnvoyResources
    Native         *NativeConfig         // ModeNative 时唯一非 nil（§2.4）
    Hash           string                // 全文件内容确定性哈希（乐观并发基准）
}

type Origin struct {
    File    string // 相对路径，如 config.d/10-listeners.yaml
    DocIndex int   // 文件内第几个 YAML 文档（0 起）
}
```

`Origin` 是 CRUD 的关键：表单保存一个对象时，配置域定位到 `(File, DocIndex)` 原地替换该 YAML 文档，**不重排、不重写其他文件**——用户手工组织的文件结构与注释得到保留。

### 2.2 Draft（草稿）

草稿不是存储实体，而是"config.d 当前内容 + DB 派生状态"的逻辑组合：

| 属性 | 载体 | 说明 |
|---|---|---|
| 内容 | 文件 `config.d/`（或 `native.yaml`） | 唯一真源 |
| `draftHash` | DB `settings` | 全部配置文件内容的确定性哈希，乐观并发与外部修改检测的基准 |
| 最近校验结果 | DB（草稿校验缓存，可丢） | schema 级校验错误列表，带 SourceRef 供 UI 行内标注（FR-4.2） |

### 2.3 Version（不可变发布版本）

发布成功即生成一个不可变版本：

```go
type Version struct {
    Seq        int64     // 单调递增序号，主键（000042 即目录名）
    CreatedAt  time.Time
    Author     string    // 会话用户名；外部修改触发记为 "external"
    Message    string    // 发布备注，可空
    Mode       string    // xds | static（发布时的下发模式）
    IRVersion  string    // 编译产物内容哈希，关联编译层 IR.Version（确定性保证）
    State      string    // effective | superseded | failed（§5）
    ParentSeq  int64     // 基于哪个版本发布（回滚发布时指向被回滚源）
    RollbackOf int64     // 非 0 表示本版本是对该序号的回滚发布
    Stats      ObjectStats // 对象计数（listeners/routes/...），供列表页展示
}
```

- `Seq` 与 `IRVersion` 双标识：**Seq 回答"第几次发布"（用户语义），IRVersion 回答"内容是什么"（数据面语义）**。两次发布内容相同（如回滚到上一版）时 Seq 不同、IRVersion 相同——M-DELIVER 据此可跳过重复下发。
- 不可变性：版本快照目录写入后任何代码路径不得修改；`State` 字段（effective→superseded）是 DB 中的投影，不改快照内容。

### 2.4 专家模式原生 static 配置的载体形态

FR-2.1（整套原生 static）在配置域中的载体为**单一文件 `native.yaml`**，与 `config.d/` **互斥**：

| 形态 | 载体 | 混用规则 |
|---|---|---|
| 抽象模式 | `config.d/*.yaml`，五对象 + `EnvoyResources`（协议 §7.2） | `EnvoyResources` 即混用入口（FR-2.2 的实现基础） |
| 原生模式 | `native.yaml`（完整 Envoy bootstrap/static） | 与 `config.d/` 同时存在 = 启动与保存均报错，必须二选一 |

依据：[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §2 已定"原生 static → IR 解析器"走独立入口并在 F6 汇合；互斥（而非任意混排）把"两套语义谁覆盖谁"的问题从运行时移到入口处的显式选择，切换模式在 UI 是一个带警告的显式操作。原生模式的 CRUD 退化为整文件编辑（Monaco YAML 编辑器），版本/diff/回滚/发布流与抽象模式完全同构。

## 3. 文件真源布局

### 3.1 数据目录结构

```
<data-dir>/                     # 默认 /var/lib/esgw；docker 挂卷；dev 模式 ./data
├── config.d/                   # 抽象模式真源（多文件，可 git 管理）
│   ├── 00-gateway.yaml
│   ├── 10-listeners.yaml
│   └── ...
├── native.yaml                 # 原生模式真源（与 config.d 互斥，§2.4）
├── versions/                   # 不可变版本快照
│   ├── 000041/
│   │   ├── config/             # 发布时 config.d 的原样拷贝（或 native.yaml）
│   │   ├── meta.json           # Version 元数据冗余落盘（DB 可从此重建）
│   │   └── artifacts/          # 编译产物：snapshot protojson / rendered static yaml
│   └── 000042/ ...
├── certs/                      # 托管证书（<name>.crt / <name>.key，权限 0600）
├── esgw.db                     # SQLite 运行态（WAL 模式）
└── tmp/                        # 渲染临时文件（F7 validate、原子写中转）
```

设计要点：

1. **快照 = 文件拷贝**：版本快照就是发布瞬间真源文件的逐字节拷贝，不做打包压缩——可直接 `diff -r`、可被 git 跟踪整个 `versions/`、可手工恢复（把 `versions/000NNN/config/` 拷回 `config.d/` 即完成一次"手工回滚"）。
2. **artifacts 是缓存不是真源**：编译产物由确定性编译（[`260716-2-compile-ir-design.md`](260716-2-compile-ir-design.md) §5）可随时重建，落盘只为排障与"查看编译产物"（FR-2.3）加速；保留策略见未决事项 CD1。
3. **meta.json 冗余**：DB 损毁时从 `versions/*/meta.json` 重建版本索引，恢复 A5 承诺。
4. `certs/` 文件权限 0600；私钥加密存储方案属未决项 D5（架构 §9），此处只固定目录与命名约定。

### 3.2 加载规则：多文件 → ConfigSet

- 扫描 `config.d/` 下 `*.yaml | *.yml`，按**文件名字典序**逐个解析；每文件可含多个 `---` 分隔文档（协议 §2.1）。
- 信封 strict decode（未知字段报错，协议 P6）；`kind` 分发入 ConfigSet 对应集合并记录 `Origin`。
- **重名检测**：同 `kind` + 同 `metadata.name` 出现两次 = 加载错误，报出两处 `(File, DocIndex)`。不做"后者覆盖前者"的 kustomize 式语义——静默覆盖是手工编辑多文件时最常见的踩坑点。
- 加载错误整体表现为草稿的校验错误（带 SourceRef），不阻止 esgw 启动——**管理面必须能在配置写坏时起来让人修**（NFR-3 的推论）。

### 3.3 DB 只存运行态与索引

`esgw.db` 中**不出现任何一份配置内容本体**。DB 丢失的恢复语义：版本索引从 `versions/*/meta.json` 重建；草稿即 `config.d/` 现状；会话/审计/缓存随 DB 一起丢弃（可接受）。表结构见 §7。

### 3.4 外部修改检测与 reload

真源是文件就意味着用户会绕过 UI 改文件（git pull、手工编辑）。策略：

| 环节 | 策略 |
|---|---|
| 检测 | fsnotify 监听 `config.d/` + `native.yaml`；事件去抖 500ms 后重算 `draftHash`，与 DB 记录比对 |
| 生效语义 | **外部修改绝不自动发布**。生效路径只有"发布"一条；文件变了 ≠ Envoy 要变 |
| UI 呈现 | 草稿状态变为 `external_modified`，控制台横幅提示"文件已被外部修改"，展示新校验结果 |
| 并发冲突 | 发布请求携带 `baseHash`（§5.4）；外部修改后旧 `baseHash` 的发布请求返回 409，前端强制刷新——防止 UI 上的旧表单覆盖 git pull 进来的新内容 |
| fsnotify 不可用（网络盘等） | 降级为 5s 轮询 mtime+size，行为一致 |

依据：检测但不自动发布，使"git 管理配置"与"控制台发布流"两种工作方式正交共存——git 用户把 esgw 当"带校验与下发的编辑器"，UI 用户不受 git 干扰。

## 4. 版本模型与 diff

### 4.1 发布即快照

发布动作在编译校验通过后执行：

1. 全量拷贝真源文件 → `versions/<seq+1>/config/`（原子：先写 `tmp/` 再 rename）；
2. 写 `meta.json` 与 DB `versions` 行；
3. 编译产物写 `artifacts/`；
4. 触发 M-DELIVER 下发。

只有第 4 步失败路径允许版本处于非 effective 态（§5）。

### 4.2 双视角 diff（FR-3.4 / FR-4.5）

| 视角 | 内容 | 用途 |
|---|---|---|
| **配置文本 diff** | 两版本 `config/` 快照逐文件 unified diff（展示前规范化：CRLF→LF、去行尾空白，抑制噪声） | 回答"用户改了什么"；发布预览与版本对比的默认视图 |
| **编译产物 diff** | 两版本 IR 的资源级对比：按 `(资源类型, name)` 归并为 added / removed / changed；changed 展开 protojson 字段级差异摘要 | 回答"Envoy 实际会怎么变"；支撑 FR-4.5 变更预览，也是理解 escape hatch 影响的唯一途径 |

编译产物 diff 复用确定性编译：对历史版本快照重新 Compile 一次即得其 IR（产物未落盘或已清理时同样成立）。`SourceMap` 使产物 diff 的每个资源可回指协议对象，UI 按对象分组展示（FR-2.3 同源能力）。

### 4.3 回滚语义

**回滚 = 以历史版本内容生成一次新版本发布**，历史保持线性，不做指针回拨：

```
v41 → v42 → v43(当前)
回滚 v42  ⇒  以 v42 的 config/ 覆盖 config.d，按正常发布流产出 v44
             v44.RollbackOf = 42, v44.ParentSeq = 43
```

- 与"重新发布旧内容"完全同一条代码路径：编译、校验、diff 预览、下发、生效确认全部走一遍，无特权捷径。
- `config.d/` 被覆盖前若存在未发布的外部修改，回滚请求需显式确认丢弃（UI 二次确认；API 需 `force: true`）。
- 线性历史使审计（FR-4.7）与"当前生效版本"的叙事永远简单：effective 永远是最新一个 State=effective 的版本。

## 5. 发布流状态机（FR-4.5）

### 5.1 状态枚举与迁移

每次发布创建一条 `publish_run`（一次状态机实例）：

```
                 ┌──────────────┐
                 │  VALIDATING  │  F1-F6 全量编译（+可选 F7 envoy validate）
                 └──────┬───────┘
              编译错误   │   编译通过
              ┌─────────┴─────────┐
              ▼                   ▼
      ┌───────────────┐   ┌────────────┐   用户确认/自动    ┌─────────────┐
      │ VALIDATE_FAILED│  │ VALIDATED  │ ────────────────► │ PUBLISHING  │
      └───────────────┘   └────────────┘                   └──┬───┬───┬──┘
                            （携带 diff 预览）      下发失败    │   │   │ M-DELIVER 受理成功
                                                  ┌───────────┘   │   ▼
                                                  ▼               │  ┌────────────┐  M-STATE 确认 IR.Version
                                          ┌──────────────┐        │  │ CONFIRMING │ ─────────────────────┐
                                          │PUBLISH_FAILED│        │  └─────┬──────┘                      │
                                          └──────────────┘        │        │ 确认超时(默认30s)            ▼
                                                                  │        ▼                     ┌───────────┐
                                                                  │  ┌───────────────┐           │ EFFECTIVE │
                                                                  │  │CONFIRM_TIMEOUT│           └───────────┘
                                                                  │  └───────────────┘
                                                                  └─ (VALIDATED 可直接到 PUBLISHING；
                                                                      API 支持 validate+publish 一步调用)
```

- **草稿无状态机**：草稿只有"内容 + 派生校验结果"，状态机只属于发布运行。一个配置集同一时刻至多一个非终态 publish_run。
- `CONFIRM_TIMEOUT` 不是失败：配置可能正常只是确认链路异常（如 admin API 暂不可达）。该状态允许人工重查（re-check 迁移到 CONFIRMING）或忽略；不阻塞下一次发布。
- 终态：`VALIDATE_FAILED`、`PUBLISH_FAILED`、`CONFIRM_TIMEOUT`、`EFFECTIVE`。

### 5.2 每步调谁

| 状态迁移 | 动作 | 依赖模块 |
|---|---|---|
| → VALIDATING | 加载 ConfigSet → `compile.Compile(cs, opts)` 跑 F1-F6；按设置附加 F7（`envoy --mode validate`，默认开，无二进制降级 Warning，见编译层 §3/F7） | M-COMPILE |
| VALIDATING → VALIDATED | 计算双视角 diff（§4.2）存入 publish_run 供预览 | M-CONF 内部 |
| VALIDATED → PUBLISHING | 写版本快照（§4.1），调 `M-DELIVER.Apply(ir)`；xDS = SnapshotCache.SetSnapshot，static = 渲染 + M-PROC hot restart（细节见 [`260717-1-deliver-layer-design.md`](260717-1-deliver-layer-design.md)） | M-DELIVER |
| PUBLISHING → CONFIRMING | M-DELIVER 返回已受理（xDS snapshot 已装载 / static 新 epoch 已拉起） | M-DELIVER |
| CONFIRMING → EFFECTIVE | 消费 M-STATE 的生效确认事件：`/config_dump` 中出现 `IR.Version`（版本注入方式与回报契约由 [`260717-1-deliver-layer-design.md`](260717-1-deliver-layer-design.md) 定义） | M-STATE |
| 任意 → EFFECTIVE 后 | 旧 effective 版本 State → superseded | M-CONF 内部 |

### 5.3 并发控制

| 机制 | 实现 |
|---|---|
| 单配置集单发布 | 进程内互斥锁 + DB 约束兜底：`publish_runs` 上对非终态状态建唯一部分索引（`WHERE state IN ('VALIDATING','VALIDATED','PUBLISHING','CONFIRMING')`），并发提交第二条直接失败 |
| 乐观并发（编辑 vs 发布） | 发布/保存请求携带 `baseHash` = 调用方最近读到的 `draftHash`；与当前真源不符返回 409 + 最新哈希。UI 表单与 YAML 编辑器共用此契约（API 细节见 [`260717-3-console-api-design.md`](260717-3-console-api-design.md)） |
| VALIDATED 滞留 | VALIDATED 状态带 TTL（默认 10 分钟）：超时未 publish 自动失效，防止"审着旧 diff 发布新世界"——publish 时复检 `draftHash` 与校验时一致，不一致强制重新 validate |

### 5.4 失败留痕与可查性

- 每次 publish_run 全量落 DB：触发者、触发时间、baseHash、各状态迁移时间戳、CompileError 列表（含 SourceRef）、diff 摘要、M-DELIVER 返回、M-STATE 确认证据（观测到的 IR.Version 与时间）。
- `VALIDATE_FAILED` 的完整错误列表就是 FR-4.5"校验结果"步骤的数据源，UI 原样呈现。
- 发布历史列表 = versions ⨝ publish_runs，控制台"发布"页一页讲清每次变更的来龙去脉（FR-4.5）；该数据同时是审计日志（FR-4.7，P2）的事实来源。

## 6. CRUD 与校验时机

| 时机 | 校验深度 | 说明 |
|---|---|---|
| **草稿保存**（写文件） | schema 级：信封 strict decode + JSON Schema（编译流水线 F1 同规则） | 保存即写真源（§1 规则 1）；写前校验，非法内容拒绝落盘。不做跨对象语义校验——允许"先存半个配置"（如 Route 引用了还没建的 Upstream），这是文本工作流的自然状态 |
| **保存后（异步）** | 全量编译 F1-F6（无 F7） | 更新草稿派生校验结果，供 UI 行内标注与"发布"按钮置灰判断；失败不阻断继续编辑 |
| **发布（VALIDATING）** | F1-F6 全量 + 可选 F7 | 唯一的生效闸门，满足 FR-3.3"下发前完成全量校验" |
| **原生模式保存** | protojson strict parse（编译层"原生 static → IR 解析器"的解析段） | 同上三级 |

UI 表单保存即写文件真源：表单编辑被翻译为对 `Origin` 定位文档的原地 YAML 替换（§2.1），表单视图与 YAML 源码视图因此永远一致——两者是同一份真源的两种投影，不存在同步问题（与 [`260717-3-console-api-design.md`](260717-3-console-api-design.md) 的表单↔YAML 策略呼应）。新建对象默认写入 `config.d/` 中按 kind 归组的既有文件，无匹配文件时新建 `<kind 小写复数>.yaml`；用户自行拆分组织的文件结构不被改动。

## 7. 存储选型落实（D2 终审）

**结论：采用 SQLite（modernc.org/sqlite，纯 Go 驱动），bbolt 淘汰。D2 就此终审关闭。**

| 判据 | SQLite | bbolt |
|---|---|---|
| 结构化查询（版本列表过滤、发布历史 join、审计检索） | 原生 SQL，直接满足 | kv 之上需自建索引，代码量大 |
| 部分唯一索引（§5.3 并发约束） | 支持 | 无 |
| 单文件、零依赖、嵌入式 | ✓（纯 Go 驱动，无 cgo，NFR-2/A6） | ✓ |
| 运维工具链（备份/检查/手工查询） | `sqlite3` CLI + WAL 在线备份成熟 | 需自写工具 |
| 风险 | 无 cgo 交叉编译问题（modernc 纯 Go）；写并发低（单写者），本场景单进程够用 | 查询层全自建，随功能演进持续付税 |

A5 下 DB 只是运行态与索引，规模极小（版本数千行量级），SQLite 的性能上限远超需求；淘汰 bbolt 的决定性理由是版本/审计这类天然关系型查询的持续开发成本。

### 7.1 表结构草案

```sql
-- 版本元数据（内容本体在 versions/<seq>/，见 A5）
CREATE TABLE versions (
  seq          INTEGER PRIMARY KEY,          -- 单调递增
  created_at   TEXT NOT NULL,                -- RFC3339
  author       TEXT NOT NULL,
  message      TEXT NOT NULL DEFAULT '',
  mode         TEXT NOT NULL,                -- xds | static
  ir_version   TEXT NOT NULL,                -- 编译产物哈希
  state        TEXT NOT NULL,                -- effective | superseded | failed
  parent_seq   INTEGER NOT NULL DEFAULT 0,
  rollback_of  INTEGER NOT NULL DEFAULT 0,
  stats_json   TEXT NOT NULL DEFAULT '{}'    -- 对象计数
);

-- 发布运行记录（§5 状态机实例，失败留痕）
CREATE TABLE publish_runs (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  version_seq  INTEGER,                      -- 成功生成版本后回填，可 NULL
  trigger_by   TEXT NOT NULL,
  base_hash    TEXT NOT NULL,
  state        TEXT NOT NULL,                -- §5.1 状态枚举
  errors_json  TEXT NOT NULL DEFAULT '[]',   -- []CompileError（含 SourceRef）
  diff_json    TEXT NOT NULL DEFAULT '{}',   -- 双视角 diff 摘要
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);
-- 单配置集单发布：非终态唯一（§5.3）
CREATE UNIQUE INDEX one_active_publish ON publish_runs(state)
  WHERE state IN ('VALIDATING','VALIDATED','PUBLISHING','CONFIRMING');

-- 本地账号与会话（FR-4.6 首期；API 形态属控制台文档）
CREATE TABLE users (
  id INTEGER PRIMARY KEY, username TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE TABLE sessions (
  token TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id),
  created_at TEXT NOT NULL, expires_at TEXT NOT NULL,
  ip TEXT, user_agent TEXT
);

-- 审计（FR-4.7，P2 预留表结构，本期不写入）
CREATE TABLE audit_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts TEXT NOT NULL, actor TEXT NOT NULL,
  action TEXT NOT NULL,                      -- save_draft | publish | rollback | login ...
  object TEXT NOT NULL DEFAULT '',           -- kind/name 或文件路径
  detail_json TEXT NOT NULL DEFAULT '{}',
  ip TEXT
);

-- 托管证书索引（文件在 certs/；索引支撑到期提醒）
CREATE TABLE certificates (
  name TEXT PRIMARY KEY, sans_json TEXT NOT NULL DEFAULT '[]',
  not_after TEXT, updated_at TEXT NOT NULL
);

-- 设置项（draftHash、下发模式、feature flags 等）
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
```

- 迁移策略：内嵌版本化 migration（`schema_migrations` 表），启动时自动升级；M1 只有一个版本，但机制从第一天就位。
- SQLite 运行参数：`journal_mode=WAL`、`foreign_keys=ON`、`busy_timeout=5000`。

## 8. 与相邻模块的契约

| 相邻方 | 契约 |
|---|---|
| M-API（上游，唯一入口） | 配置 CRUD、发布（validate/publish/rollback）、版本与发布历史查询**全部**经 M-CONF 暴露的领域接口，M-API 不直接触碰文件与 DB；冲突语义统一为 409 + 最新 `draftHash`（API 契约细节见 [`260717-3-console-api-design.md`](260717-3-console-api-design.md)） |
| M-COMPILE（下游） | 提供带 `Origin` 的 ConfigSet（§2.1）；消费 `CompileError`（发布流校验结果，带 SourceRef 原样透传 UI）与 `IR.Version`（版本记录与生效确认基准）；产物 diff 复用 `Compile` 重放历史快照 |
| M-DELIVER（下游） | 调用 `Apply(ir)` 触发下发（xDS snapshot 换版 / static 渲染 + M-PROC hot restart）；消费其受理结果与失败原因驱动状态机；下发模式与回报契约见 [`260717-1-deliver-layer-design.md`](260717-1-deliver-layer-design.md) |
| M-STATE（旁路） | 消费生效确认事件（config_dump 中观测到 `IR.Version`）作为 CONFIRMING → EFFECTIVE 的唯一依据；M-STATE 本身只读，不回写配置域（采集模型见 [`260717-4-state-collection-design.md`](260717-4-state-collection-design.md)） |
| M-DISCO（旁路） | 无直接依赖：k8s upstream 候选由 M-API 聚合提供（见 [`260717-5-k8s-disco-design.md`](260717-5-k8s-disco-design.md)）；配置域只持久化 `kubernetesService` 引用本身，对端点动态性无感知（A4） |
| M-PROC（旁路） | static 模式下由 M-DELIVER 间接触发 hot restart；配置域不直接调用 |

## 9. 未决事项

| # | 事项 | 计划决策时机 |
|---|---|---|
| CD1 | 版本保留上限：`versions/` 快照是否全量保留、artifacts 保留最近 N 份（倾向：快照全留——纯文本极小；artifacts 留最近 5 份） | M1 编码期定 |
| CD2 | `esgw.db` 备份策略（WAL 在线备份 API / 定时快照 / 不管） | M2 运维能力建设时定 |
| CD3 | VALIDATED 状态 TTL 与 CONFIRMING 超时默认值（初定 10min / 30s）是否需按部署拓扑区分 | M1 联调期按实测定 |
| CD4 | 外部修改触发的 `external` 作者语义：是否需要把外部修改固化为"外部版本"进入版本历史（当前决策：不进历史，只改草稿态） | M2 收集 git 工作流用户反馈 |
| CD5 | 审计（FR-4.7，P2）写入触发点全集与保留期 | P2 启动时定，表结构已按 §7.1 预留 |

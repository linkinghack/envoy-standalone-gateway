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
| T4 | 接入 bootstrap 渲染 + esgw bootstrap 命令 | 已完成 | T1（T3 后可独立做） | A7 |
| T5 | ADS e2e + CI 集成 + 收口验收 | 已完成 | T3、T4 | A2~A4、A5（日志）、A7、A8 |

状态枚举：`未开始` / `进行中` / `已完成` / `受阻(附原因)`

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-20 | 冲刺创建：需求、技术设计、5 个 task 文档就绪；依赖决策 SD1（go-control-plane 根模块 v0.14.0 + envoy v1.37.0）已实测 MVS 兼容；等待 T1 开工 |
| 2026-07-20 | T1 完成：dev_design/260720-1-mcore-assembly.md + internal/config（LoadFile strict decode + defaults + 校验，含非 loopback listen 硬校验）；commit `ad9f052`（docs）、`10bcacb`（config）；`make build test lint` 全绿 |
| 2026-07-20 | T2 完成：internal/deliver/xds FromIR 纯函数装配（Consistent + SDS 闭合补检）+ go-control-plane v0.14.0 依赖引入；commit `7c0fe64`（deps）、`bbf1fbd`（xds）；`make build test lint` 全绿，go-licenses 通过 |
| 2026-07-20 | T3 完成：internal/deliver Deliverer 接口与状态事件模型、xds ADS server（ACK/NACK 跟踪、事件 fan-out）、internal/core RunServe 与 esgw serve 命令；commit `3987962`（deliver）、`ccd30e9`（xds）、`3051d49`（core,cmd）；`make build test lint` 全绿，deliver/xds 与 core 过 -race |
| 2026-07-20 | T4 完成：xds.RenderBootstrap 纯函数（§2.7 骨架 proto → PGV 自检 → 复用 static 确定性 YAML 路径，发射器导出为 static.MarshalYAML）+ esgw bootstrap 命令 + bootstrap-xds golden；产物经真实 Envoy v1.37.5 `--mode validate` 实测通过；commit `d7c9c79`（static）、`dae9d76`（xds）、`b2a9895`（cmd）、`eb49acc`（test）；`make build test lint` 全绿 |
| 2026-07-20 | T5 完成：e2e/xds ADS 场景落地（共享 netns pod 形态、S1 输入零改写、scratch 容器跑静态 esgw 二进制），A2/A3/A4/A5/A7 断言脚本化并本地全过（envoyproxy/envoy:v1.39.0）；serve 增加 -log-level 开关；make e2e-xds + CI e2e-xds job；`make build test lint` + static e2e + ADS e2e 本地全绿，A1~A8 逐项核验见下表，M0 验收第 ③ 项闭环；commit `1c2ae0f`（cmd）、`72c947e`（e2e）、`95f7b25`（ci）、docs 收口见后续提交；远端 CI 待推送确认。实现取舍 5 条（A4 比较口径、共享 netns 拓扑、EDS 无 version_info、-log-level、adminAddress 形态）见决议记录 |
| 2026-07-20 | A8 收口：推送后远端 CI run 29725492294 五 job（build-test-lint/licenses/validate-matrix/e2e/e2e-xds）全部 success；A1~A8 全部「已核验」，冲刺关闭 |

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
| 接入 bootstrap 产物与 §2.7 骨架的字面出入：`lb_policy: ROUND_ROBIN` 等枚举零值字段被省略 | 接受省略：protojson 不输出零值字段（与 static.Render 产物同一路径、同一风格），语义等价（Envoy 缺省即 ROUND_ROBIN）；骨架其余字段（type: STATIC、connect_timeout: 5s、显式 h2、ads GRPC/V3）均按骨架输出 | 2026-07-20 | T4 进展记录、internal/deliver/xds/bootstrap_test.go 注释 |
| A4「static 产物文件头 config_version == ADS version_info」的字面比较不可行（任务书与设计假设的版本一致性出入） | 实测 static/xds 两模式 IR.Version 不同（S1：static 0ebffff04330 / xds 4ba6bf6b0638）——F5 形态化按模式分流（内联 vs 引用），IR 资源形态不同则内容哈希不同，IR.Version 计算在 F5 之后，属既有设计语义而非缺陷。e2e 断言改为「同一配置目录 compile --mode xds 产物 version == golden want-xds.json version == ADS version_info」——这才是「同一 IR 经 ADS 拉起」的直接证据；requirements A4 的「static 产物」表述建议后续修订为「xds 产物」 | 2026-07-20 | T5 进展记录、e2e/xds/run.sh、本表 A4 行 |
| ADS e2e 拓扑：esgw 运行形态与网络（任务书要求自定） | 四服务（esgw/envoy/两后端）经 network_mode: service:esgw 共享网络命名空间（pod 形态），esgw 以静态 linux 二进制跑在 scratch 容器内。理由：loopback 硬校验下宿主 loopback 对 linux 容器不可达；共享 netns 后 listen 保持 127.0.0.1（与生产单机默认一致），S1 输入的 127.0.0.1 端点与 testdata/certs/ 相对证书路径零改写可用（不需要 static e2e 的 sed/awk 弥合）。adminAddress 取 0.0.0.0:9901（admin 只绑 loopback 时 docker 端口发布不可达；发布侧限定 127.0.0.1:19901；adminAddress 本无 loopback 硬校验） | 2026-07-20 | T5 进展记录、e2e/xds/README.md、e2e/xds/docker-compose.yaml |
| Envoy /config_dump 的 EDS 段不携带 version_info（v1.39.0 实测） | A2 断言对 LDS/CDS/RDS/SDS 直接断言 version_info == IR.Version；EDS 断言资源在册 + @type，版本以 esgw 侧 EDS ACK 日志（version=<IR.Version>，A5 断言）为证 | 2026-07-20 | T5 进展记录、e2e/xds/run.sh check_dump 注释 |
| esgw serve 无日志级别配置，ACK（Debug 级）在 e2e 不可见 | serve 增加 -log-level flag（debug\|info\|warn\|error，默认 info），e2e 以 -log-level debug 启动；不进 esgw.yaml schema（e2e/运维侧临时观测手段，正式日志配置待 S7/S8 统一设计） | 2026-07-20 | T5 进展记录、cmd/esgw/serve.go |

## 验收核验（requirements §4 A1~A8）

| # | 标准 | 结论 | 证据 |
|---|---|---|---|
| A1 | FromIR 纯函数 + Consistent 自检 + 反例 | 已核验（T2） | internal/deliver/xds/snapshot.go + snapshot_test.go：正例 TestFromIR_S1（真实 IR、逐类型断言、Consistent 通过）、反例 TestFromIR_Inconsistent（RDS/EDS/SDS 三类不闭合）；commit `bbf1fbd` |
| A2 | esgw serve + 真实 Envoy ADS 全类型拉取、版本=IR.Version | 已核验（T3 单测层 + T5 e2e 真实 Envoy 层） | 单测：internal/deliver/xds/server_test.go TestApplyServesAllTypes（commit `ccd30e9`）；e2e：e2e/xds/run.sh A2 断言——真实 Envoy v1.39.0 admin /config_dump 五类型资源在册，LDS/CDS/RDS/SDS version_info == IR.Version（4ba6bf6b0638），EDS 资源在册（config_dump 不携带 EDS version_info，其版本由 A5 EDS ACK 日志佐证，见决议记录） |
| A3 | ADS 下 S1 流量四断言 | 已核验（T5 e2e） | e2e/xds/run.sh A3-A~D：www→www-backend、blog→blog-backend、http→301 https、SNI 集合外握手失败，2026-07-20 本地全过（envoyproxy/envoy:v1.39.0） |
| A4 | static 产物与 ADS 下发 IR.Version 一致（M0 第 3 项闭环） | 已核验（T5 e2e；比较口径按决议修订：xds 产物 version == ADS version_info） | e2e/xds/run.sh A4 断言：compile --mode xds 产物 version == golden testdata/s1/want-xds.json version == ADS version_info（均 4ba6bf6b0638）。任务书字面的「--mode static 文件头 == ADS version_info」不成立——F5 形态化按模式分流，static/xds 两模式 IR 资源形态不同、版本串不同（0ebffff04330 vs 4ba6bf6b0638），见决议记录 |
| A5 | ACK 可见 + NACK 单测（不重推） | 已核验（T3 单测层 + T5 e2e 日志层） | 单测：internal/deliver/xds/server_test.go TestNACK（commit `ccd30e9`）；e2e：e2e/xds/run.sh A5 断言——esgw 以 -log-level debug 启动，日志可见五类型 `xds: ACK received`（version=4ba6bf6b0638） |
| A6 | 幂等跳过 | 已核验（T3 单测层） | internal/deliver/xds/server_test.go TestApplyIdempotentSkip：重复 Apply 同 IR 成功、客户端 500ms 内无新推送、Status 不变、无重复 Applied 事件；commit `ccd30e9` |
| A7 | bootstrap 导出被真实 Envoy 接受 + 非 loopback 硬校验 | 已核验（T1 硬校验 + T4 单测层 + T5 e2e 真实 Envoy 层） | 单测层证据见 T4 记录（commit `dae9d76`、`eb49acc`）；e2e：e2e/xds/run.sh A7 断言——真实 Envoy v1.39.0 以 `bin/esgw bootstrap` 导出的接入 bootstrap 启动、admin /ready=LIVE 并承接流量 |
| A8 | CI 全绿、本地可复现 | ✅ 已核验（2026-07-20：本地 make build/test/lint + static e2e + ADS e2e 全绿；推送后远端 CI run 29725492294 五 job 全 success） | CI 新增 e2e-xds job（.github/workflows/ci.yaml）；远端：build-test-lint/licenses/validate-matrix/e2e/e2e-xds 全绿；本地实跑记录见 T5 进展记录 |

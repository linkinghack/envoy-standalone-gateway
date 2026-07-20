# T5：ADS e2e + CI 集成 + 收口验收

- **状态**: 已完成（2026-07-20）
- **依赖**: T3（serve）、T4（bootstrap）
- **验收锚点**: requirements A2~A5（日志部分）、A7（e2e 部分）、A8；闭环 M0 验收第 3 项

## 目标

搭建 ADS 模式的端到端测试：esgw serve 以 xds 模式下发 S1 场景配置，真实 Envoy 以 `esgw bootstrap` 导出的接入 bootstrap 连上 ADS，跑通真实流量断言；与既有 static e2e 并存并纳入 CI。完成后按 requirements §4 逐项核验 A1~A8、更新索引与路线图、闭环 M0 验收第 3 项。

## 上游设计引用

- 冲刺 [requirements.md](../requirements.md) §4（A1~A8）、[technical_design.md](../technical_design.md) SD6
- [下发层设计](../../../../system_design/260717-1-deliver-layer-design.md) §2.6（断连重连语义，e2e 覆盖重连场景可选）
- 现有设施：`e2e/`（static 模式 compose + run.sh + 四断言，尽量复用后端服务与断言函数）、`testdata/s1/`（S1 场景配置与证书）、`.github/workflows/`（CI job 结构）、`Makefile`

## 执行步骤

1. **e2e 拓扑**：新增 `e2e/xds/`（或扩展 e2e/ 根，保持 static e2e 不动；布局取舍记进展记录）：
   - compose 服务：`esgw`（宿主编排构建的 bin/esgw 打进容器或本地二进制挂载，与 static e2e 既有模式对齐——S1 e2e 用的是 envoy 官方镜像 + http-echo 后端，esgw 侧形态参照其做法；注意容器内证书路径与 testdata/s1 的替换表，参考 testdata/s1/README）、`envoy`（官方镜像，版本取支持区间内一个，与 static e2e 同源）、两个 `hashicorp/http-echo` 后端。
   - 编排：先 `bin/esgw bootstrap` 生成接入 bootstrap（adminAddress/listen 用容器网络可达值）→ esgw serve（`-c` esgw.yaml + `-f` S1 配置目录）→ envoy 以该 bootstrap 启动。
   - 时序：envoy 依赖 esgw 就绪（compose `depends_on` + esgw 侧 healthcheck 或断言脚本内重试），不靠固定 sleep（SD6）。
2. **断言**（脚本化，复用 static e2e 的 curl 断言）：
   - A3 四断言：www→www-backend、blog→blog-backend、http→301 https、SNI 集合外域名握手失败；
   - A2：经 envoy admin（compose 内 curl admin 端口/socket 的 `/config_dump`）断言五类型资源在册且版本串 == IR.Version；
   - A4：`bin/esgw compile -f <同目录> --mode static` 产物文件头 `config_version` == ADS 下发的 version_info（M0 验收第 3 项闭环的直接证据）；
   - A5（日志部分）：esgw 日志可见五类型 ACK（若 T3 把 ACK 日志定为 debug 级，e2e 以 debug 日志级别启动 esgw）；
   - A7（e2e 部分）：envoy 进程以导出的 bootstrap 正常启动即达成。
3. **Make/CI**：`make e2e-xds`（或扩展 `make e2e` 跑两个场景，取舍记进展记录；本地默认目标不强制 docker 的既有约定不破）；CI 新增/扩展 e2e job 跑 ADS 场景。build tag `e2e` 约定不变（脚本型 e2e 无需 tag）。
4. **收口**：
   - 逐项填写 plan_todos_trace.md 验收核验表（A1~A8 结论 + 证据）；
   - 更新 `sprints/README.md` 索引状态；
   - 更新路线图 `260719-1-dev-roadmap.md`：S2 行标完成、M0 验收锚点表第 ③ 项标记闭环；
   - 若冲刺内产生决议（SD 表以外的实现取舍），补入决议记录表并回写相关上游文档；
   - `make build test lint e2e`（含 xds）本地全绿后推送，确认远端 CI 全绿。
5. commit 建议拆分：`e2e: ADS xds 场景与断言 (T5)`、`ci: ADS e2e 纳入 CI (T5)`、`docs(sprint): 收口 (T5)`。

## 验收标准

- requirements A1~A8 全部核验「达成」并附证据位置；
- M0 验收第 3 项「同一 IR 经 ADS 正常拉起」在路线图与 M0 核验记录中闭环；
- CI 全绿。

## 进展记录

| 日期 | 记录 |
|---|---|
| 2026-07-20 | task 创建 |
| 2026-07-20 | 完成。① 拓扑取舍（任务书要求自定）：新增 `e2e/xds/` 独立目录，static e2e 不动。四服务（esgw/envoy/两 http-echo 后端）经 `network_mode: service:esgw` 共享 esgw 的网络命名空间（pod 形态）——loopback 硬校验下宿主 loopback 对 linux 容器不可达，共享 netns 后 `deliver.xds.listen` 保持 `127.0.0.1:18000`（与生产默认一致），且 S1 输入的 `127.0.0.1:3000/4000` 端点与 `testdata/certs/` 相对证书路径**零改写**直接可用（static e2e 的 sed/awk 弥合在这里不需要，ADS 与 golden 消费同一份输入）。esgw 跑在容器内：run.sh 按 docker server arch 交叉编译 `CGO_ENABLED=0` 静态 linux 二进制，打入 scratch 镜像（零外部依赖）。adminAddress 取 `0.0.0.0:9901`：envoy admin 只绑 loopback 时 docker 端口发布（DNAT 到 eth0）不可达，发布侧限定 `127.0.0.1:19901`；adminAddress 无 loopback 硬校验。时序全靠断言重试（/ready、config_dump、ACK 日志、流量各 30s），无固定 sleep。② 断言全部落地并本地全过（envoyproxy/envoy:v1.39.0）：A7（/ready=LIVE）、A2（/config_dump 五类型在册，LDS/CDS/RDS/SDS version_info == IR.Version=4ba6bf6b0638；**EDS 的 config_dump 不携带 version_info**（v1.39.0 实测），断言资源在册+@type，版本由 A5 的 EDS ACK 日志佐证）、A4（见下条口径修订）、A5（五类型 `xds: ACK received`）、A3 四断言。③ **A4 口径修订**：任务书字面的「`compile --mode static` 文件头 config_version == ADS version_info」实测不成立——F5 形态化按模式分流，static/xds 两模式 IR 资源形态不同、IR.Version 不同（S1：static 0ebffff04330 / xds 4ba6bf6b0638；版本计算在 F5 之后，属既有设计语义）。e2e 改为断言「同一配置目录 compile --mode xds 产物 version == golden want-xds.json version == ADS version_info」，这是「同一 IR 经 ADS 拉起」（M0 第 3 项）的直接证据；requirements A4 的「static 产物」表述建议后续修订，已记决议。④ A5 前提补齐：T3 把 ACK 定为 Debug 级而 serve 无级别配置，为 `esgw serve` 增加 `-log-level` flag（默认 info，非法值 exit 2；不进 esgw.yaml schema，正式日志配置待 S7/S8）。⑤ Make/CI：新增独立 `make e2e-xds` 目标与 CI `e2e-xds` job（与 static e2e 并列，取舍：两个场景拓扑/前置差异大，分开目标各自独立可跑，比合并单目标清晰；本地默认目标不强制 docker 的约定不破）。⑥ 收口：plan_todos_trace 核验表 A1~A8 全填、路线图 S2 行标完成 + M0 第 ③ 项闭环（2026-07-20，S2 e2e）、sprints 索引与 requirements 状态行更新。本地 `make build test lint` + static e2e + ADS e2e 全绿；commit `1c2ae0f`（cmd -log-level）、`72c947e`（e2e/xds）、`95f7b25`（ci）；**远端 CI 待推送确认**（按会话约束未 push） |

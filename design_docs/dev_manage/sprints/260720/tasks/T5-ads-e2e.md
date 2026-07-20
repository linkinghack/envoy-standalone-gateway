# T5：ADS e2e + CI 集成 + 收口验收

- **状态**: 未开始
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

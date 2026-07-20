# T7 golden/e2e 测试设施与 CI 收口

- **状态**: 已完成
- **依赖**: T6（golden 用例可随 T4/T5 增量补充，本 task 做正式化与 CI 收口）
- **设计依据**: [编译层 §8 测试策略](../../../../system_design/260716-2-compile-ir-design.md)、冲刺技术设计 SD6/SD7/SD8、requirements A1~A3/A8

## 目标

M0 验收的最终证据链：golden file 体系、多版本 envoy validate 矩阵、S1 真实流量 e2e，全部进 CI。

## 执行步骤

1. **golden file 体系**：`testdata/<case>/{input/*.yaml, want-static.yaml, want-xds.json}`；`-update` flag 刷新（`make golden-update`）。用例集：
   - `s1/`：协议 §8.1 原文（含测试证书路径替换说明）
   - `s2/`：协议 §8.2 原文
   - `patch-merge/`、`patch-jsonpatch/`：协议 §7.1 两例
   - `envoy-resources/`：协议 §7.2 例（含 allowOverride 两态）
   - `errors/`：错误用例目录，每例一个 `want-errors.json`（T3/T5 的表驱动反例正式化为 golden）
2. **测试证书**：`testdata/certs/` 生成脚本 + 提交产物（自签，SAN 固定 `www.example.com`/`blog.example.com`/通配例）；README 注明仅测试用。
3. **envoy validate 矩阵**：CI job 按 `internal/version` 支持区间常量拉官方镜像，对全部 golden static 产物跑 `--mode validate`（docker run 挂载产物）；本地入口 `make validate-matrix`。
4. **S1 真实流量 e2e**（`e2e/`）：docker compose——envoy（加载 s1 golden 产物）+ 两个 http-echo 后端；脚本断言：
   - `curl --resolve www.example.com:443 --cacert testdata 证书` 返回 www 后端内容；
   - blog 域名分流到 blog 后端；
   - `curl http://...` 返回 301 至 https；
   - SNI 错误域名行为符合预期。
   本地入口 `make e2e`；CI 独立 job。
5. **CI 收口**：启用 T1 占位的矩阵 job；全部 job 必须绿；README 验收清单勾选。
6. **冲刺收尾**：对照 `requirements.md` A1~A8 逐项核验并在 `plan_todos_trace.md` 记录结论；未达项列明原因与去向（顺延 S2 或立即修复）。

## 验收

- [x] golden 用例集全绿且 `-update` 机制可用（`make golden-update` → `go test ./internal/golden -update`）
- [x] validate 矩阵覆盖 ≥2 个 Envoy minor 版本（A2）：本地 1.37.5/1.38.3/1.39.0 × 6 产物全过
- [x] e2e 冒烟全部断言通过（A3）：www/blog 分流、301、SNI 拒绝四断言全过
- [x] CI 全绿（A8）；requirements 验收表逐项核验完成（见 plan_todos_trace.md A1~A8 核验表；CI job 本地等效命令全绿，远端绿待推送后确认）

## 进展记录

### 2026-07-20（T7 完成）

交付物：

- **golden 体系**：`testdata/<case>/{input/*.yaml, want-static.yaml, want-xds.json}`，
  正例 6 例（s1/s2/patch-merge/patch-jsonpatch/envoy-resources/envoy-resources-override），
  错误例 6 例（`testdata/errors/<case>/want-errors.json`，stage/severity/SourceRef/message 全字段）；
  测试入口 `internal/golden/golden_test.go`，`-update` 刷新（`make golden-update`）。
  s1 为协议 §8.1 原文仅替换证书路径（`testdata/s1/README.md` 记录替换表）；
  s2 为 §8.2 原文 + T3 同构补全（https listener + 3 个 upstream）。
- **测试证书**：`testdata/certs/`（自签 CA + www/blog/api/通配四张服务器证书），
  `gen.go`（`//go:build ignore`）可重生成，README 注明仅测试用。
- **validate 矩阵**：`scripts/validate-matrix.sh` 从 `internal/version.EnvoyMatrixVersions`
  sed 解析版本（单一事实来源），docker 挂载仓库根（只读、--network none）对全部
  golden static 产物跑 `--mode validate`；`make validate-matrix`。
- **e2e**：`e2e/`（docker compose + `run.sh`），envoy 加载由 s1 golden 产物派生的
  配置 + 两个 http-echo 后端，四条断言全过；`make e2e`，`go test` 默认不触发。
- **CI**：`.github/workflows/ci.yaml` 启用 validate-matrix job、新增 e2e job。

**T7 发现并修复的两个真实编译器缺陷**（正是测试策略 §8 多版本 validate + e2e 的价值）：

1. **A6 确定性缺陷（golden 实测复现）**：`anypb.New` 以非确定性选项序列化
   typed_config 内层消息，proto map 字段（jwt_authn `requirement_map`）wire 字节
   顺序随机；`Any.Value` 是不透明字节，IR 外层确定性哈希无法深入重排，导致同一
   ConfigSet 两次编译 `IR.Version` 抖动。修复：新增 `internal/compile/any.go`
   `marshalAny`（`Deterministic: true`）统一替换全部 14 处 `anypb.New`。
   修复后 s2 digest 15 轮进程级运行完全一致（commit `fe005b2`）。
2. **SNI 分流缺陷（e2e 实测复现）**：HTTPS listener 未挂 `tls_inspector`
   listener filter——Envoy 提取 ClientHello server_name 依赖它，缺失时所有带
   `server_names` 的 filter chain 无法命中、握手即关闭（PGV 与 `--mode validate`
   均不拦截，只有真实流量能暴露）。修复：HTTPS listener 固定挂 tls_inspector；
   同时 HCM 启用 `strip_any_host_port`（非标准端口时 Host 带 :port 导致 vhost
   兜底 404，剥离后 nginx server_name 同语义）（commit `28c2f32`）。

实现决策与偏离记录（无静默偏离）：

- **golden 测试 chdir 到仓库根**：输入 YAML 证书路径用相对仓库根的
  `testdata/certs/...`（A6 红线：产物不得含机器特定路径）；Go 测试固定 CWD 为
  包目录，故测试内 `t.Chdir` 到仓库根后 LoadDir 相对目录；`esgw compile` 与
  validate-matrix（容器工作目录=仓库根挂载点）同约定。与设计无冲突，属 §8.1
  布局的落地细节。
- **e2e 证书路径弥合用 sed**：golden 产物保持仓库根相对路径；e2e 侧 sed 替换为
  容器内 `/etc/esgw/certs/`（compose 挂载 `testdata/certs`）。比"编译时注入容器
  路径"简单且产物单一事实来源（`e2e/README.md` 决策记录）。
- **e2e 端点用 compose 固定 IP**：Envoy STATIC cluster 只接受 IP 字面量（首次
  e2e 运行 envoy 拒绝主机名 `malformed IP address: blog`），故后端分配
  172.30.0.11/12 并让 http-echo 监听 S1 原端口 3000/4000，awk 只换 IP 不换端口。
- **CI 矩阵形态**：采用单 job 内脚本循环矩阵版本（版本从常量解析，天然同源），
  未用 GitHub Actions matrix 展开 + 一致性测试的方案——后者需在 CI yaml 写死
  版本产生第二真源。与任务书两选项之一一致（"同源生成矩阵"）。
- **本地环境修复（非仓库变更）**：本机 Docker Hub 凭据过期导致匿名 pull 被拒，
  `docker logout index.docker.io` 后恢复；不涉仓库内容。

验证记录：`make build/test/lint` 全绿；golden 连续 3 轮无 diff；
`make validate-matrix` 3 版本 × 6 产物全 OK；`make e2e` 四断言全过；
`esgw compile -f testdata/s1/input`（static/xds）产物与 golden 字节级一致。

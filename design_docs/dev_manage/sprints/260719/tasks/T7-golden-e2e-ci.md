# T7 golden/e2e 测试设施与 CI 收口

- **状态**: 未开始
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

- [ ] golden 用例集全绿且 `-update` 机制可用
- [ ] validate 矩阵覆盖 ≥2 个 Envoy minor 版本（A2）
- [ ] e2e 冒烟全部断言通过（A3）
- [ ] CI 全绿（A8）；requirements 验收表逐项核验完成

## 进展记录

（接手会话在此追加）

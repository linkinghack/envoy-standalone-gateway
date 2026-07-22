# T6：P1 数据面真实矩阵与收口

## 目标

用真实 Envoy 证明本 Sprint 能力在 static/xDS 与支持版本内一致，关闭 S9。

## 步骤

1. 复用无 bind-mount e2e 基础设施；2. L4/策略/mTLS 流量矩阵；3. 全量回归；4. 文档与兼容说明；5. A1–A8、roadmap/index 收口提交。

## 进展

- 已完成：新增 `e2e/p1-xds` 组合场景，所有运行文件通过 `docker cp` 注入、无宿主 bind mount；同一份 P1 配置在 Envoy 1.37.5、1.38.3、1.39.0 上先做 static validate，再连接真实 ADS。
- 已完成：每个版本均核验 LDS/RDS/CDS/SDS `version_info` 与编译版本一致、EDS 在册、`crt/mtls/0` 与 `ca/mtls` Secret 在册；管理面五类 ACK 完整且无 NACK。
- 已完成：组合流量逐版本覆盖 TCP、TLS passthrough 双 SNI/未知 SNI、UDP、HTTP/gRPC extAuth、fail-open/fail-closed、IP/XFF 边界、clientIP/header 独立配额、downstream mTLS 与 circuit-breaker 503 overflow。
- 已完成：分项 `e2e/l4`、`e2e/extauth`、`e2e/policy`、`e2e/mtls-circuit` 全部复跑通过；支持矩阵 3 × 10 static artifacts validate 全通过。
- 已完成：Go gofumpt/build/unit/race/vet/golangci-lint/300 轮 patch fuzz smoke、协议仓库外 clean-diff/conformance、文档检查全部通过。
- 已完成：Web generate/typecheck、3 文件 5 条 Vitest、生产构建以及桌面/移动 Playwright 5 passed + 1 个项目条件 skip 通过；生成物 clean diff。
- 已完成：更正 SDS 能力边界——P1 Secret 仍使用 filename DataSource，数据面必须落地对应材料；跨主机内联/独立 Secret 材料传输留给 P2。

## 验收

- A7：`make e2e-p1-xds` 在全部三个支持版本完成 static/ADS 与 P1 全流量闭环。
- A8：Sprint 全量 Go/Web/协议/Envoy/文档门禁通过，工作树仅包含本任务证据文档更新。

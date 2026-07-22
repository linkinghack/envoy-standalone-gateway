# Sprint 260726 任务拆分与进展追踪

| ID | 任务 | 状态 | 验收 |
|---|---|---|---|
| T1 | systemd、安装/升级/卸载与权限 | 已完成 | shell tests + unit security checks |
| T2 | 两类镜像、compose 与 healthcheck | 进行中 | image/config health smoke |
| T3 | 四组合真实 Envoy e2e | 待开始 | T1/T2 × xDS/static matrix |
| T4 | 快速开始与运维文档 | 待开始 | command/link/docs checks |
| T5 | M1 矩阵、资源、许可证/SBOM、发行收口 | 待开始 | release artifacts + full gates |

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-22 | S7 收口后启动 S8；复核 FR-5、架构 §5/§7 与 M1 工程计划，冻结 AD8、Linux 布局、systemd KillMode、容器用户和供应链边界，T1 开始。 |
| 2026-07-22 | T1 完成：systemd hardening、原子安装/升级、配置与数据保留、显式 purge 均由 staged lifecycle 测试覆盖；T2 开始。 |

## 验收核验

| ID | 状态 | 证据 |
|---|---|---|
| A1 Linux 安装/权限 | 已核验 | `packaging/tests/install_test.sh`、`systemd-analyze verify` |
| A2 systemd 数据面保持 | 已核验 | `KillMode=process`；S7 `e2e/static-managed/run.sh` 的管理面退出/接管断言 |
| A3 两类镜像/compose | 待核验 | — |
| A4 四组合 e2e | 待核验 | — |
| A5 10 分钟快速开始 | 待核验 | — |
| A6 备份/恢复/升级/安全 | 待核验 | — |
| A7 release/SBOM/licenses | 待核验 | — |
| A8 M1 矩阵/全量门禁 | 待核验 | — |

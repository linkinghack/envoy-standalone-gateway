# Sprint 260726 任务拆分与进展追踪

| ID | 任务 | 状态 | 验收 |
|---|---|---|---|
| T1 | systemd、安装/升级/卸载与权限 | 已完成 | shell tests + unit security checks |
| T2 | 两类镜像、compose 与 healthcheck | 已完成 | image/config health smoke |
| T3 | 四组合真实 Envoy e2e | 已完成 | T1/T2 × xDS/static matrix |
| T4 | 快速开始与运维文档 | 已完成 | command/link/docs checks |
| T5 | M1 矩阵、资源、许可证/SBOM、发行收口 | 已完成 | release artifacts + full gates |

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-22 | S7 收口后启动 S8；复核 FR-5、架构 §5/§7 与 M1 工程计划，冻结 AD8、Linux 布局、systemd KillMode、容器用户和供应链边界，T1 开始。 |
| 2026-07-22 | T1 完成：systemd hardening、原子安装/升级、配置与数据保留、显式 purge 均由 staged lifecycle 测试覆盖；T2 开始。 |
| 2026-07-22 | T2 完成：healthcheck、两类 uid 65532 镜像、all-in-one/separated compose 及真实运行层 smoke 通过；T3 开始。 |
| 2026-07-22 | T3 完成：四组合真实 Envoy 矩阵连续流量通过；验证 managed PID 接管、external 隔离和 file-only static 显式重启边界；T4 开始。 |
| 2026-07-22 | T4 完成：10 分钟 quickstart、配置、运维、备份恢复、升级与安全文档齐备，示例编译和三份 compose 配置门禁通过；T5 开始。 |
| 2026-07-22 | T5 与 S8 完成：双架构发行物/许可证/SPDX、29,016 KiB 稳态 RSS、M1 P0 证据矩阵及全量 Go/Web/Envoy/交付门禁通过，M1 收口。 |

## 验收核验

| ID | 状态 | 证据 |
|---|---|---|
| A1 Linux 安装/权限 | 已核验 | `packaging/tests/install_test.sh`、`systemd-analyze verify` |
| A2 systemd 数据面保持 | 已核验 | `KillMode=process`；S7 `e2e/static-managed/run.sh` 的管理面退出/接管断言 |
| A3 两类镜像/compose | 已核验 | `packaging/tests/image_smoke.sh`、三份 compose `config --quiet` |
| A4 四组合 e2e | 已核验 | `e2e/topology-matrix/run.sh`、`e2e/static-managed/run.sh` |
| A5 10 分钟快速开始 | 已核验 | `docs/quickstart.md`、`packaging/compose/quickstart.yaml`、`make docs-test` |
| A6 备份/恢复/升级/安全 | 已核验 | `docs/operations.md`、`docs/backup-restore.md`、`docs/upgrade.md`、`docs/security.md` |
| A7 release/SBOM/licenses | 已核验 | `packaging/release/build.sh`；双归档、397 行 CSV、SPDX 与 SHA256 实测通过 |
| A8 M1 矩阵/全量门禁 | 已核验 | `extra_docs/m1-requirements-matrix.md`；Go/Web/Envoy/交付全门禁通过 |

# T4：用户与运维文档

## 目标

让裸机和 Docker 用户从零到首个代理不超过 10 分钟，并明确日常运维、安全、备份恢复和升级流程。

## 步骤

1. 快速开始与配置概念；
2. systemd/Docker 安装与拓扑选择；
3. 日常运维、故障诊断、备份/恢复/升级；
4. 安全清单和已知 M1 边界；
5. 链接/命令检查、trace 与独立提交。

## 进展

- 已完成：根 README 指向 M1 用户入口，`docs/` 提供快速开始、配置/拓扑、运维排障、备份恢复、升级回滚和安全边界六份主题文档。
- Docker quickstart 从空 volume 初始化真实三对象代理，提供控制台登录与一条可断言的 curl；Linux 路径覆盖 Envoy 兼容范围、安装、密码注入和首次配置。
- 运维文档明确 systemd 管理面重启保持数据面、all-in-one 生命周期边界，以及 external static 必须显式重启的语义。
- `make docs-test` 实际编译 quickstart 配置并解析三份 compose，同时检查关键文档链接/安全提示；与 `make packaging-test` 一并通过。

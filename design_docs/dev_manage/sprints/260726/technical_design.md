# Sprint 260726 技术设计

## 1. 交付决策

| 决策 | 结论 |
|---|---|
| AD8 默认监听 | 裸机示例保持 `127.0.0.1:8080`；容器配置显式使用 `0.0.0.0:8080` 并建议宿主绑定 loopback。初始管理员只允许 `ESGW_INITIAL_ADMIN_PASSWORD` 注入或 30 分钟引导窗口，不写入镜像。 |
| Linux 布局 | binary `/usr/local/bin/esgw`，配置 `/etc/esgw/esgw.yaml`，数据 `/var/lib/esgw`；日志进 journald。运行用户/组均为 `esgw`。 |
| systemd 生命周期 | `Type=simple`、`KillMode=process`：restart/stop 只信号 MainPID，独立 launcher/Envoy 留在 cgroup 内供新管理面接管；`NoNewPrivileges`、只写数据目录，按需授予 `CAP_NET_BIND_SERVICE`。 |
| 容器用户 | 固定 uid/gid 65532；volume 在镜像内预建并要求宿主目录匹配权限。管理面镜像不含 Envoy且 `proc.enabled=false`；all-in-one 固定官方 Envoy 1.39 系列并显式启用 proc。 |
| 健康检查 | 增加无 shell 依赖的 `esgw healthcheck --url`，Docker HEALTHCHECK 与运维探针复用 `/readyz`。 |
| 供应链 | 发行脚本在干净临时目录交叉构建，归档包含 binary、unit、示例配置、安装脚本、LICENSE；生成 SHA256、Go/npm 许可证报告与 SPDX JSON SBOM。工具缺失必须失败并给出安装提示。 |

## 2. T1/T2 × 模式矩阵

| 组合 | 进程归属 | 配置生效 |
|---|---|---|
| T1 xDS | esgw launcher 托管 Envoy | ADS snapshot，发布零重启 |
| T1 static | esgw launcher 托管 Envoy | 原子文件 + Envoy hot restart |
| T2 xDS | 外部 Envoy 容器/unit | esgw 仅 ADS 下发，不发信号 |
| T2 static | 外部 Envoy 容器/unit | esgw 仅原子写共享文件；外部管理器显式重启后生效 |

所有组合都验证管理面停止后现有代理流量继续；T2 static 明确不把“文件写入”等同于“外部 Envoy 已生效”。

## 3. 安装与升级事务

安装脚本先校验 binary/config 输入，再创建用户和目录，通过临时文件 + rename 安装 unit/binary。已有配置永不覆盖，只补 `.example`；升级先复制当前 binary 为带时间戳备份，替换后 `daemon-reload` 并 restart 管理面。卸载停止/disable unit、移除 binary/unit，只有显式 `--purge` 才删除配置、数据和用户。

备份边界是 `/etc/esgw` 与 `/var/lib/esgw` 的一致副本。备份前暂停管理面写入但不停止 Envoy；SQLite 使用 checkpoint 后复制。恢复要求停止管理面、校验所有权和 0600 私钥，再启动并观察 `/readyz`。

## 4. 质量与发布

脚本测试使用临时 `DESTDIR`/mock systemctl，不改宿主。Docker e2e 通过 tar context 避免 WSL bind mount 依赖。最终执行 Go/Web 全门禁、Envoy validate matrix、四组合 e2e、shell 语法检查、镜像配置检查、发布归档内容/SBOM/许可证断言。

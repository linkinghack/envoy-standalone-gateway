# 日常运维与故障定位

## 健康与日志

```sh
esgw healthcheck --url http://127.0.0.1:8080/readyz --timeout 2s
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
sudo journalctl -u esgw -f
```

`/healthz` 表示管理 HTTP 进程存活；`/readyz` 只在初始配置加载、交付器、监听器和托管 Envoy（若启用）就绪后返回 2xx。容器 HEALTHCHECK 直接调用同一个无 shell 的 `esgw healthcheck`。

## systemd 常用操作

```sh
sudo systemctl status esgw
sudo systemctl restart esgw
sudo journalctl -u esgw --since '30 minutes ago'
sudo -u esgw /usr/local/bin/esgw version
```

unit 使用 `KillMode=process`。停止或重启只向管理 MainPID 发信号，托管 launcher/Envoy 继续承载最后有效配置；新管理进程根据 `proc.json` 和 Envoy admin 实际状态接管。不要用 `systemctl kill --kill-who=all` 代替正常重启。

## Docker 常用操作

```sh
docker compose -f packaging/compose/all-in-one.yaml ps
docker compose -f packaging/compose/all-in-one.yaml logs -f gateway
docker compose -f packaging/compose/all-in-one.yaml exec gateway esgw healthcheck
```

all-in-one 容器重启会同时重建容器内数据面网络环境；需要严格管理面/数据面故障隔离时使用 separated 拓扑或 systemd。

## 定位顺序

1. `/healthz` 失败：检查进程、端口占用、文件权限和日志。
2. `/healthz` 成功但 `/readyz` 失败：检查配置 strict decode、Envoy 可执行文件/版本、admin socket 和 `proc.json`。
3. 发布失败：先看控制台诊断的 load/link/build/validate/envoy 阶段；失败发布不会替换 last-good。
4. 管理面正常但代理失败：检查 Listener 端口、Host/SNI、Upstream DNS/端点和 Envoy admin 的 listener/cluster 状态。
5. xDS 未生效：以 ACK/NACK、`config_dump` 的版本和状态页为准；不要只看文件时间。
6. external static 未生效：确认外部服务管理器已显式重启 Envoy。

托管模式反复崩溃会按 `restartBackoff` 退避并在 10 分钟预算耗尽后停止自动重启，避免 crash loop 占满主机。

# 升级与回滚

## systemd

先做一致备份并阅读发行说明，然后运行：

```sh
make build
sudo packaging/scripts/upgrade.sh ./bin/esgw
curl -fsS http://127.0.0.1:8080/readyz
sudo journalctl -u esgw --since '5 minutes ago'
```

升级脚本不会覆盖 `/etc/esgw/esgw.yaml` 或 `/var/lib/esgw`；它把现有 binary 保存为 `/usr/local/bin/esgw.previous.<UTC timestamp>`，原子替换新 binary，reload unit 并显式 restart 管理面。托管 Envoy 应继续服务并被新进程接管。

若 readiness 或兼容检查失败，选择最新备份回滚：

```sh
sudo systemctl stop esgw
sudo install -m 0755 /usr/local/bin/esgw.previous.YYYYMMDDHHMMSS /usr/local/bin/esgw
sudo systemctl start esgw
curl -fsS http://127.0.0.1:8080/readyz
```

涉及持久化 schema 的跨版本回滚应同时恢复升级前的完整备份，不能只换 binary。

## Docker

镜像标签应固定到版本而非 `latest`。升级前备份 volume，拉取/构建新标签，检查 compose diff 后重建：

```sh
docker compose -f packaging/compose/all-in-one.yaml config
docker compose -f packaging/compose/all-in-one.yaml up -d --build
docker compose -f packaging/compose/all-in-one.yaml ps
```

all-in-one 重建会重启数据面；需要零数据面重启的升级路径应把 Envoy 外置。回滚时恢复旧镜像标签；若新版本已迁移数据，再一并恢复旧 volume 备份。

# 备份与恢复

完整备份边界是 `/etc/esgw` 与 `/var/lib/esgw`。其中包含进程配置、配置真源/版本快照、SQLite 账号与审计数据、托管证书私钥和进程记录，必须按敏感数据加密保存。

## systemd 一致备份

正常停止管理面会关闭 SQLite 并完成 WAL 收尾，同时 `KillMode=process` 保留托管 Envoy 的最后有效数据面：

```sh
sudo systemctl stop esgw
sudo tar --numeric-owner --xattrs -C / -cpf esgw-backup.tar etc/esgw var/lib/esgw
sudo systemctl start esgw
curl -fsS http://127.0.0.1:8080/readyz
```

不要只复制 `esgw.db` 而遗漏 `-wal`/`-shm`、证书或配置文件；复制整个数据目录是稳定边界。

## systemd 恢复

在干净主机安装相同或更新的兼容版本，然后：

```sh
sudo systemctl stop esgw
sudo tar --numeric-owner --xattrs -C / -xpf esgw-backup.tar
sudo chown -R esgw:esgw /var/lib/esgw /etc/esgw
sudo find /var/lib/esgw/certs -type d -exec chmod 0700 {} +
sudo find /var/lib/esgw/certs -name tls.key -type f -exec chmod 0600 {} +
sudo systemctl start esgw
curl -fsS http://127.0.0.1:8080/readyz
```

恢复后检查版本、状态页、Listener/Cluster 与一条真实代理流量，再删除旧备份。

## Docker volume

先用 `docker volume ls` 确认实际 volume 名。为获得一致副本，应停止写入该 volume 的 gateway，再用一次性容器打包；all-in-one 会在此窗口中断代理流量：

```sh
docker compose -f packaging/compose/all-in-one.yaml stop gateway
docker run --rm -v YOUR_ESGW_VOLUME:/data:ro -v "$PWD":/backup alpine \
  tar -C /data -cpf /backup/esgw-volume.tar .
docker compose -f packaging/compose/all-in-one.yaml start gateway
```

恢复时先停止 gateway、清空目标空 volume 后解包并把所有者修复为 `65532:65532`，再启动并检查 `/readyz`。不要在有运行实例写入时覆盖 volume。

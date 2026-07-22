# 10 分钟快速开始

## Docker：一条主路径

要求 Docker Engine/Compose 可用。克隆仓库后执行：

```sh
export ESGW_INITIAL_ADMIN_PASSWORD='replace-with-a-strong-password'
docker compose -f packaging/compose/quickstart.yaml up -d --build
docker compose -f packaging/compose/quickstart.yaml ps
curl -fsS -H 'Host: quickstart.local' http://127.0.0.1:10080/
```

最后一条命令应输出 `hello-from-esgw`。管理控制台位于 <http://127.0.0.1:8080/>，用户名为 `admin`，密码是上面注入的值。`quickstart.yaml` 会把示例配置仅初始化一次；后续重启不会覆盖控制台内的修改。

若不注入密码，可在启动后 30 分钟内通过控制台创建首个管理员。不要把未初始化的 8080 端口暴露到不可信网络。

清理演示环境：

```sh
docker compose -f packaging/compose/quickstart.yaml down
# 同时删除配置、账号和证书时才使用：down -v
```

## Linux + systemd

支持 Envoy 1.37–1.39。先把 Envoy 安装到 `PATH`，然后从源码构建并安装：

```sh
make build
sudo packaging/scripts/install.sh ./bin/esgw
sudo install -m 0600 packaging/config/esgw.env.example /etc/esgw/esgw.env
sudoedit /etc/esgw/esgw.env
sudo systemctl restart esgw
curl -fsS http://127.0.0.1:8080/readyz
```

在 `esgw.env` 设置至少 12 字符的 `ESGW_INITIAL_ADMIN_PASSWORD`。默认使用 xDS + 本机托管 Envoy；管理 API、ADS 和 Envoy admin 都只在 loopback/Unix socket 上开放。

创建首个代理可把 [`quickstart-gateway.yaml`](../packaging/examples/quickstart-gateway.yaml) 中的上游改成真实地址后复制到配置真源：

```sh
sudo install -o esgw -g esgw -m 0640 packaging/examples/quickstart-gateway.yaml /var/lib/esgw/config.d/gateway.yaml
sudo systemctl restart esgw
```

生产变更推荐在控制台中编辑、校验并发布；直接修改文件适合首次引导和故障恢复。

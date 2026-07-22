# e2e — S1 场景真实流量冒烟（验收 A3）

`run.sh` 用 docker compose 拉起真实 Envoy + 两个 `hashicorp/http-echo` 后端，
对 S1（多域名 TLS 反代）做流量级断言：

| 断言 | 内容 |
|---|---|
| A | `https://www.example.com`（`--resolve` + `--cacert` 测试 CA）返回 www 后端内容 |
| B | `https://blog.example.com` 分流到 blog 后端 |
| C | `http://www.example.com` 返回 301 跳转 https |
| D | SNI 为证书域名集合外的域名时握手失败（无匹配 filter chain） |

## 运行

```sh
make e2e        # 或 e2e/run.sh
```

ADS 模式（esgw serve + 接入 bootstrap）的对应 e2e 见 [`xds/`](xds/)
（`make e2e-xds`，Sprint 260720 T5）。

L4 场景见 [`l4/`](l4/)（`make e2e-l4`）：使用同一份 L4 golden static
产物验证 TCP、两条 TLS passthrough SNI 分流、未知 SNI 拒绝和 UDP 回包。

默认 `go test ./...` 不触发（工程基线 §3：e2e 依赖 docker）。

## 关键决策

- **Envoy 配置来源**：由 golden 产物 `testdata/s1/want-static.yaml` 派生，
  与 golden 测试、validate-matrix 消费同一份产物（单一事实来源）。
- **证书路径弥合**：golden 产物必须保持仓库根相对路径（`testdata/certs/`，
  A6 确定性红线）；e2e 侧用 `sed` 替换为容器内挂载路径 `/etc/esgw/certs/`，
  compose 把 `testdata/certs` 只读挂载到该路径。这比"编译时注入容器路径"
  简单，且不污染产物。
- **端点改写**：S1 输入中的 `127.0.0.1:3000/4000` 在 compose 网络中不可达；
  且 Envoy `STATIC` cluster 只接受 IP 字面量（不接受主机名），故 compose
  给后端分配固定 IP 并让 http-echo 监听原端口（www=`172.30.0.11:3000`、
  blog=`172.30.0.12:4000`），`awk` 只需按端口把 `127.0.0.1` 替换为对应
  固定 IP。
- **Envoy 镜像版本**：默认取 `internal/version.EnvoyMatrixVersions` 最后
  一个版本（与 validate 矩阵同源），`ENVOY_IMAGE` 可覆盖。
- **SD8**：S2 场景只做 `--mode validate` 级验证（见 validate-matrix），
  真实流量断言留 S2 冲刺 ADS e2e 一并做。

生成物落在 `e2e/generated/`（已 gitignore）。端口可用
`E2E_HTTP_PORT`（18080）/`E2E_HTTPS_PORT`（18443）覆盖。

# testdata/s1 — S1 场景 golden 用例（协议 §8.1，M0 验收 A1/A2/A3）

`input/config.yaml` 是协议文档
[`260716-1-gateway-config-protocol-v0.md` §8.1](../../design_docs/system_design/260716-1-gateway-config-protocol-v0.md)
的演练 YAML 原文，**唯一改动**是证书路径：

| 文档原文 | 本用例 |
|---|---|
| `/etc/esgw/certs/www.crt` | `testdata/certs/www.crt` |
| `/etc/esgw/certs/blog.crt` | `testdata/certs/blog.crt` |

替换原因：编译期 F2 会校验证书文件存在与密钥配对，F3 需从证书 SAN 提取
SNI 域名，仓库内必须有真实证书文件。测试证书为自签（仅测试用），见
[`../certs/README.md`](../certs/README.md)。

路径为相对仓库根的相对路径：golden 测试在仓库根目录下运行（测试内
chdir），`esgw compile -f testdata/s1/input` 同样从仓库根执行——产物中
证书路径原样保留，保证 golden 内容不含机器特定路径（验收 A6 红线）。

产物消费方：
- `want-static.yaml` / `want-xds.json`：golden 快照（`make golden-update` 刷新）。
- `make validate-matrix`：docker 挂载仓库根跑 `envoy --mode validate`。
- `make e2e`：sed 把 `testdata/certs/` 替换为容器内 `/etc/esgw/certs/`、
  把两个 127.0.0.1 端点替换为 compose 服务名后，由真实 Envoy 加载跑流量断言
  （见 `e2e/`）。
- `make e2e-xds`：input 目录**零改写**直接由 esgw serve（xds 模式）加载，
  经 ADS 下发给共享网络命名空间的真实 Envoy（127.0.0.1 端点与相对证书
  路径在共享 netns 内原样成立，见 `e2e/xds/`）。

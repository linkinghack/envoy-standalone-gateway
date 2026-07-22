# testdata/certs — 测试证书（仅测试用，禁止用于任何真实部署）

本目录提交的是**自签测试证书**，供 golden 快照测试、`make validate-matrix` 与
`make e2e` 使用。私钥随仓库公开提交，无任何安全意义。

| 文件 | 说明 |
|---|---|
| `ca.crt` | 自签测试 CA（CN=`esgw-test-ca`）；e2e 中 `curl --cacert` 用它 |
| `www.crt` / `www.key` | SAN `www.example.com`，CA 签发；S1 场景 www 站点 |
| `blog.crt` / `blog.key` | SAN `blog.example.com`，CA 签发；S1 场景 blog 站点 |
| `api.crt` / `api.key` | SAN `api.example.com`，CA 签发；S2 场景入口 |
| `wildcard.crt` / `wildcard.key` | SAN `*.example.com`（通配例），CA 签发；备后续通配域名用例 |
| `client-ca.crt` | 下游 mTLS 专用 CA（CN=`esgw-test-client-ca`） |
| `client.crt` / `client.key` | client CA 签发且仅含 ClientAuth EKU 的可信测试客户端身份 |

## 重新生成

```sh
go run testdata/certs/gen.go   # 仓库根目录执行
go run testdata/certs/gen.go -client-only # 只重建客户端 CA/身份
```

脚本（`gen.go`，`//go:build ignore`，不进入任何包构建）每次运行随机生成密钥；
仓库提交的是产物本身。golden 快照只引用证书**路径**与编译器从证书提取的
SAN，与密钥内容无关，因此重生成不会使快照抖动。证书有效期 2026-01-01 至
2046-01-01。

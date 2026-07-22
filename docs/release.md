# 发行物校验

发行构建要求 Go、Node.js、`go-licenses`、`syft`、GNU tar 和 `sha256sum`。输出目录必须不存在，脚本不会自动删除或覆盖旧发行物：

```sh
VERSION=1.0.0 DIST_DIR="$PWD/dist/1.0.0" make release
cd dist/1.0.0
sha256sum -c SHA256SUMS
```

每个 linux/amd64、linux/arm64 归档包含静态 `bin/esgw`、systemd/容器交付文件、安装脚本、用户文档、AGPL-3.0 LICENSE、第三方许可证 CSV 和 SPDX JSON SBOM。顶层同时提供许可证、SBOM 与全部 SHA256。

发行前还应完成 `make portability-test`、全量 Go/Web 门禁、Envoy validate matrix 和四拓扑 e2e。脚本先用 `go-licenses` 拒绝 forbidden Go 依赖，再结合 Go module 与 npm lock 生成可审计清单；任一包缺少许可证元数据都会失败。

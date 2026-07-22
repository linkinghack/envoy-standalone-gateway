# T5：发行工程与 M1 收口

## 目标

生成可审计发行物和供应链清单，以需求—实现—证据矩阵关闭 M1。

## 步骤

1. amd64/arm64 归档与 SHA256；
2. 第三方许可证扫描与 SPDX SBOM；
3. 管理面资源基线和 <150MB 目标核验；
4. M1 P0 需求矩阵、全量质量门禁；
5. roadmap/index/trace 收口并独立提交。

## 进展

- 已完成：发行脚本实测生成 linux/amd64（11.6 MB）与 linux/arm64（10.7 MB）归档、SHA256SUMS、397 行 Go/npm 许可证清单和 SPDX JSON SBOM，归档内容与校验和断言通过。
- `go-licenses` forbidden policy、npm lock 许可证完整性与缺失工具硬失败均进入发行门禁；CI 固定 go-licenses v1.6.0、syft v1.49.0。
- 管理面 steady-state RSS 最终实测 29,016 KiB（目标 153,600 KiB）；保留 Argon2id 64 MiB 参数，并在一次性 bootstrap 后归还临时页。
- M1 P0 需求矩阵逐条给出实现和直接证据，NFR-5 延后边界明确；Linux/macOS × amd64/arm64 交叉构建通过。
- 最终门禁通过：Go build/test/race/vet/golangci-lint，Web generate/typecheck/Vitest/build/Playwright，packaging/docs/resource/portability/image smoke，3 Envoy × 6 artifact validate，四拓扑连续流量与 static hot restart e2e，release artifacts/checksums。

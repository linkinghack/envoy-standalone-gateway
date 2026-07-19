# T1 仓库工程基线

- **状态**: 未开始
- **依赖**: 无
- **设计依据**: [工程基线](../../../dev_design/260719-1-engineering-baseline.md)（本 task 同时是该文档的落地与校准）

## 目标

建立可持续的 Go 工程骨架与 CI，使后续所有 task 在统一基线上开发。

## 执行步骤

1. `go mod init github.com/linkinghack/envoy-standalone-gateway`（Go ≥ 1.22）。
2. 建目录骨架（空包可含 `doc.go` 占位）：`cmd/esgw/`、`internal/protocol/`、`internal/compile/`、`internal/ir/`、`internal/deliver/static/`、`internal/version/`、`testdata/`、`e2e/`。
3. `internal/version/`：`Version`（构建注入）、`BinaryName = "esgw"`、Envoy 支持区间常量（SD4，初值按当前 Envoy 最近 3 个 minor 填写并注明来源日期）。
4. `Makefile`：`build`（ldflags 注入版本）、`test`、`lint`、`fmt`、`e2e`（占位）、`golden-update`（占位）。
5. golangci-lint 配置（启用 `govet, staticcheck, errcheck, revive, gofumpt`；行宽不限）。
6. GitHub Actions：`ci.yaml`——push/PR 触发 build + test + lint；envoy validate 矩阵 job 先占位（T7 启用）。
7. `.gitignore`（bin、dist、覆盖率产物等）；`LICENSE` 已存在不动。
8. 依赖许可证扫描：CI 加 `go-licenses check ./...` 步骤（可行性 §7），禁止清单按 AGPL 兼容性配置。
9. 更新根 `README.md`：加入项目状态徽章与开发入口指引（指向 design_docs 与本 sprint）。

## 验收

- [ ] `make build && make test && make lint` 本地全绿
- [ ] CI 在远端跑通（如仓库未接远端，则本地 `act` 或等价验证并记录）
- [ ] 提交为 1~2 个清晰 commit

## 进展记录

（接手会话在此追加：日期 / 完成内容 / 问题 / 下一步）

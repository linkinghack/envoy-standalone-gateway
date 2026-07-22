# Envoy Standalone Gateway — 工程基线见 design_docs/dev_manage/dev_design/260719-1-engineering-baseline.md

MODULE      := github.com/linkinghack/envoy-standalone-gateway
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BIN         := bin/esgw
GOFLAGS     := -trimpath
LDFLAGS     := -s -w -X $(MODULE)/internal/version.Version=$(VERSION)

.DEFAULT_GOAL := build

.PHONY: build
build: ## 构建 esgw 二进制（注入版本）
	@mkdir -p bin
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/esgw

.PHONY: test
test: ## 运行全部单元测试（不含 e2e）
	go test ./...

.PHONY: fmt
fmt: ## gofumpt 格式化
	gofumpt -w .

.PHONY: lint
lint: ## golangci-lint
	golangci-lint run ./...

.PHONY: web-install
web-install: ## 安装锁定的管理控制台依赖
	cd web && npm ci

.PHONY: web-generate
web-generate: ## 由 OpenAPI 生成管理控制台 TypeScript 类型
	cd web && npm run generate

.PHONY: web-test
web-test: ## 运行管理控制台类型检查和单元测试
	cd web && npm run typecheck && npm run test

.PHONY: web-build
web-build: ## 构建并同步 Go embed 管理控制台产物
	cd web && npm run build

.PHONY: web-e2e
web-e2e: ## 运行管理控制台桌面/移动端浏览器冒烟
	cd web && npm run e2e

.PHONY: e2e
e2e: ## S1 真实流量冒烟（docker compose；默认 go test 不触发）
	e2e/run.sh

.PHONY: e2e-xds
e2e-xds: ## ADS 模式 e2e（esgw serve + 真实 Envoy；docker compose）
	e2e/xds/run.sh

.PHONY: e2e-static-managed
e2e-static-managed: ## static 托管 hot restart e2e（真实 Envoy）
	e2e/static-managed/run.sh

.PHONY: e2e-topology-matrix
e2e-topology-matrix: ## T1/T2 × xDS/static 四组合真实 Envoy e2e
	e2e/topology-matrix/run.sh

.PHONY: e2e-l4
e2e-l4: ## TCP/TLS passthrough/UDP 真实流量 e2e
	e2e/l4/run.sh

.PHONY: e2e-extauth
e2e-extauth: ## HTTP/gRPC extAuth 真实流量 e2e
	e2e/extauth/run.sh

.PHONY: e2e-policy
e2e-policy: ## IPAccess 与动态 key 本地限流三版本真实流量 e2e
	e2e/policy/run.sh

.PHONY: golden-update
golden-update: ## 刷新 golden 快照（diff 必须人工评审）
	go test ./internal/golden -update

.PHONY: validate-matrix
validate-matrix: ## 多版本 envoy --mode validate（版本同源 internal/version 常量）
	scripts/validate-matrix.sh

.PHONY: packaging-test
packaging-test: ## systemd 与安装/升级/卸载脚本测试
	packaging/tests/install_test.sh

.PHONY: image-smoke
image-smoke: ## 两类 OCI 镜像的非 root/健康检查 smoke
	packaging/tests/image_smoke.sh

.PHONY: docs-test
docs-test: ## 用户文档链接、示例配置与 compose 检查
	scripts/docs-test.sh

.PHONY: resource-test
resource-test: ## 管理面空载 RSS < 150 MiB 基线
	scripts/measure-resources.sh

.PHONY: portability-test
portability-test: ## Linux/macOS amd64/arm64 交叉构建
	scripts/portability-test.sh

.PHONY: release
release: ## 双架构归档、许可证、SPDX SBOM 与 SHA256
	packaging/release/build.sh

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: help
help: ## 显示全部目标
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

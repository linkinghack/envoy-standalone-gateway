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

.PHONY: e2e
e2e: ## S1 真实流量冒烟（docker compose；默认 go test 不触发）
	e2e/run.sh

.PHONY: golden-update
golden-update: ## 刷新 golden 快照（diff 必须人工评审）
	go test ./internal/golden -update

.PHONY: validate-matrix
validate-matrix: ## 多版本 envoy --mode validate（版本同源 internal/version 常量）
	scripts/validate-matrix.sh

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: help
help: ## 显示全部目标
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

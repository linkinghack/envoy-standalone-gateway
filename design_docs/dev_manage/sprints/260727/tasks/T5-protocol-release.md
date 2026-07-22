# T5：协议 schema/spec/examples 发行

## 目标

让第三方无需引用内部 Go 包即可获得规范、JSON Schema、样例和一致性验证入口。

## 步骤

1. schema CLI/生成器；2. 规范索引与版本策略；3. valid/invalid examples；4. clean-diff/conformance 门禁；5. 归档接入与提交。

## 进展

- 已完成：新增 `esgw schema [-o]`，由 `internal/protocol.Schemas()` 单一真源导出 JSON Schema 2020-12；提交 `protocol/schema/v1alpha1.json` 并以逐字节 clean-diff 测试防漂移。
- 已完成：新增 `esgw conformance -f <dir> [-o]`，执行 strict load + F1–F6/xDS 资源校验，输出确定排序的 JSON 报告；错误码固定为 `ESGW_<STAGE>_INVALID`，并携带 schema 的 file/docIndex 或编译阶段的 file/kind/name/path SourceRef。
- 已完成：新增自包含 `protocol/README.md`、`SPEC.md`，冻结六种对象、HTTP/L4、策略、动态限流、mTLS/熔断、默认值、诊断和 alpha 兼容规则。
- 已完成：三个 valid 与三个 invalid 样例均提交逐字节 `expected.json`；`scripts/protocol-check.sh` 把二进制和 bundle 复制到仓库外临时目录后执行 schema 重生成和全部样例，不依赖内部 Go 包或仓库相对路径。
- 已完成：CI `build-test-lint` 接入 `make protocol-check`；release 双架构归档包含整个 `protocol/`。完整 release 实测同时修复两个既有门禁 bug：`tar | grep -q` 在 pipefail 下的 SIGPIPE 141，以及 SPDX 文件匹配错误。
- 已完成：`internal/protocol/release_test.go` 使用提交的 schema 校验所有 valid 样例和 schema/semantic 两类 invalid 样例，并核验生成结果无差异。

## 完成判定

Schema、规范、样例、诊断 CLI、仓库外 conformance、CI clean-diff 和发行归档已闭环。T5 完成，进入 T6 全矩阵与 Sprint 收口。

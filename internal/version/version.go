// Package version 集中版本与命名常量。
// D1（项目命名）未决期间，二进制名统一从 BinaryName 取，这里是唯一改名点。
package version

// Version 构建期注入：make build 通过 -ldflags "-X .../version.Version=<v>" 设置。
var Version = "dev"

// BinaryName 是产出的单一二进制名称。
const BinaryName = "esgw"

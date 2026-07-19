package version

// Envoy 支持区间（技术设计 SD4，RK4/NFR-6）：最近 3 个 minor 版本。
// 来源：envoyproxy/envoy GitHub releases，2026-07-19 查询
// （v1.39.0 发布于 2026-07-14，当前最新稳定）。
const (
	// EnvoyMinMinor 支持的最小 Envoy minor（1.<n>）。
	EnvoyMinMinor = 37
	// EnvoyMaxMinor 支持的最大 Envoy minor（1.<n>）。
	EnvoyMaxMinor = 39
)

// EnvoyMatrixVersions 是 CI validate 矩阵（T7）与本地 make validate-matrix
// 使用的具体补丁版本，均落在 [EnvoyMinMinor, EnvoyMaxMinor] 区间内。
var EnvoyMatrixVersions = []string{"1.37.5", "1.38.3", "1.39.0"}

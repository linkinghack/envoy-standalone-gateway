#!/usr/bin/env bash
# validate-matrix.sh — 多版本 envoy --mode validate 矩阵（编译层 §8.3，验收 A2）。
#
# 版本列表单一事实来源是 internal/version.EnvoyMatrixVersions（SD4）；本脚本
# 从常量中解析版本，逐版本拉取 envoyproxy/envoy:v<X> 官方镜像，对全部 golden
# static 产物（testdata/<case>/want-static.yaml）跑 `envoy --mode validate`。
#
# 证书相对路径（testdata/certs/...）通过把仓库根挂载为容器工作目录解析，
# 产物内容与机器无关（A6）。
#
# 用法：
#   scripts/validate-matrix.sh            # 全部矩阵版本
#   MATRIX_VERSIONS="1.39.0" scripts/validate-matrix.sh   # 只跑指定版本（调试）
set -euo pipefail

cd "$(dirname "$0")/.."

if [[ -n "${MATRIX_VERSIONS:-}" ]]; then
	versions="$MATRIX_VERSIONS"
else
	# 从 internal/version 常量解析（单一事实来源，CI 与本地同源）。
	versions=$(sed -n 's/.*EnvoyMatrixVersions = \[\]string{\(.*\)}/\1/p' internal/version/envoy.go \
		| tr -d ' "' | tr ',' ' ')
fi
if [[ -z "$versions" ]]; then
	echo "error: no Envoy versions resolved from internal/version/envoy.go" >&2
	exit 1
fi

cases=()
for f in testdata/*/want-static.yaml; do
	cases+=("$f")
done
if [[ ${#cases[@]} -eq 0 ]]; then
	echo "error: no golden static artifacts found under testdata/" >&2
	exit 1
fi

fail=0
for v in $versions; do
	image="envoyproxy/envoy:v${v}"
	echo "=== envoy ${v}: docker pull ${image}"
	if ! docker pull "$image"; then
		echo "FAIL: pull ${image}" >&2
		fail=1
		continue
	fi
	for c in "${cases[@]}"; do
		printf '  validate %-50s ' "$c"
		# --network none：validate 不需要网络；仓库根只读挂载为工作目录，
		# 产物内 testdata/certs/... 相对路径据此解析。
		if docker run --rm --network none -v "$PWD:/workspace:ro" -w /workspace \
			"$image" --mode validate -c "$c" >/tmp/esgw-validate-matrix.log 2>&1; then
			echo "OK"
		else
			echo "FAIL"
			tail -20 /tmp/esgw-validate-matrix.log >&2
			fail=1
		fi
	done
done

if [[ $fail -ne 0 ]]; then
	echo "validate-matrix: FAILED" >&2
	exit 1
fi
echo "validate-matrix: all OK (${versions} x ${#cases[@]} artifacts)"

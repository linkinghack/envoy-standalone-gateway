#!/usr/bin/env bash
# validate-matrix.sh — 多版本 envoy --mode validate 矩阵（编译层 §8.3，验收 A2）。
#
# 版本列表单一事实来源是 internal/version.EnvoyMatrixVersions（SD4）；本脚本
# 从常量中解析版本，逐版本拉取 envoyproxy/envoy:v<X> 官方镜像，对全部 golden
# static 产物（testdata/<case>/want-static.yaml）跑 `envoy --mode validate`。
#
# 证书相对路径（testdata/certs/...）通过 tar stdin 复制到一次性容器解析；
# 不依赖宿主 bind mount，产物内容与机器无关（A6）。
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
log=$(mktemp)
container=""
cleanup() {
	[[ -n "$container" ]] && docker rm -f "$container" >/dev/null 2>&1 || true
	rm -f "$log"
}
trap cleanup EXIT
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
		# --network none：validate 不需要网络。docker cp 接受 tar stdin，既
		# 避开 Docker Desktop/WSL bind mount，又保留相对证书路径布局。
		container=$(docker create --network none -w /workspace "$image" --mode validate -c "$c")
		if tar -cf - testdata | docker cp - "$container:/workspace" \
			&& docker start -a "$container" >"$log" 2>&1; then
			echo "OK"
		else
			echo "FAIL"
			tail -20 "$log" >&2
			fail=1
		fi
		docker rm "$container" >/dev/null 2>&1 || true
		container=""
	done
done

if [[ $fail -ne 0 ]]; then
	echo "validate-matrix: FAILED" >&2
	exit 1
fi
echo "validate-matrix: all OK (${versions} x ${#cases[@]} artifacts)"

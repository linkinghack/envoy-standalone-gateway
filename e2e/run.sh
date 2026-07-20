#!/usr/bin/env bash
# e2e/run.sh — S1 场景真实流量冒烟（编译层 §8.4，验收 A3；SD8：仅 S1 做流量级断言）。
#
# 拓扑（docker compose）：envoy（官方镜像）+ 两个 hashicorp/http-echo 后端。
# Envoy 配置由 golden 产物 testdata/s1/want-static.yaml 派生（保证 e2e 与
# golden/validate-matrix 消费同一份产物）：
#   1. sed 把证书相对路径 testdata/certs/ 替换为容器内挂载路径 /etc/esgw/certs/
#      （决策记录：golden 产物必须保持仓库根相对路径以满足 A6，容器路径差异
#      在 e2e 侧用 sed 弥合，比"编译时注入容器路径"简单且单一事实来源）；
#   2. awk 把两个 127.0.0.1 静态端点改指 compose 服务名（www:5678 / blog:5678，
#      http-echo 默认监听 :5678）。
#
# 断言：
#   A. https://www.example.com  → www 后端内容（TLS 终止 + SNI/域名分流）
#   B. https://blog.example.com → blog 后端内容
#   C. http://www.example.com   → 301 跳转到 https
#   D. SNI 不在证书域名集合内    → 握手失败（无匹配 filter chain）
#
# 端口可用环境变量覆盖：E2E_HTTP_PORT（默认 18080）、E2E_HTTPS_PORT（默认 18443）。
# Envoy 镜像默认取 internal/version.EnvoyMatrixVersions 的最后一个版本，
# 可用 ENVOY_IMAGE 覆盖。
set -euo pipefail

cd "$(dirname "$0")"
ROOT="$(cd .. && pwd)"
HTTP_PORT="${E2E_HTTP_PORT:-18080}"
HTTPS_PORT="${E2E_HTTPS_PORT:-18443}"

if [[ -z "${ENVOY_IMAGE:-}" ]]; then
	latest=$(sed -n 's/.*EnvoyMatrixVersions = \[\]string{\(.*\)}/\1/p' "$ROOT/internal/version/envoy.go" \
		| tr -d ' "' | tr ',' '\n' | tail -1)
	ENVOY_IMAGE="envoyproxy/envoy:v${latest}"
fi
export ENVOY_IMAGE E2E_HTTP_PORT="$HTTP_PORT" E2E_HTTPS_PORT="$HTTPS_PORT"

COMPOSE=(docker compose -f docker-compose.yaml)

cleanup() {
	"${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "=== generate envoy config from testdata/s1/want-static.yaml"
mkdir -p generated
sed 's|testdata/certs/|/etc/esgw/certs/|g' "$ROOT/testdata/s1/want-static.yaml" |
	awk '
		/address: 127\.0\.0\.1/ {
			buf = $0
			if ((getline nxt) > 0) {
				# STATIC cluster 只接受 IP：按 S1 输入端口改指 compose 固定 IP
				# （www=172.30.0.11:3000，blog=172.30.0.12:4000，见 docker-compose.yaml）。
				if (nxt ~ /port_value: 3000/)      { sub("127.0.0.1", "172.30.0.11", buf) }
				else if (nxt ~ /port_value: 4000/) { sub("127.0.0.1", "172.30.0.12", buf) }
				print buf; print nxt
			} else print buf
			next
		}
		{ print }
	' >generated/envoy.yaml
grep -q "address: 172.30.0.11" generated/envoy.yaml || { echo "error: endpoint rewrite failed" >&2; exit 1; }
grep -q "/etc/esgw/certs/www.crt" generated/envoy.yaml || { echo "error: cert path rewrite failed" >&2; exit 1; }

echo "=== docker compose up (${ENVOY_IMAGE})"
# --force-recreate：生成的 envoy.yaml 内容变化不会触发 compose 的重建判断，
# 强制重建保证 envoy 加载的是本次生成的配置。
"${COMPOSE[@]}" up -d --force-recreate

CA="--cacert $ROOT/testdata/certs/ca.crt"
fails=0

# retry_get <url> <resolve-host:port> —— 等待后端就绪后返回 body。
retry_get() {
	local url="$1" hostport="$2" out="" i
	for i in $(seq 1 30); do
		if out=$(curl -fsS --noproxy '*' $CA --resolve "${hostport}:${HTTPS_PORT}:127.0.0.1" "$url" 2>/dev/null); then
			printf '%s' "$out"
			return 0
		fi
		sleep 1
	done
	return 1
}

echo "=== assert A: https www.example.com -> www backend"
body=$(retry_get "https://www.example.com:${HTTPS_PORT}/" "www.example.com") || { echo "FAIL A: request error"; fails=1; }
if [[ $fails -eq 0 && "$body" == *www-backend* ]]; then echo "OK A ($body)"; else echo "FAIL A: body=$body"; fails=1; fi

echo "=== assert B: https blog.example.com -> blog backend"
body=$(retry_get "https://blog.example.com:${HTTPS_PORT}/" "blog.example.com") || { echo "FAIL B: request error"; fails=1; }
if [[ $fails -eq 0 && "$body" == *blog-backend* ]]; then echo "OK B ($body)"; else echo "FAIL B: body=$body"; fails=1; fi

echo "=== assert C: http -> 301 https"
out=$(curl -s --noproxy '*' -o /dev/null -w '%{http_code} %{redirect_url}' \
	--resolve "www.example.com:${HTTP_PORT}:127.0.0.1" "http://www.example.com:${HTTP_PORT}/") || true
if [[ "$out" == 301\ https://www.example.com* ]]; then echo "OK C ($out)"; else echo "FAIL C: $out"; fails=1; fi

echo "=== assert D: SNI unknown.example.com -> handshake failure"
if curl -fsS --noproxy '*' $CA --resolve "unknown.example.com:${HTTPS_PORT}:127.0.0.1" \
	"https://unknown.example.com:${HTTPS_PORT}/" >/dev/null 2>&1; then
	echo "FAIL D: unexpected success"
	fails=1
else
	echo "OK D (connection rejected as expected)"
fi

if [[ $fails -ne 0 ]]; then
	echo "=== envoy logs (tail)"
	"${COMPOSE[@]}" logs envoy | tail -30
	echo "e2e: FAILED" >&2
	exit 1
fi
echo "e2e: all assertions passed"

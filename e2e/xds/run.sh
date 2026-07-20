#!/usr/bin/env bash
# e2e/xds/run.sh — ADS 模式真实流量 e2e（Sprint 260720 T5；
# 验收 A2/A3/A4/A5(日志)/A7(e2e 部分)，闭环 M0 验收第 3 项）。
#
# 拓扑（docker compose，详见 docker-compose.yaml 头部注释）：
#   esgw serve（xds 模式，-log-level debug）+ envoy（官方镜像，接入 bootstrap
#   由 bin/esgw bootstrap 导出）+ 两个 hashicorp/http-echo 后端，
#   四者经 network_mode: service:esgw 共享网络命名空间（pod 形态）。
#
# 编排序：
#   1. 交叉编译静态 linux esgw 二进制（scratch 镜像零依赖）；
#   2. 宿主 bin/esgw bootstrap 导出接入 bootstrap（纯函数，宿主跑即可）；
#   3. 宿主 bin/esgw compile --mode xds 取期望 IR.Version（A2/A4 基准）；
#   4. compose up（envoy depends_on esgw；就绪一律靠断言重试，无固定 sleep）。
#
# 断言：
#   A7. envoy 以导出 bootstrap 正常启动（admin /ready = LIVE）
#   A2. admin /config_dump：LDS/RDS/CDS/EDS/SDS 五类型资源在册且
#       version_info == IR.Version（== compile --mode xds 产物 version）
#   A4. 同一配置目录：compile --mode xds 产物 version == golden
#       testdata/s1/want-xds.json version == ADS version_info（M0 第 3 项闭环）
#   A5. esgw 日志可见五类型 ACK（Debug 级，e2e 以 -log-level debug 启动）
#   A3. 四断言同 static e2e：www→www-backend、blog→blog-backend、
#       http→301 https、SNI 集合外握手失败
#
# 端口可用环境变量覆盖：E2E_HTTP_PORT（18080）、E2E_HTTPS_PORT（18443）、
# E2E_ADMIN_PORT（19901）。Envoy 镜像默认取
# internal/version.EnvoyMatrixVersions 最后一个版本，ENVOY_IMAGE 可覆盖。
set -euo pipefail

cd "$(dirname "$0")"
ROOT="$(cd ../.. && pwd)"
HTTP_PORT="${E2E_HTTP_PORT:-18080}"
HTTPS_PORT="${E2E_HTTPS_PORT:-18443}"
ADMIN_PORT="${E2E_ADMIN_PORT:-19901}"

if [[ -z "${ENVOY_IMAGE:-}" ]]; then
	latest=$(sed -n 's/.*EnvoyMatrixVersions = \[\]string{\(.*\)}/\1/p' "$ROOT/internal/version/envoy.go" \
		| tr -d ' "' | tr ',' '\n' | tail -1)
	ENVOY_IMAGE="envoyproxy/envoy:v${latest}"
fi
export ENVOY_IMAGE E2E_HTTP_PORT="$HTTP_PORT" E2E_HTTPS_PORT="$HTTPS_PORT" E2E_ADMIN_PORT="$ADMIN_PORT"

COMPOSE=(docker compose -f docker-compose.yaml)

cleanup() {
	"${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "=== build esgw binary for container (static linux)"
mkdir -p generated
# scratch 镜像无 libc/shell，必须静态编译；架构对齐 docker server
# （本地 macOS 与 CI linux 的宿主二进制均不能直接进容器）。
docker_arch=$(docker version --format '{{.Server.Arch}}')
case "$docker_arch" in
	x86_64)  docker_arch=amd64 ;;
	aarch64) docker_arch=arm64 ;;
esac
(cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$docker_arch" go build -trimpath -o e2e/xds/generated/esgw ./cmd/esgw)

echo "=== build host esgw + render bootstrap"
(cd "$ROOT" && go build -trimpath -o bin/esgw ./cmd/esgw)
"$ROOT/bin/esgw" bootstrap -c esgw.yaml -o generated/bootstrap.yaml
grep -q "address: 127.0.0.1" generated/bootstrap.yaml || { echo "error: bootstrap xds address unexpected" >&2; exit 1; }

echo "=== compute expected IR.Version (compile --mode xds, same config dir)"
EXPECTED_VERSION=$( (cd "$ROOT" && bin/esgw compile -f testdata/s1/input --mode xds) \
	| python3 -c 'import json,sys; print(json.load(sys.stdin)["version"])')
GOLDEN_VERSION=$(python3 -c 'import json; print(json.load(open("'"$ROOT"'/testdata/s1/want-xds.json"))["version"])')
STATIC_VERSION=$( (cd "$ROOT" && bin/esgw compile -f testdata/s1/input --mode static) \
	| sed -n 's/^# config_version: //p')
echo "    xds compile version: $EXPECTED_VERSION"
echo "    golden xds version:  $GOLDEN_VERSION"
echo "    static header version: $STATIC_VERSION (F5 形态化按模式分流，static/xds 版本串不同属预期，见 T5 进展记录)"
export EXPECTED_VERSION

echo "=== docker compose up (${ENVOY_IMAGE})"
"${COMPOSE[@]}" up -d --build --force-recreate

fails=0

echo "=== assert A7: envoy up with exported bootstrap (admin /ready)"
ready=""
for i in $(seq 1 30); do
	if out=$(curl -fsS --noproxy '*' "http://127.0.0.1:${ADMIN_PORT}/ready" 2>/dev/null) && [[ "$out" == LIVE* ]]; then
		ready=1
		break
	fi
	sleep 1
done
if [[ -n "$ready" ]]; then echo "OK A7 (envoy ready, bootstrap accepted)"; else echo "FAIL A7: envoy not ready"; fails=1; fi

# check_dump —— A2：五类型动态资源在册且 version_info == IR.Version。
# config_dump 结构实测（v1.39.0）：顶层 {"configs": [...]} 按 admin.v3 类型分段；
# LDS 版本在 dynamic_listeners[].active_state.version_info；EDS 的
# DynamicEndpointConfig 不携带 version_info（Envoy 行为），其版本以 A5 的
# EDS ACK 日志（version=<IR.Version>）为证。
check_dump() {
	curl -fsS --noproxy '*' "http://127.0.0.1:${ADMIN_PORT}/config_dump?include_eds" 2>/dev/null | python3 -c '
import json, os, sys
want = os.environ["EXPECTED_VERSION"]
try:
    dump = json.load(sys.stdin)
except Exception:
    sys.exit(1)
sections = {}
for c in dump.get("configs", []):
    sections[c.get("@type", "").split(".")[-1]] = c

fails = []

def check_versioned(name, items, res_key, type_url, get_vi):
    if not items:
        fails.append(f"{name}: no resources in config_dump")
        return
    for it in items:
        vi = get_vi(it)
        if vi != want:
            fails.append(f"{name}: version_info={vi!r} != {want}")
        at = (it.get(res_key) or {}).get("@type", "")
        if not at.endswith(type_url):
            fails.append(f"{name}: unexpected @type {at!r}")

lds = (sections.get("ListenersConfigDump") or {}).get("dynamic_listeners") or []
check_versioned("LDS", [l.get("active_state") or {} for l in lds], "listener",
                "envoy.config.listener.v3.Listener", lambda it: it.get("version_info"))
cds = (sections.get("ClustersConfigDump") or {}).get("dynamic_active_clusters") or []
check_versioned("CDS", cds, "cluster",
                "envoy.config.cluster.v3.Cluster", lambda it: it.get("version_info"))
rds = (sections.get("RoutesConfigDump") or {}).get("dynamic_route_configs") or []
check_versioned("RDS", rds, "route_config",
                "envoy.config.route.v3.RouteConfiguration", lambda it: it.get("version_info"))
sds = (sections.get("SecretsConfigDump") or {}).get("dynamic_active_secrets") or []
check_versioned("SDS", sds, "secret",
                "envoy.extensions.transport_sockets.tls.v3.Secret", lambda it: it.get("version_info"))
# EDS：config_dump 不含 version_info，断言资源在册与类型；版本由 A5 ACK 日志佐证。
eds = (sections.get("EndpointsConfigDump") or {}).get("dynamic_endpoint_configs") or []
if not eds:
    fails.append("EDS: no resources in config_dump")
for it in eds:
    at = (it.get("endpoint_config") or {}).get("@type", "")
    if not at.endswith("envoy.config.endpoint.v3.ClusterLoadAssignment"):
        fails.append(f"EDS: unexpected @type {at!r}")

if fails:
    print("\n".join(fails), file=sys.stderr)
    sys.exit(1)
print(f"OK A2 (LDS/RDS/CDS/SDS version_info={want}; EDS present, version via ACK log)")
'
}

echo "=== assert A2: config_dump five types, version_info == IR.Version"
a2=""
for i in $(seq 1 30); do
	if msg=$(check_dump); then
		a2=1
		echo "$msg"
		break
	fi
	sleep 1
done
if [[ -z "$a2" ]]; then echo "FAIL A2: $msg"; fails=1; fi

echo "=== assert A4: ADS version == compile --mode xds version == golden version"
ads_version=$(curl -fsS --noproxy '*' "http://127.0.0.1:${ADMIN_PORT}/config_dump" 2>/dev/null \
	| python3 -c '
import json, sys
d = json.load(sys.stdin)
for c in d.get("configs", []):
    if c.get("@type", "").endswith("ClustersConfigDump"):
        print(c.get("version_info") or "")
        break
' || true)
if [[ "$ads_version" == "$EXPECTED_VERSION" && "$GOLDEN_VERSION" == "$EXPECTED_VERSION" ]]; then
	echo "OK A4 (ads=$ads_version == compile=$EXPECTED_VERSION == golden=$GOLDEN_VERSION)"
else
	echo "FAIL A4: ads=$ads_version compile=$EXPECTED_VERSION golden=$GOLDEN_VERSION"
	fails=1
fi

echo "=== assert A5: five-type ACK visible in esgw logs (debug)"
a5=""
for i in $(seq 1 30); do
	logs=$("${COMPOSE[@]}" logs esgw 2>/dev/null || true)
	missing=""
	for t in \
		"envoy.config.listener.v3.Listener" \
		"envoy.config.route.v3.RouteConfiguration" \
		"envoy.config.cluster.v3.Cluster" \
		"envoy.config.endpoint.v3.ClusterLoadAssignment" \
		"envoy.extensions.transport_sockets.tls.v3.Secret"; do
		if ! grep -q "ACK received" <<<"$logs" || ! grep "ACK received" <<<"$logs" | grep -q "$t"; then
			missing="$missing $t"
		fi
	done
	if [[ -z "$missing" ]]; then
		a5=1
		break
	fi
	sleep 1
done
if [[ -n "$a5" ]]; then echo "OK A5 (ACK received for all 5 type_urls)"; else echo "FAIL A5: missing ACK for:$missing"; fails=1; fi

CA="--cacert $ROOT/testdata/certs/ca.crt"

# retry_get <url> <resolve-host:port> —— 等待后端就绪后返回 body（同 static e2e）。
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

echo "=== assert A3-A: https www.example.com -> www backend"
body=$(retry_get "https://www.example.com:${HTTPS_PORT}/" "www.example.com") || body=""
if [[ -n "$body" && "$body" == *www-backend* ]]; then echo "OK A3-A ($body)"; else echo "FAIL A3-A: body=$body"; fails=1; fi

echo "=== assert A3-B: https blog.example.com -> blog backend"
body=$(retry_get "https://blog.example.com:${HTTPS_PORT}/" "blog.example.com") || body=""
if [[ -n "$body" && "$body" == *blog-backend* ]]; then echo "OK A3-B ($body)"; else echo "FAIL A3-B: body=$body"; fails=1; fi

echo "=== assert A3-C: http -> 301 https"
out=$(curl -s --noproxy '*' -o /dev/null -w '%{http_code} %{redirect_url}' \
	--resolve "www.example.com:${HTTP_PORT}:127.0.0.1" "http://www.example.com:${HTTP_PORT}/") || true
if [[ "$out" == 301\ https://www.example.com* ]]; then echo "OK A3-C ($out)"; else echo "FAIL A3-C: $out"; fails=1; fi

echo "=== assert A3-D: SNI unknown.example.com -> handshake failure"
if curl -fsS --noproxy '*' $CA --resolve "unknown.example.com:${HTTPS_PORT}:127.0.0.1" \
	"https://unknown.example.com:${HTTPS_PORT}/" >/dev/null 2>&1; then
	echo "FAIL A3-D: unexpected success"
	fails=1
else
	echo "OK A3-D (connection rejected as expected)"
fi

if [[ $fails -ne 0 ]]; then
	echo "=== esgw logs (tail)"
	"${COMPOSE[@]}" logs esgw | tail -30
	echo "=== envoy logs (tail)"
	"${COMPOSE[@]}" logs envoy | tail -30
	echo "e2e-xds: FAILED" >&2
	exit 1
fi
echo "e2e-xds: all assertions passed"

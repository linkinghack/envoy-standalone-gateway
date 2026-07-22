#!/usr/bin/env bash
# Combined P1 static validation and ADS real-traffic matrix. All runtime files
# are copied into containers so the scenario does not depend on host bind mounts.
set -euo pipefail

cd "$(dirname "$0")"
root=$(cd ../.. && pwd)
docker_bin=${DOCKER:-docker}
network="esgw-p1-xds-$PPID-$$"
subnet=10.246.37.0/24
manager_ip=10.246.37.2
manager="${network}-manager"
backend="${network}-backend"
auth="${network}-auth"
tcp_backend="${network}-tcp"
udp_backend="${network}-udp"
tls_api="${network}-tls-api"
tls_web="${network}-tls-web"
proxy=""
tmp=$(mktemp -d)
containers=()
images=()

tcp_port=${E2E_P1_TCP_PORT:-14306}
tls_port=${E2E_P1_TLS_PORT:-19443}
udp_port=${E2E_P1_UDP_PORT:-16353}
http_auth_port=${E2E_P1_HTTP_AUTH_PORT:-18181}
grpc_auth_port=${E2E_P1_GRPC_AUTH_PORT:-18182}
mtls_port=${E2E_P1_MTLS_PORT:-19444}
admin_port=${E2E_P1_ADMIN_PORT:-19911}

cleanup() {
	local status=$?
	if ((${#containers[@]})); then
		"$docker_bin" rm -f "${containers[@]}" >/dev/null 2>&1 || true
	fi
	"$docker_bin" network rm "$network" >/dev/null 2>&1 || true
	if ((${#images[@]})); then
		"$docker_bin" image rm -f "${images[@]}" >/dev/null 2>&1 || true
	fi
	rm -rf "$tmp"
	return "$status"
}
trap cleanup EXIT

fail() {
	echo "FAIL: $*" >&2
	if [[ -n "$proxy" ]]; then
		"$docker_bin" logs "$proxy" 2>&1 | tail -80 >&2 || true
	fi
	"$docker_bin" logs "$manager" 2>&1 | tail -80 >&2 || true
	exit 1
}

build_image() {
	local tag=$1 context=$2
	tar -C "$context" -cf - . | "$docker_bin" build -q -t "$tag" - >/dev/null
	images+=("$tag")
}

echo "=== build host/container binaries and isolated images"
mkdir -p "$tmp/manager" "$tmp/auth" "$tmp/backend" "$tmp/client"
docker_arch=$("$docker_bin" version --format '{{.Server.Arch}}')
case "$docker_arch" in
	x86_64) docker_arch=amd64 ;;
	aarch64) docker_arch=arm64 ;;
	amd64 | arm64) ;;
	*) fail "unsupported Docker server architecture $docker_arch" ;;
esac
CGO_ENABLED=0 go build -trimpath -o "$tmp/esgw-host" "$root/cmd/esgw"
CGO_ENABLED=0 GOOS=linux GOARCH="$docker_arch" go build -trimpath -o "$tmp/manager/esgw" "$root/cmd/esgw"
CGO_ENABLED=0 GOOS=linux GOARCH="$docker_arch" go build -trimpath -o "$tmp/auth/auth-server" ./../extauth/server
CGO_ENABLED=0 GOOS=linux GOARCH="$docker_arch" go build -trimpath -o "$tmp/backend/server" ./../mtls-circuit/server
CGO_ENABLED=0 GOOS=linux GOARCH="$docker_arch" go build -trimpath -o "$tmp/client/client" ./../policy/client
cp Dockerfile "$tmp/manager/Dockerfile"
cp ../extauth/Dockerfile "$tmp/auth/Dockerfile"
cp ../mtls-circuit/Dockerfile "$tmp/backend/Dockerfile"
cp ../policy/Dockerfile "$tmp/client/Dockerfile"
manager_image="esgw-p1-manager:$PPID-$$"
auth_image="esgw-p1-auth:$PPID-$$"
backend_image="esgw-p1-backend:$PPID-$$"
client_image="esgw-p1-client:$PPID-$$"
build_image "$manager_image" "$tmp/manager"
build_image "$auth_image" "$tmp/auth"
build_image "$backend_image" "$tmp/backend"
build_image "$client_image" "$tmp/client"

(cd "$root" && "$tmp/esgw-host" bootstrap -c e2e/p1-xds/esgw.yaml -o "$tmp/bootstrap.yaml")
(cd "$root" && "$tmp/esgw-host" compile -f e2e/p1-xds/config --mode xds -o "$tmp/xds.json")
(cd "$root" && "$tmp/esgw-host" compile -f e2e/p1-xds/config --mode static -o "$tmp/static.yaml")
expected_version=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$tmp/xds.json")
export EXPECTED_VERSION="$expected_version"
echo "    expected ADS version: $expected_version"

echo "=== start shared network namespace and loopback services"
"$docker_bin" network create --subnet "$subnet" "$network" >/dev/null
"$docker_bin" create --name "$manager" --network "$network" --ip "$manager_ip" -w / \
	-p "127.0.0.1:${tcp_port}:13306" \
	-p "127.0.0.1:${tls_port}:18443" \
	-p "127.0.0.1:${udp_port}:15353/udp" \
	-p "127.0.0.1:${http_auth_port}:18081" \
	-p "127.0.0.1:${grpc_auth_port}:18082" \
	-p "127.0.0.1:${mtls_port}:18444" \
	-p "127.0.0.1:${admin_port}:9901" \
	"$manager_image" serve -c /etc/esgw/esgw.yaml -f /var/lib/esgw/config.d -log-level debug >/dev/null
containers+=("$manager")
mkdir -p "$tmp/runtime/etc/esgw" "$tmp/runtime/var/lib/esgw/config.d" "$tmp/runtime/testdata/certs"
cp esgw.yaml "$tmp/runtime/etc/esgw/esgw.yaml"
cp config/config.yaml "$tmp/runtime/var/lib/esgw/config.d/config.yaml"
cp "$root"/testdata/certs/* "$tmp/runtime/testdata/certs/"
tar -C "$tmp/runtime" -cf - . | "$docker_bin" cp - "$manager:/"
"$docker_bin" start "$manager" >/dev/null

"$docker_bin" run -d --name "$backend" --network "container:$manager" "$backend_image" >/dev/null
containers+=("$backend")
"$docker_bin" run -d --name "$auth" --network "container:$manager" "$auth_image" >/dev/null
containers+=("$auth")
"$docker_bin" run -d --name "$tcp_backend" --network "container:$manager" alpine:3.22 sh -c \
	'while true; do printf "HTTP/1.1 200 OK\r\nContent-Length: 11\r\nConnection: close\r\n\r\ntcp-backend" | nc -l -p 23306; done' >/dev/null
containers+=("$tcp_backend")
"$docker_bin" run -d --name "$udp_backend" --network "container:$manager" alpine:3.22 \
	nc -u -lk -p 25353 -e /bin/cat >/dev/null
containers+=("$udp_backend")

start_tls_backend() {
	local name=$1 port=$2 cert=$3
	"$docker_bin" create --name "$name" --network "container:$manager" --entrypoint sh \
		envoyproxy/envoy:v1.39.0 -c "openssl s_server -quiet -accept $port -cert /tmp/tls.crt -key /tmp/tls.key -www" >/dev/null
	containers+=("$name")
	"$docker_bin" cp "$root/testdata/certs/$cert.crt" "$name:/tmp/tls.crt"
	"$docker_bin" cp "$root/testdata/certs/$cert.key" "$name:/tmp/tls.key"
	"$docker_bin" start "$name" >/dev/null
}
start_tls_backend "$tls_api" 24443 api
start_tls_backend "$tls_web" 25443 www

request_status() {
	local ip=$1 url=$2 header=${3:-} output
	local args=(--rm --network "$network" --ip "$ip" "$client_image" -url "$url")
	[[ -n "$header" ]] && args+=(-header "$header")
	output=$("$docker_bin" run "${args[@]}" 2>/dev/null || true)
	printf '%s' "$output" | sed -n '1p'
}

expect_status() {
	local label=$1 want=$2 ip=$3 url=$4 header=${5:-} got
	got=$(request_status "$ip" "$url" "$header")
	[[ "$got" == "$want" ]] || fail "$label status=$got want=$want"
	echo "  OK $label -> $got"
}

host_status() {
	local port=$1 authorization=$2 path=${3:-/}
	curl --noproxy '*' --max-time 3 -sS -o "$tmp/body" -w '%{http_code}' \
		-H "Authorization: $authorization" "http://127.0.0.1:${port}${path}" 2>/dev/null || true
}

wait_host_status() {
	local port=$1 authorization=$2 want=$3 path=${4:-/} got=""
	for _ in $(seq 1 100); do
		got=$(host_status "$port" "$authorization" "$path")
		[[ "$got" == "$want" ]] && return 0
		sleep 0.1
	done
	fail "port=$port path=$path status=$got want=$want"
}

check_dump() {
	curl -fsS --noproxy '*' "http://127.0.0.1:${admin_port}/config_dump?include_eds" 2>/dev/null | python3 -c '
import json, os, sys
want = os.environ["EXPECTED_VERSION"]
d = json.load(sys.stdin)
sections = {c.get("@type", "").split(".")[-1]: c for c in d.get("configs", [])}
checks = [
    ("LDS", "ListenersConfigDump", "dynamic_listeners", lambda x: (x.get("active_state") or {}).get("version_info")),
    ("CDS", "ClustersConfigDump", "dynamic_active_clusters", lambda x: x.get("version_info")),
    ("RDS", "RoutesConfigDump", "dynamic_route_configs", lambda x: x.get("version_info")),
    ("SDS", "SecretsConfigDump", "dynamic_active_secrets", lambda x: x.get("version_info")),
]
for name, section, key, version in checks:
    items = (sections.get(section) or {}).get(key) or []
    if not items or any(version(item) != want for item in items):
        raise SystemExit(f"{name}: missing resources or version mismatch")
eds = (sections.get("EndpointsConfigDump") or {}).get("dynamic_endpoint_configs") or []
if not eds:
    raise SystemExit("EDS: missing resources")
serialized = json.dumps(d)
if "ca/mtls" not in serialized or "crt/mtls/0" not in serialized:
    raise SystemExit("SDS: mTLS certificate or client CA secret missing")
'
}

wait_ads() {
	local ready=""
	for _ in $(seq 1 150); do
		if ready=$(curl -fsS --noproxy '*' "http://127.0.0.1:${admin_port}/ready" 2>/dev/null) &&
			[[ "$ready" == LIVE* ]] && check_dump; then
			return 0
		fi
		sleep 0.1
	done
	fail "Envoy did not become ready with complete ADS resources"
}

tls_subject() {
	local server_name=$1
	timeout 3 openssl s_client -connect "127.0.0.1:${tls_port}" -servername "$server_name" </dev/null 2>/dev/null \
		| openssl x509 -noout -subject 2>/dev/null || true
}

curl_mtls() {
	curl --noproxy '*' --max-time 10 -sS \
		--cacert "$root/testdata/certs/ca.crt" \
		--cert "$root/testdata/certs/client.crt" \
		--key "$root/testdata/certs/client.key" \
		--resolve "www.example.com:${mtls_port}:127.0.0.1" "$@"
}

validate_static() {
	local version=$1 name="${network}-validate-${version//./-}"
	"$docker_bin" create --name "$name" -w /workspace "envoyproxy/envoy:v$version" \
		--mode validate -c /workspace/envoy.yaml >/dev/null
	containers+=("$name")
	"$docker_bin" cp "$tmp/static.yaml" "$name:/workspace/envoy.yaml"
	tar -C "$root" -cf - testdata/certs | "$docker_bin" cp - "$name:/workspace"
	if ! "$docker_bin" start -a "$name" >"$tmp/validate-$version.log" 2>&1; then
		cat "$tmp/validate-$version.log" >&2
		fail "static validation failed on Envoy $version"
	fi
	"$docker_bin" rm "$name" >/dev/null
}

versions=$(sed -n 's/.*EnvoyMatrixVersions = \[\]string{\(.*\)}/\1/p' "$root/internal/version/envoy.go" |
	tr -d ' "' | tr ',' ' ')
for version in $versions; do
	echo "=== combined static/xDS P1 traffic on Envoy $version"
	validate_static "$version"
	# BusyBox nc retains its UDP peer/session after the first proxy exits. Restart
	# the echo process so each Envoy version begins with an isolated backend.
	"$docker_bin" restart "$udp_backend" >/dev/null
	proxy="${network}-envoy-${version//./-}"
	"$docker_bin" create --name "$proxy" --network "container:$manager" -w / \
		"envoyproxy/envoy:v$version" --mode serve -c /etc/envoy/bootstrap.yaml >/dev/null
	containers+=("$proxy")
	"$docker_bin" cp "$tmp/bootstrap.yaml" "$proxy:/etc/envoy/bootstrap.yaml"
	tar -C "$root" -cf - testdata/certs | "$docker_bin" cp - "$proxy:/"
	"$docker_bin" start "$proxy" >/dev/null
	wait_ads

	echo "--- L4 TCP, TLS passthrough/SNI, and UDP"
	tcp_body=$(curl --noproxy '*' --max-time 3 -fsS "http://127.0.0.1:${tcp_port}/")
	[[ "$tcp_body" == tcp-backend ]] || fail "TCP body=$tcp_body"
	[[ "$(tls_subject api.example.com)" == *"CN = api.example.com"* ]] || fail "api SNI route"
	[[ "$(tls_subject www.example.com)" == *"CN = www.example.com"* ]] || fail "www SNI route"
	[[ -z "$(tls_subject unknown.example.org)" ]] || fail "unknown SNI unexpectedly matched"
	udp_reply=""
	for _ in $(seq 1 20); do
		udp_reply=$(printf 'udp-backend\n' | nc -u -w 1 127.0.0.1 "$udp_port" 2>/dev/null || true)
		[[ "$udp_reply" == udp-backend ]] && break
		sleep 0.1
	done
	[[ "$udp_reply" == udp-backend ]] || fail "UDP reply=$udp_reply"

	echo "--- HTTP/gRPC external authorization"
	wait_host_status "$http_auth_port" allow 200
	grep -q backend-ok "$tmp/body" || fail "HTTP extAuth allow body"
	wait_host_status "$http_auth_port" deny 403
	wait_host_status "$http_auth_port" deny 200 /public
	wait_host_status "$grpc_auth_port" allow 200
	wait_host_status "$grpc_auth_port" deny 403
	"$docker_bin" stop "$auth" >/dev/null
	wait_host_status "$http_auth_port" allow 200
	wait_host_status "$grpc_auth_port" allow 403
	"$docker_bin" start "$auth" >/dev/null

	echo "--- source IP policy and independent quota keys"
	expect_status "allowed source" 200 10.246.37.8 "http://${manager_ip}:18083/"
	expect_status "deny wins" 403 10.246.37.9 "http://${manager_ip}:18083/"
	expect_status "allowed peer ignores spoofed XFF" 200 10.246.37.8 "http://${manager_ip}:18083/" "X-Forwarded-For:10.246.37.9"
	expect_status "denied peer cannot spoof allow" 403 10.246.37.9 "http://${manager_ip}:18083/" "X-Forwarded-For:10.246.37.8"
	for ip in 10.246.37.8 10.246.37.10; do
		for want in 200 200 429; do
			expect_status "client $ip quota" "$want" "$ip" "http://${manager_ip}:18084/"
		done
	done
	for tenant in alpha beta; do
		for want in 200 200 429; do
			expect_status "tenant $tenant quota" "$want" 10.246.37.8 "http://${manager_ip}:18085/" "x-tenant:$tenant"
		done
	done

	echo "--- downstream mTLS and circuit-breaker overflow"
	if curl --noproxy '*' --max-time 3 -fsS --cacert "$root/testdata/certs/ca.crt" \
		--resolve "www.example.com:${mtls_port}:127.0.0.1" "https://www.example.com:${mtls_port}/" >/dev/null 2>&1; then
		fail "request without client certificate succeeded"
	fi
	if curl --noproxy '*' --max-time 3 -fsS --cacert "$root/testdata/certs/ca.crt" \
		--cert "$root/testdata/certs/api.crt" --key "$root/testdata/certs/api.key" \
		--resolve "www.example.com:${mtls_port}:127.0.0.1" "https://www.example.com:${mtls_port}/" >/dev/null 2>&1; then
		fail "request with untrusted client certificate succeeded"
	fi
	trusted_body=$(curl_mtls "https://www.example.com:${mtls_port}/")
	[[ "$trusted_body" == *backend-ok* ]] || fail "trusted mTLS body=$trusted_body"
	rm -f "$tmp"/status-* "$tmp"/hold-body-*
	pids=()
	for i in $(seq 1 8); do
		(curl_mtls -o "$tmp/hold-body-$i" -w '%{http_code}' \
			"https://www.example.com:${mtls_port}/hold" >"$tmp/status-$i" 2>/dev/null || true) &
		pids+=("$!")
	done
	for pid in "${pids[@]}"; do wait "$pid"; done
	count200=0
	count503=0
	statuses=""
	for status_file in "$tmp"/status-*; do
		status=$(<"$status_file")
		statuses+="${statuses:+ }$status"
		[[ "$status" == 200 ]] && ((count200 += 1))
		[[ "$status" == 503 ]] && ((count503 += 1))
	done
	((count200 >= 1 && count503 >= 1 && count200 + count503 == 8)) ||
		fail "circuit-breaker statuses=$statuses"

	"$docker_bin" rm -f "$proxy" >/dev/null
	proxy=""
done

echo "=== assert ADS ACKs and absence of NACK"
logs=$("$docker_bin" logs "$manager" 2>&1)
grep -F "ACK received" <<<"$logs" >"$tmp/ack.log" || fail "management log contains no ACK"
for type_url in \
	"envoy.config.listener.v3.Listener" \
	"envoy.config.route.v3.RouteConfiguration" \
	"envoy.config.cluster.v3.Cluster" \
	"envoy.config.endpoint.v3.ClusterLoadAssignment" \
	"envoy.extensions.transport_sockets.tls.v3.Secret"; do
	grep -Fq "$type_url" "$tmp/ack.log" || fail "missing ACK for $type_url"
done
if grep -Eq 'NACK received|response rejected|error_detail' <<<"$logs"; then
	fail "management log contains a NACK"
fi

echo "p1-xds e2e: all P1 features passed in static and ADS modes on every supported Envoy version"

#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"
root=$(cd ../.. && pwd)
docker_bin=${DOCKER:-docker}
envoy_image=${ENVOY_IMAGE:-envoyproxy/envoy:v1.39.0}
network="esgw-extauth-$PPID-$$"
tmp=$(mktemp -d)
containers=()

cleanup() {
	local status=$?
	if ((${#containers[@]})); then
		"$docker_bin" rm -f "${containers[@]}" >/dev/null 2>&1 || true
	fi
	"$docker_bin" network rm "$network" >/dev/null 2>&1 || true
	rm -rf "$tmp"
	return "$status"
}
trap cleanup EXIT

CGO_ENABLED=0 go build -trimpath -o "$tmp/auth-server" ./server
cp Dockerfile "$tmp/"
tar -C "$tmp" -cf - auth-server Dockerfile | "$docker_bin" build -q -t esgw-extauth:e2e - >/dev/null

"$docker_bin" network create "$network" >/dev/null
backend="${network}-backend"
auth="${network}-auth"
proxy="${network}-envoy"
"$docker_bin" run -d --name "$backend" --network "$network" \
	hashicorp/http-echo -listen=:18080 -text=backend-ok >/dev/null
containers+=("$backend")
"$docker_bin" run -d --name "$auth" --network "$network" esgw-extauth:e2e >/dev/null
containers+=("$auth")
backend_ip=$("$docker_bin" inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$backend")
auth_ip=$("$docker_bin" inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$auth")

awk -v backend_ip="$backend_ip" -v auth_ip="$auth_ip" '
	/address: 127\.0\.0\.1/ {
		line = $0
		if ((getline nextline) > 0) {
			if      (nextline ~ /port_value: 18081/) sub("127.0.0.1", "0.0.0.0", line)
			else if (nextline ~ /port_value: 18082/) sub("127.0.0.1", "0.0.0.0", line)
			else if (nextline ~ /port_value: 18080/) sub("127.0.0.1", backend_ip, line)
			else if (nextline ~ /port_value: 19000/) sub("127.0.0.1", auth_ip, line)
			else if (nextline ~ /port_value: 19001/) sub("127.0.0.1", auth_ip, line)
			print line; print nextline
		} else print line
		next
	}
	{ print }
' "$root/testdata/extauth/want-static.yaml" >"$tmp/envoy.yaml"

"$docker_bin" create --name "$proxy" --network "$network" \
	-p 18081:18081 -p 18082:18082 "$envoy_image" --mode serve -c /tmp/envoy.yaml >/dev/null
"$docker_bin" cp "$tmp/envoy.yaml" "$proxy:/tmp/envoy.yaml"
"$docker_bin" start "$proxy" >/dev/null
containers+=("$proxy")

request_status() {
	local port=$1 auth_header=$2 path=${3:-/}
	curl --noproxy '*' --max-time 2 -sS -o "$tmp/body" -w '%{http_code}' \
		-H "Authorization: $auth_header" "http://127.0.0.1:${port}${path}" 2>/dev/null || true
}
wait_status() {
	local port=$1 header=$2 want=$3 path=${4:-/} got=""
	for _ in $(seq 1 100); do
		got=$(request_status "$port" "$header" "$path")
		[[ "$got" == "$want" ]] && return 0
		sleep 0.1
	done
	echo "port=$port path=$path status=$got want=$want body=$(cat "$tmp/body" 2>/dev/null || true)" >&2
	return 1
}

echo "=== HTTP extAuth allow/deny/route disabled"
wait_status 18081 allow 200 /
grep -q backend-ok "$tmp/body"
wait_status 18081 deny 403 /
wait_status 18081 deny 200 /public
grep -q backend-ok "$tmp/body"

echo "=== gRPC extAuth allow/deny"
wait_status 18082 allow 200 /
grep -q backend-ok "$tmp/body"
wait_status 18082 deny 403 /

echo "=== fail-open HTTP and fail-closed gRPC"
"$docker_bin" stop "$auth" >/dev/null
wait_status 18081 allow 200 /
grep -q backend-ok "$tmp/body"
wait_status 18082 allow 403 /

echo "extauth e2e: HTTP/gRPC allow, deny, route disable, fail-open and fail-closed passed"

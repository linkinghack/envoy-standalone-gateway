#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"
root=$(cd ../.. && pwd)
docker_bin=${DOCKER:-docker}
host_port=${E2E_MTLS_PORT:-18444}
network="esgw-mtls-circuit-$PPID-$$"
backend="esgw-mtls-backend-$PPID-$$"
proxy=""
tmp=$(mktemp -d)

cleanup() {
	local status=$?
	[[ -n "$proxy" ]] && "$docker_bin" rm -f "$proxy" >/dev/null 2>&1 || true
	"$docker_bin" rm -f "$backend" >/dev/null 2>&1 || true
	"$docker_bin" network rm "$network" >/dev/null 2>&1 || true
	rm -rf "$tmp"
	return "$status"
}
trap cleanup EXIT

CGO_ENABLED=0 go build -trimpath -o "$tmp/server" ./server
cp Dockerfile "$tmp/"
tar -C "$tmp" -cf - server Dockerfile | "$docker_bin" build -q -t esgw-mtls-backend:e2e - >/dev/null

"$docker_bin" network create --subnet 10.244.44.0/24 "$network" >/dev/null
"$docker_bin" run -d --name "$backend" --network "$network" --ip 10.244.44.3 esgw-mtls-backend:e2e >/dev/null

awk '
	/address: 127\.0\.0\.1/ {
		line = $0
		if ((getline nextline) > 0) {
			if      (nextline ~ /port_value: 18080/) sub("127.0.0.1", "10.244.44.3", line)
			else if (nextline ~ /port_value: 18444/) sub("127.0.0.1", "0.0.0.0", line)
			print line; print nextline
		} else print line
		next
	}
	{ print }
' "$root/testdata/mtls-circuit/want-static.yaml" >"$tmp/envoy.yaml"

curl_mtls() {
	curl --noproxy '*' --max-time 10 -sS \
		--cacert "$root/testdata/certs/ca.crt" \
		--cert "$root/testdata/certs/client.crt" \
		--key "$root/testdata/certs/client.key" \
		--resolve "www.example.com:${host_port}:127.0.0.1" "$@"
}

wait_ready() {
	local body=""
	for _ in $(seq 1 100); do
		if body=$(curl_mtls "https://www.example.com:${host_port}/" 2>/dev/null) && [[ "$body" == *backend-ok* ]]; then
			return 0
		fi
		sleep 0.1
	done
	echo "mTLS Envoy did not become ready; last body=$body" >&2
	return 1
}

versions=$(sed -n 's/.*EnvoyMatrixVersions = \[\]string{\(.*\)}/\1/p' "$root/internal/version/envoy.go" |
	tr -d ' "' | tr ',' ' ')
for version in $versions; do
	echo "=== downstream mTLS and circuit breaking on Envoy $version"
	proxy="esgw-mtls-envoy-${version//./-}-$PPID-$$"
	"$docker_bin" create --name "$proxy" --network "$network" --ip 10.244.44.2 \
		-p "127.0.0.1:${host_port}:18444" -w /workspace \
		"envoyproxy/envoy:v$version" --mode serve -c /workspace/envoy.yaml >/dev/null
	tar -C "$root" -cf - testdata/certs | "$docker_bin" cp - "$proxy:/workspace"
	"$docker_bin" cp "$tmp/envoy.yaml" "$proxy:/workspace/envoy.yaml"
	"$docker_bin" start "$proxy" >/dev/null
	wait_ready

	echo "--- client certificate enforcement"
	if curl --noproxy '*' --max-time 3 -fsS --cacert "$root/testdata/certs/ca.crt" \
		--resolve "www.example.com:${host_port}:127.0.0.1" \
		"https://www.example.com:${host_port}/" >/dev/null 2>&1; then
		echo "FAIL: request without client certificate succeeded" >&2
		exit 1
	fi
	echo "  OK missing client certificate rejected"
	if curl --noproxy '*' --max-time 3 -fsS --cacert "$root/testdata/certs/ca.crt" \
		--cert "$root/testdata/certs/api.crt" --key "$root/testdata/certs/api.key" \
		--resolve "www.example.com:${host_port}:127.0.0.1" \
		"https://www.example.com:${host_port}/" >/dev/null 2>&1; then
		echo "FAIL: untrusted client certificate succeeded" >&2
		exit 1
	fi
	echo "  OK untrusted client certificate rejected"
	body=$(curl_mtls "https://www.example.com:${host_port}/")
	[[ "$body" == *backend-ok* ]] || { echo "FAIL: trusted client body=$body" >&2; exit 1; }
	echo "  OK trusted client certificate accepted"

	echo "--- maxConnections=1 and maxPendingRequests=1 overload"
	rm -f "$tmp"/status-*
	pids=()
	for i in $(seq 1 8); do
		(curl_mtls -o "$tmp/body-$i" -w '%{http_code}' \
			"https://www.example.com:${host_port}/hold" >"$tmp/status-$i" 2>/dev/null || true) &
		pids+=("$!")
	done
	for pid in "${pids[@]}"; do
		wait "$pid"
	done
	statuses=""
	count200=0
	count503=0
	for status_file in "$tmp"/status-*; do
		status=$(<"$status_file")
		statuses+="${statuses:+ }$status"
		[[ "$status" == 200 ]] && ((count200 += 1))
		[[ "$status" == 503 ]] && ((count503 += 1))
	done
	if ((count200 < 1 || count503 < 1 || count200 + count503 != 8)); then
		echo "FAIL: overload statuses=$statuses (200=$count200 503=$count503)" >&2
		exit 1
	fi
	echo "  OK overload enforced (statuses=$statuses)"

	"$docker_bin" rm -f "$proxy" >/dev/null
	proxy=""
done

echo "mtls-circuit e2e: client authentication and upstream overflow passed on all versions"

#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"
root=$(cd ../.. && pwd)
docker_bin=${DOCKER:-docker}
main_network="esgw-policy-main-$PPID-$$"
outside_network="esgw-policy-outside-$PPID-$$"
main_subnet=10.243.37.0/24
outside_subnet=10.243.38.0/24
backend="esgw-policy-backend-$PPID-$$"
proxy=""
tmp=$(mktemp -d)

cleanup() {
	local status=$?
	[[ -n "$proxy" ]] && "$docker_bin" rm -f "$proxy" >/dev/null 2>&1 || true
	"$docker_bin" rm -f "$backend" >/dev/null 2>&1 || true
	"$docker_bin" network rm "$main_network" "$outside_network" >/dev/null 2>&1 || true
	rm -rf "$tmp"
	return "$status"
}
trap cleanup EXIT

CGO_ENABLED=0 go build -trimpath -o "$tmp/client" ./client
cp Dockerfile "$tmp/"
tar -C "$tmp" -cf - client Dockerfile | "$docker_bin" build -q -t esgw-policy-client:e2e - >/dev/null

"$docker_bin" network create --subnet "$main_subnet" "$main_network" >/dev/null
"$docker_bin" network create --subnet "$outside_subnet" "$outside_network" >/dev/null
"$docker_bin" run -d --name "$backend" --network "$main_network" --ip 10.243.37.3 \
	hashicorp/http-echo -listen=:18080 -text=backend-ok >/dev/null

awk '
	/address: 127\.0\.0\.1/ {
		line = $0
		if ((getline nextline) > 0) {
			if      (nextline ~ /port_value: 18080/) sub("127.0.0.1", "10.243.37.3", line)
			else if (nextline ~ /port_value: 1808[345]/) sub("127.0.0.1", "0.0.0.0", line)
			print line; print nextline
		} else print line
		next
	}
	{ print }
' "$root/testdata/ipaccess/want-static.yaml" >"$tmp/envoy.yaml"

request_status() {
	local network=$1 ip=$2 url=$3 header=${4:-} output
	local args=(--rm --network "$network" --ip "$ip" esgw-policy-client:e2e -url "$url")
	[[ -n "$header" ]] && args+=(-header "$header")
	output=$("$docker_bin" run "${args[@]}" 2>/dev/null || true)
	printf '%s' "$output" | sed -n '1p'
}

expect_status() {
	local label=$1 want=$2 network=$3 ip=$4 url=$5 header=${6:-} got
	got=$(request_status "$network" "$ip" "$url" "$header")
	if [[ "$got" != "$want" ]]; then
		echo "FAIL: $label status=$got want=$want" >&2
		return 1
	fi
	echo "  OK $label -> $got"
}

wait_ready() {
	local got=""
	for _ in $(seq 1 100); do
		got=$(request_status "$main_network" 10.243.37.8 http://10.243.37.2:18083/)
		[[ "$got" == 200 ]] && return 0
		sleep 0.1
	done
	echo "Envoy did not become ready; last status=$got" >&2
	return 1
}

versions=$(sed -n 's/.*EnvoyMatrixVersions = \[\]string{\(.*\)}/\1/p' "$root/internal/version/envoy.go" |
	tr -d ' "' | tr ',' ' ')
for version in $versions; do
	echo "=== policy real traffic on Envoy $version"
	proxy="esgw-policy-envoy-${version//./-}-$PPID-$$"
	"$docker_bin" create --name "$proxy" --network "$main_network" --ip 10.243.37.2 \
		"envoyproxy/envoy:v$version" --mode serve -c /tmp/envoy.yaml >/dev/null
	"$docker_bin" network connect --ip 10.243.38.2 "$outside_network" "$proxy"
	"$docker_bin" cp "$tmp/envoy.yaml" "$proxy:/tmp/envoy.yaml"
	"$docker_bin" start "$proxy" >/dev/null
	wait_ready

	echo "--- IP allow/deny and untrusted XFF boundary"
	expect_status "allowed source" 200 "$main_network" 10.243.37.8 http://10.243.37.2:18083/
	expect_status "deny wins" 403 "$main_network" 10.243.37.9 http://10.243.37.2:18083/
	expect_status "outside allow list" 403 "$outside_network" 10.243.38.8 http://10.243.38.2:18083/
	expect_status "allowed peer cannot be denied by spoofed XFF" 200 "$main_network" 10.243.37.8 \
		http://10.243.37.2:18083/ "X-Forwarded-For:10.243.37.9"
	expect_status "denied peer cannot bypass with spoofed XFF" 403 "$main_network" 10.243.37.9 \
		http://10.243.37.2:18083/ "X-Forwarded-For:10.243.37.8"

	echo "--- independent clientIP token buckets"
	for want in 200 200 429; do
		expect_status "client .8 quota" "$want" "$main_network" 10.243.37.8 http://10.243.37.2:18084/
	done
	for want in 200 200 429; do
		expect_status "client .10 quota" "$want" "$main_network" 10.243.37.10 http://10.243.37.2:18084/
	done

	echo "--- independent header token buckets and missing-header fallback"
	for tenant in alpha beta; do
		for want in 200 200 429; do
			expect_status "tenant $tenant quota" "$want" "$main_network" 10.243.37.8 \
				http://10.243.37.2:18085/ "x-tenant:$tenant"
		done
	done
	for want in 200 200 429; do
		expect_status "missing tenant shared fallback" "$want" "$main_network" 10.243.37.8 \
			http://10.243.37.2:18085/
	done

	"$docker_bin" rm -f "$proxy" >/dev/null
	proxy=""
done

echo "policy e2e: IP access and independent dynamic rate-limit buckets passed on all versions"

#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"
root=$(cd ../.. && pwd)
docker_bin=${DOCKER:-docker}
image=esgw-topology-matrix:e2e
network=esgw-topology-matrix
tmp=$(mktemp -d)
containers=()
volumes=()

cleanup_scenario() {
	if ((${#containers[@]})); then
		"$docker_bin" rm -f "${containers[@]}" >/dev/null 2>&1 || true
	fi
	containers=()
}
cleanup() {
	local status=$?
	cleanup_scenario
	if ((${#volumes[@]})); then
		"$docker_bin" volume rm "${volumes[@]}" >/dev/null 2>&1 || true
	fi
	"$docker_bin" network rm "$network" >/dev/null 2>&1 || true
	rm -rf "$tmp"
	return "$status"
}
trap cleanup EXIT

CGO_ENABLED=0 make -C "$root" build >/dev/null
cp "$root/bin/esgw" "$tmp/esgw"
cp Dockerfile management-loop.sh ./*.yaml "$tmp/"
"$root/bin/esgw" bootstrap -c external-xds.yaml -o "$tmp/external-xds-bootstrap.yaml"
tar -C "$tmp" -cf - . | "$docker_bin" build --progress=plain -t "$image" -

"$docker_bin" network rm "$network" >/dev/null 2>&1 || true
"$docker_bin" network create --subnet 172.31.26.0/24 "$network" >/dev/null
"$docker_bin" run -d --name esgw-matrix-old --network "$network" --ip 172.31.26.11 \
	hashicorp/http-echo -listen=:8080 -text=old-backend >/dev/null
"$docker_bin" run -d --name esgw-matrix-new --network "$network" --ip 172.31.26.12 \
	hashicorp/http-echo -listen=:8080 -text=new-backend >/dev/null
containers+=(esgw-matrix-old esgw-matrix-new)

wait_ready() {
	local port=$1
	for _ in $(seq 1 150); do
		curl --noproxy '*' --max-time 1 -fsS "http://127.0.0.1:${port}/readyz" >/dev/null 2>&1 && return 0
		sleep 0.1
	done
	return 1
}

wait_traffic() {
	local port=$1 expected=$2 body
	for _ in $(seq 1 150); do
		body=$(curl --noproxy '*' --max-time 1 -fsS -H 'Host: gateway.test' "http://127.0.0.1:${port}/" 2>/dev/null || true)
		[[ "$body" == *"$expected"* ]] && return 0
		sleep 0.1
	done
	return 1
}

assert_zero_gap_restart() {
	local management=$1 api_port=$2 proxy_port=$3 expected=$4 managed=$5
	local management_pid envoy_pid_before envoy_pid_after failures="$tmp/failures"
	management_pid=$("$docker_bin" exec "$management" cat /run/esgw-management.pid)
	if [[ "$managed" == true ]]; then
		envoy_pid_before=$("$docker_bin" exec "$management" sed -n 's/.*"pid":\([0-9]*\).*/\1/p' /var/lib/esgw/run/proc.json)
	fi
	: >"$failures"
	(
		for _ in $(seq 1 100); do
			curl --noproxy '*' --max-time 1 -fsS -H 'Host: gateway.test' "http://127.0.0.1:${proxy_port}/" >/dev/null 2>&1 \
				|| printf 'failure\n' >>"$failures"
			sleep 0.03
		done
	) &
	local probe_pid=$!
	"$docker_bin" exec "$management" kill -TERM "$management_pid"
	for _ in $(seq 1 100); do
		local current
		current=$("$docker_bin" exec "$management" cat /run/esgw-management.pid 2>/dev/null || true)
		[[ -n "$current" && "$current" != "$management_pid" ]] && break
		sleep 0.1
	done
	wait "$probe_pid"
	[[ ! -s "$failures" ]]
	wait_ready "$api_port"
	wait_traffic "$proxy_port" "$expected"
	if [[ "$managed" == true ]]; then
		envoy_pid_after=$("$docker_bin" exec "$management" sed -n 's/.*"pid":\([0-9]*\).*/\1/p' /var/lib/esgw/run/proc.json)
		[[ "$envoy_pid_after" == "$envoy_pid_before" ]]
	fi
}

run_managed() {
	local mode=$1 api_port=$2 proxy_port=$3 address=$4 name
	name="esgw-matrix-managed-${mode}"
	echo "=== T1 ${mode}: managed Envoy"
	"$docker_bin" run -d --name "$name" --network "$network" --ip "$address" \
		-p "${api_port}:8080" -p "${proxy_port}:10080" "$image" \
		/usr/local/bin/esgw-management-loop "/fixtures/managed-${mode}.yaml" /fixtures/gateway-old.yaml >/dev/null
	containers+=("$name")
	wait_ready "$api_port"
	wait_traffic "$proxy_port" old-backend
	assert_zero_gap_restart "$name" "$api_port" "$proxy_port" old-backend true
}

run_external_xds() {
	local management=esgw-matrix-external-xds envoy=esgw-matrix-external-xds-envoy
	echo "=== T2 xds: external Envoy"
	"$docker_bin" run -d --name "$management" --network "$network" --ip 172.31.26.21 \
		-p 19203:8080 -p 19103:10080 "$image" /usr/local/bin/esgw-management-loop \
		/fixtures/external-xds.yaml /fixtures/gateway-old.yaml >/dev/null
	containers+=("$management")
	wait_ready 19203
	"$docker_bin" run -d --name "$envoy" --network "container:${management}" "$image" \
		/usr/local/bin/envoy -c /fixtures/external-xds-bootstrap.yaml >/dev/null
	containers+=("$envoy")
	wait_traffic 19103 old-backend
	assert_zero_gap_restart "$management" 19203 19103 old-backend false
}

run_external_static() {
	local management=esgw-matrix-external-static envoy=esgw-matrix-external-static-envoy volume=esgw-matrix-static-data
	echo "=== T2 static: file-only external Envoy"
	"$docker_bin" volume rm "$volume" >/dev/null 2>&1 || true
	"$docker_bin" volume create "$volume" >/dev/null
	volumes+=("$volume")
	"$docker_bin" run -d --name "$management" --network "$network" --ip 172.31.26.22 \
		-p 19204:8080 -v "$volume:/var/lib/esgw" "$image" /usr/local/bin/esgw-management-loop \
		/fixtures/external-static.yaml /fixtures/gateway-old.yaml >/dev/null
	containers+=("$management")
	wait_ready 19204
	"$docker_bin" run -d --name "$envoy" --network "$network" --ip 172.31.26.23 \
		-p 19104:10080 -v "$volume:/var/lib/esgw" "$image" /usr/local/bin/envoy \
		-c /var/lib/esgw/envoy/envoy.yaml >/dev/null
	containers+=("$envoy")
	wait_traffic 19104 old-backend
	assert_zero_gap_restart "$management" 19204 19104 old-backend false

	local before after management_pid
	before=$("$docker_bin" exec "$management" sha256sum /var/lib/esgw/envoy/envoy.yaml | awk '{print $1}')
	"$docker_bin" exec "$management" cp /fixtures/gateway-invalid.yaml /var/lib/esgw/config.d/gateway.yaml
	management_pid=$("$docker_bin" exec "$management" cat /run/esgw-management.pid)
	"$docker_bin" exec "$management" kill -TERM "$management_pid"
	sleep 1
	after=$("$docker_bin" exec "$management" sha256sum /var/lib/esgw/envoy/envoy.yaml | awk '{print $1}')
	[[ "$after" == "$before" ]]
	wait_traffic 19104 old-backend

	"$docker_bin" exec "$management" cp /fixtures/gateway-new.yaml /var/lib/esgw/config.d/gateway.yaml
	for _ in $(seq 1 150); do
		after=$("$docker_bin" exec "$management" sha256sum /var/lib/esgw/envoy/envoy.yaml 2>/dev/null | awk '{print $1}')
		[[ -n "$after" && "$after" != "$before" ]] && break
		sleep 0.1
	done
	[[ "$after" != "$before" ]]
	wait_ready 19204
	wait_traffic 19104 old-backend
	"$docker_bin" restart "$envoy" >/dev/null
	wait_traffic 19104 new-backend
}

run_managed xds 19201 19101 172.31.26.20
run_managed static 19202 19102 172.31.26.24
run_external_xds
run_external_static
echo "topology matrix: T1/T2 x xDS/static passed with zero-gap management restarts"

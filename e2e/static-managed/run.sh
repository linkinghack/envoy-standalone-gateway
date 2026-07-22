#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"
ROOT="$(cd ../.. && pwd)"
IMAGE="esgw-static-managed:e2e"
GATEWAY="esgw-s7-gateway"
BACKEND="esgw-s7-backend"
NETWORK="esgw-s7-network"
API_PORT="${E2E_STATIC_API_PORT:-19080}"
PROXY_PORT="${E2E_STATIC_PROXY_PORT:-19081}"
ADMIN_PASSWORD="static-e2e-password"
tmp="$(mktemp -d)"

cleanup_runtime() {
	docker rm -f "$GATEWAY" "$BACKEND" >/dev/null 2>&1 || true
	docker network rm "$NETWORK" >/dev/null 2>&1 || true
}
cleanup() {
	local status=$?
	if [[ $status -ne 0 ]]; then
		echo "=== runtime network/config diagnostics" >&2
		docker inspect "$BACKEND" --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' >&2 || true
		docker exec "$GATEWAY" cat /var/lib/esgw/envoy/envoy.yaml >&2 || true
		if [[ -s "$tmp/login.json" ]]; then
			echo "=== login response" >&2
			cat "$tmp/login.json" >&2
		fi
		if [[ -s "$tmp/draft-response.json" ]]; then
			echo "=== draft response" >&2
			cat "$tmp/draft-response.json" >&2
		fi
		if [[ -s "$tmp/replace-response.json" ]]; then
			echo "=== replace response" >&2
			cat "$tmp/replace-response.json" >&2
		fi
		for response in bad-response.json bad-publish-response.json; do
			if [[ -s "$tmp/$response" ]]; then
				echo "=== $response" >&2
				cat "$tmp/$response" >&2
			fi
		done
		echo "=== gateway lifecycle logs (failure tail)" >&2
		docker logs "$GATEWAY" 2>&1 | grep -E 'time=|shutting down admin|terminating parent|closing and draining|error:|caught SIG|exiting' | tail -80 >&2 || true
		echo "=== backend logs (failure tail)" >&2
		docker logs "$BACKEND" 2>&1 | tail -40 >&2 || true
	fi
	cleanup_runtime
	rm -rf "$tmp"
	return "$status"
}
trap cleanup EXIT
cleanup_runtime

echo "=== build esgw and no-bind-mount e2e image"
CGO_ENABLED=0 make -C "$ROOT" build
cp "$ROOT/bin/esgw" "$tmp/esgw"
cp Dockerfile entrypoint.sh esgw.yaml gateway.yaml "$tmp/"
tar -C "$tmp" -cf - . | docker build --progress=plain -t "$IMAGE" -

echo "=== start backend and static-managed gateway"
docker network create --subnet 172.31.25.0/24 "$NETWORK" >/dev/null
docker run -d --name "$BACKEND" --network "$NETWORK" --ip 172.31.25.11 \
	hashicorp/http-echo -listen=:8080 -text=managed-static-backend >/dev/null
docker run -d --name "$GATEWAY" --network "$NETWORK" --ip 172.31.25.10 \
	-e ESGW_INITIAL_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
	-p "${API_PORT}:8080" -p "${PROXY_PORT}:10080" "$IMAGE" >/dev/null

wait_ready() {
	local i
	for i in $(seq 1 100); do
		if curl --noproxy '*' --max-time 1 -fsS "http://127.0.0.1:${API_PORT}/readyz" >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.1
	done
	docker logs "$GATEWAY" | tail -80 >&2
	return 1
}
wait_ready

probe_traffic() {
	local body
	body=$(curl --noproxy '*' --max-time 1 -fsS -H 'Host: gateway.test' \
		"http://127.0.0.1:${PROXY_PORT}/" 2>/dev/null) \
		&& [[ "$body" == *managed-static-backend* ]]
}

wait_traffic() {
	local i
	for i in $(seq 1 50); do
		probe_traffic && return 0
		sleep 0.1
	done
	return 1
}

echo "=== assert epoch 0 and initial traffic"
wait_traffic
record=$(docker exec "$GATEWAY" cat /var/lib/esgw/run/proc.json)
[[ $(jq -r .epoch <<<"$record") == 0 ]]
envoy_pid_0=$(jq -r .pid <<<"$record")

echo "=== authenticate and change the draft"
cookie="$tmp/cookie.txt"
login_status=$(curl --noproxy '*' -sS -o "$tmp/login.json" -w '%{http_code}' -c "$cookie" \
	-H 'Content-Type: application/json' -H 'X-ESGW-Request: 1' \
	-d "{\"username\":\"admin\",\"password\":\"${ADMIN_PASSWORD}\"}" \
	"http://127.0.0.1:${API_PORT}/api/v1/auth/login")
[[ "$login_status" == 200 ]]
draft_status=$(curl --noproxy '*' -sS -o "$tmp/draft-response.json" -w '%{http_code}' -b "$cookie" \
	"http://127.0.0.1:${API_PORT}/api/v1/config/draft")
[[ "$draft_status" == 200 ]]
mv "$tmp/draft-response.json" "$tmp/draft.json"
jq '{sourceType, expectedResourceVersion: .resourceVersion, files: (.files | map(.content |= sub("weight: 1"; "weight: 2")))}' \
	"$tmp/draft.json" >"$tmp/replace.json"
replace_status=$(curl --noproxy '*' -sS -o "$tmp/replace-response.json" -w '%{http_code}' -b "$cookie" \
	-H 'Content-Type: application/json' -H 'X-ESGW-Request: 1' \
	-X PUT --data-binary "@$tmp/replace.json" \
	"http://127.0.0.1:${API_PORT}/api/v1/config/draft")
[[ "$replace_status" == 200 ]]
mv "$tmp/replace-response.json" "$tmp/replaced.json"
resource_version=$(jq -r .draftResourceVersion "$tmp/replaced.json")

echo "=== publish while continuously probing traffic"
failures="$tmp/traffic-failures"
(
	for _ in $(seq 1 120); do
		probe_traffic || printf 'failure\n' >>"$failures"
		sleep 0.05
	done
) &
traffic_pid=$!
jq -n --arg rv "$resource_version" '{message: "static hot restart e2e", expectedResourceVersion: $rv}' >"$tmp/publish.json"
curl --noproxy '*' -fsS -b "$cookie" -H 'Content-Type: application/json' -H 'X-ESGW-Request: 1' \
	--data-binary "@$tmp/publish.json" \
	"http://127.0.0.1:${API_PORT}/api/v1/config/publish" >"$tmp/published.json"
wait "$traffic_pid"
[[ ! -s "$failures" ]]

record=$(docker exec "$GATEWAY" cat /var/lib/esgw/run/proc.json)
[[ $(jq -r .epoch <<<"$record") == 1 ]]
envoy_pid_1=$(jq -r .pid <<<"$record")
[[ "$envoy_pid_1" != "$envoy_pid_0" ]]
probe_traffic

echo "=== restart only management plane and verify adoption"
management_pid_0=$(docker exec "$GATEWAY" cat /run/esgw-management.pid)
docker exec "$GATEWAY" sh -c "kill -0 ${envoy_pid_1} && readlink /proc/${envoy_pid_1}/exe" >"$tmp/adoption-before-kill.txt"
echo "Envoy before management stop: alive $(cat "$tmp/adoption-before-kill.txt")"
docker exec "$GATEWAY" kill -TERM "$management_pid_0"
sleep 0.2
if docker exec "$GATEWAY" sh -c "kill -0 ${envoy_pid_1} && readlink /proc/${envoy_pid_1}/exe" >"$tmp/adoption-preflight.txt" 2>&1; then
	echo "Envoy adoption preflight: alive $(cat "$tmp/adoption-preflight.txt")"
else
	echo "Envoy adoption preflight: process ${envoy_pid_1} already exited" >&2
fi
for _ in $(seq 1 100); do
	management_pid_1=$(docker exec "$GATEWAY" cat /run/esgw-management.pid 2>/dev/null || true)
	if [[ -n "$management_pid_1" && "$management_pid_1" != "$management_pid_0" ]]; then
		break
	fi
	sleep 0.1
done
[[ "$management_pid_1" != "$management_pid_0" ]]
wait_ready
record=$(docker exec "$GATEWAY" cat /var/lib/esgw/run/proc.json)
echo "management PID ${management_pid_0} -> ${management_pid_1}; Envoy record: ${record}"
[[ $(jq -r .epoch <<<"$record") == 1 ]]
[[ $(jq -r .pid <<<"$record") == "$envoy_pid_1" ]]
probe_traffic

echo "=== reject a bad draft without touching the live artifact"
artifact_hash=$(docker exec "$GATEWAY" sha256sum /var/lib/esgw/envoy/envoy.yaml | awk '{print $1}')
curl --noproxy '*' -fsS -b "$cookie" "http://127.0.0.1:${API_PORT}/api/v1/config/draft" >"$tmp/good-draft.json"
jq '{sourceType, expectedResourceVersion: .resourceVersion, files: (.files | map(.content |= sub("port: 10080"; "port: 70000")))}' \
	"$tmp/good-draft.json" >"$tmp/bad-replace.json"
bad_status=$(curl --noproxy '*' -sS -o "$tmp/bad-response.json" -w '%{http_code}' -b "$cookie" \
	-H 'Content-Type: application/json' -H 'X-ESGW-Request: 1' -X PUT \
	--data-binary "@$tmp/bad-replace.json" "http://127.0.0.1:${API_PORT}/api/v1/config/draft")
[[ "$bad_status" == 422 ]]
[[ $(jq -r .error.code "$tmp/bad-response.json") == VALIDATION_FAILED ]]
record=$(docker exec "$GATEWAY" cat /var/lib/esgw/run/proc.json)
[[ $(jq -r .epoch <<<"$record") == 1 ]]
[[ $(jq -r .pid <<<"$record") == "$envoy_pid_1" ]]
[[ $(docker exec "$GATEWAY" sha256sum /var/lib/esgw/envoy/envoy.yaml | awk '{print $1}') == "$artifact_hash" ]]
probe_traffic

echo "static-managed e2e: epoch 0 -> 1, zero failed probes, management restart adopted PID ${envoy_pid_1}, bad draft preserved last-good"

#!/usr/bin/env bash
# Real-traffic TCP, TLS passthrough/SNI, and UDP smoke against the L4 golden artifact.
set -euo pipefail

cd "$(dirname "$0")"
root=$(cd ../.. && pwd)
docker_bin=${DOCKER:-docker}
envoy_image=${ENVOY_IMAGE:-envoyproxy/envoy:v1.39.0}
network="esgw-l4-$PPID-$$"
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

# Golden endpoints use loopback for deterministic snapshots. Rewrite only socket
# addresses selected by their following port, preserving the checked-in artifact as
# the single source for listener/filter structure.
awk '
	/address: 127\.0\.0\.1/ {
		line = $0
		if ((getline nextline) > 0) {
			if      (nextline ~ /port_value: 13306/) sub("127.0.0.1", "0.0.0.0", line)
			else if (nextline ~ /port_value: 18443/) sub("127.0.0.1", "0.0.0.0", line)
			else if (nextline ~ /port_value: 15353/) sub("127.0.0.1", "0.0.0.0", line)
			else if (nextline ~ /port_value: 23306/) sub("127.0.0.1", "172.31.27.11", line)
			else if (nextline ~ /port_value: 24443/) sub("127.0.0.1", "172.31.27.12", line)
			else if (nextline ~ /port_value: 25443/) sub("127.0.0.1", "172.31.27.13", line)
			else if (nextline ~ /port_value: 25353/) sub("127.0.0.1", "172.31.27.14", line)
			print line
			print nextline
		} else print line
		next
	}
	{ print }
' "$root/testdata/l4/want-static.yaml" >"$tmp/envoy.yaml"

"$docker_bin" network create --subnet 172.31.27.0/24 "$network" >/dev/null

tcp_backend="${network}-tcp"
"$docker_bin" run -d --name "$tcp_backend" --network "$network" --ip 172.31.27.11 alpine:3.22 \
	sh -c 'while true; do printf "HTTP/1.1 200 OK\r\nContent-Length: 11\r\nConnection: close\r\n\r\ntcp-backend" | nc -l -p 23306; done' >/dev/null
containers+=("$tcp_backend")

start_tls_backend() {
	local name=$1 address=$2 port=$3 cert=$4
	"$docker_bin" create --name "$name" --network "$network" --ip "$address" "$envoy_image" \
		sh -c "openssl s_server -quiet -accept $port -cert /tmp/tls.crt -key /tmp/tls.key -www" >/dev/null
	"$docker_bin" cp "$root/testdata/certs/$cert.crt" "$name:/tmp/tls.crt"
	"$docker_bin" cp "$root/testdata/certs/$cert.key" "$name:/tmp/tls.key"
	"$docker_bin" start "$name" >/dev/null
	containers+=("$name")
}
start_tls_backend "${network}-api" 172.31.27.12 24443 api
start_tls_backend "${network}-web" 172.31.27.13 25443 www

udp_backend="${network}-udp"
"$docker_bin" run -d --name "$udp_backend" --network "$network" --ip 172.31.27.14 alpine:3.22 \
	nc -u -lk -p 25353 -e /bin/cat >/dev/null
containers+=("$udp_backend")

proxy="${network}-envoy"
"$docker_bin" create --name "$proxy" --network "$network" --ip 172.31.27.20 \
	-p 13306:13306 -p 18443:18443 -p 15353:15353/udp "$envoy_image" \
	--mode serve -c /tmp/envoy.yaml >/dev/null
"$docker_bin" cp "$tmp/envoy.yaml" "$proxy:/tmp/envoy.yaml"
"$docker_bin" start "$proxy" >/dev/null
containers+=("$proxy")

echo "=== TCP raw proxy"
tcp_body=""
for _ in $(seq 1 100); do
	tcp_body=$(curl --noproxy '*' --max-time 1 -fsS http://127.0.0.1:13306/ 2>/dev/null || true)
	[[ "$tcp_body" == "tcp-backend" ]] && break
	sleep 0.1
done
[[ "$tcp_body" == "tcp-backend" ]]

tls_subject() {
	local server_name=$1
	openssl s_client -connect 127.0.0.1:18443 -servername "$server_name" </dev/null 2>/dev/null \
		| openssl x509 -noout -subject 2>/dev/null || true
}

echo "=== TLS passthrough SNI routes"
api_subject=""
for _ in $(seq 1 100); do
	api_subject=$(tls_subject api.example.com)
	[[ "$api_subject" == *"CN = api.example.com"* ]] && break
	sleep 0.1
done
[[ "$api_subject" == *"CN = api.example.com"* ]]
web_subject=$(tls_subject www.example.com)
[[ "$web_subject" == *"CN = www.example.com"* ]]
unknown_subject=$(tls_subject unknown.example.org)
[[ -z "$unknown_subject" ]]

echo "=== UDP proxy echo"
udp_reply=""
for _ in $(seq 1 100); do
	udp_reply=$(printf 'udp-backend\n' | nc -u -w 1 127.0.0.1 15353 2>/dev/null || true)
	[[ "$udp_reply" == "udp-backend" ]] && break
	sleep 0.1
done
[[ "$udp_reply" == "udp-backend" ]]

echo "l4 e2e: TCP, TLS passthrough SNI, unknown-SNI rejection, and UDP passed"

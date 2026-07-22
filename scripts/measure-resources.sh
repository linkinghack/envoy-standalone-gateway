#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
pid=""
cleanup() {
	local status=$?
	if [[ -n "$pid" ]]; then
		kill -TERM "$pid" >/dev/null 2>&1 || true
		wait "$pid" >/dev/null 2>&1 || true
	fi
	rm -rf "$tmp"
	return "$status"
}
trap cleanup EXIT

read -r xds_port api_port < <(python3 - <<'PY'
import socket
sockets=[]
ports=[]
for _ in range(2):
    s=socket.socket()
    s.bind(("127.0.0.1", 0))
    sockets.append(s)
    ports.append(s.getsockname()[1])
print(*ports)
PY
)
mkdir -p "$tmp/data/config.d"
{
	printf 'dataDir: %s\n' "$tmp/data"
	printf 'deliver:\n  mode: xds\n  xds:\n    listen: 127.0.0.1:%s\n    adminAddress: 127.0.0.1:29901\n' "$xds_port"
	printf 'proc:\n  enabled: false\napi:\n  listen: 127.0.0.1:%s\n  topology: sidecar\n' "$api_port"
} >"$tmp/esgw.yaml"

CGO_ENABLED=0 make -C "$root" build >/dev/null
ESGW_INITIAL_ADMIN_PASSWORD=resource-baseline-password "$root/bin/esgw" serve -c "$tmp/esgw.yaml" >"$tmp/esgw.log" 2>&1 &
pid=$!
for _ in $(seq 1 100); do
	"$root/bin/esgw" healthcheck --url "http://127.0.0.1:${api_port}/readyz" --timeout 200ms >/dev/null 2>&1 && break
	sleep 0.1
done
"$root/bin/esgw" healthcheck --url "http://127.0.0.1:${api_port}/readyz"
# Argon2id bootstrap intentionally allocates 64 MiB. Measure resident steady
# state after Go's background scavenger has had time to return that burst.
sleep 10

rss_kib=$(awk '/^VmRSS:/ {print $2}' "/proc/${pid}/status")
peak_kib=$(awk '/^VmHWM:/ {print $2}' "/proc/${pid}/status")
binary_bytes=$(stat -c %s "$root/bin/esgw")
target_kib=$((150 * 1024))
printf 'management_rss_kib=%s\nmanagement_peak_kib=%s\nbinary_bytes=%s\ntarget_kib=%s\n' \
	"$rss_kib" "$peak_kib" "$binary_bytes" "$target_kib"
if ((rss_kib >= target_kib)); then
	echo "error: management RSS exceeds 150 MiB target" >&2
	exit 1
fi

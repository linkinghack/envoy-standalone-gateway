#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
docker_bin=${DOCKER:-docker}
tmp=$(mktemp -d)
management=esgw-image-smoke-management
all_in_one=esgw-image-smoke-all-in-one

cleanup() {
	local status=$?
	"$docker_bin" rm -f "$management" "$all_in_one" >/dev/null 2>&1 || true
	rm -rf "$tmp"
	return "$status"
}
trap cleanup EXIT

CGO_ENABLED=0 make -C "$root" build >/dev/null
install -d -m 0750 "$tmp/rootfs/etc/esgw" "$tmp/rootfs/run/esgw"
install -d -m 0700 "$tmp/rootfs/var/lib/esgw/config.d" "$tmp/rootfs/var/lib/esgw/envoy" "$tmp/rootfs/var/lib/esgw/run"
install -m 0755 "$root/bin/esgw" "$tmp/esgw"

build_image() {
	local dockerfile=$1 image=$2 config=$3
	cp "$root/packaging/docker/$dockerfile" "$tmp/Dockerfile"
	cp "$root/packaging/docker/$config" "$tmp/esgw.yaml"
	tar -C "$tmp" -cf - . | "$docker_bin" build --progress=plain -t "$image" -
}

build_image Dockerfile.esgw-prebuilt esgw:smoke esgw.yaml
build_image Dockerfile.all-in-one-prebuilt esgw-all-in-one:smoke esgw-all-in-one.yaml

for image in esgw:smoke esgw-all-in-one:smoke; do
	test "$("$docker_bin" image inspect "$image" --format '{{.Config.User}}')" = "65532:65532"
	"$docker_bin" image inspect "$image" --format '{{json .Config.Healthcheck.Test}}' | grep -q 'esgw.*healthcheck.*readyz'
done

"$docker_bin" run -d --name "$management" -e ESGW_INITIAL_ADMIN_PASSWORD=image-smoke-password esgw:smoke >/dev/null
"$docker_bin" run -d --name "$all_in_one" -e ESGW_INITIAL_ADMIN_PASSWORD=image-smoke-password esgw-all-in-one:smoke >/dev/null
for container in "$management" "$all_in_one"; do
	for _ in $(seq 1 100); do
		if "$docker_bin" exec "$container" /usr/local/bin/esgw healthcheck >/dev/null 2>&1; then
			break
		fi
		sleep 0.1
	done
	"$docker_bin" exec "$container" /usr/local/bin/esgw healthcheck
	test "$("$docker_bin" inspect "$container" --format '{{.Config.User}}')" = "65532:65532"
done

"$docker_bin" exec "$all_in_one" /usr/local/bin/envoy --version | grep -q '1.39.0'
echo "container images: non-root metadata and readiness probes ok"

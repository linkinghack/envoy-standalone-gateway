#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
docker_bin=${DOCKER:-docker}

for path in \
	docs/README.md docs/quickstart.md docs/configuration.md docs/operations.md \
	docs/backup-restore.md docs/upgrade.md docs/security.md \
	packaging/examples/quickstart-gateway.yaml \
	design_docs/system_design/260716-1-gateway-config-protocol-v0.md; do
	test -e "$root/$path"
done

CGO_ENABLED=0 make -C "$root" build >/dev/null
"$root/bin/esgw" compile -f "$root/packaging/examples" --mode xds >/dev/null
"$docker_bin" compose -f "$root/packaging/compose/quickstart.yaml" config --quiet
"$docker_bin" compose -f "$root/packaging/compose/all-in-one.yaml" config --quiet
"$docker_bin" compose -f "$root/packaging/compose/separated-xds.yaml" config --quiet

grep -q 'docs/quickstart.md' "$root/README.md"
grep -q 'ESGW_INITIAL_ADMIN_PASSWORD' "$root/docs/quickstart.md"
grep -q 'KillMode=process' "$root/docs/operations.md"
grep -q '65532' "$root/docs/security.md"
echo "documentation examples and deployment configs: ok"

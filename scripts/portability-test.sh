#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
	goos=${target%/*}
	goarch=${target#*/}
	(cd "$root" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -o "$tmp/esgw-${goos}-${goarch}" ./cmd/esgw)
done
echo "portability: linux/darwin amd64/arm64 builds ok"

#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
version=${VERSION:-$(git -C "$root" describe --tags --always)}
dist=${DIST_DIR:-"$root/dist/$version"}
case "$version" in
	*[!A-Za-z0-9._-]*) echo "error: VERSION contains unsafe archive characters: $version" >&2; exit 2 ;;
esac
if [[ -e "$dist" ]]; then
	echo "error: release output already exists: $dist" >&2
	exit 1
fi
for tool in go node go-licenses syft sha256sum tar; do
	command -v "$tool" >/dev/null || { echo "error: required release tool is missing: $tool" >&2; exit 1; }
done

stage=$(mktemp -d)
cleanup() { rm -rf "$stage"; }
trap cleanup EXIT
mkdir -p "$dist" "$stage/meta"

echo "=== validate dependency license policy"
(cd "$root" && go-licenses check --confidence_threshold=0.75 --disallowed_types=forbidden \
	--ignore github.com/linkinghack/envoy-standalone-gateway ./cmd/esgw)
(cd "$root" && node scripts/dependency-licenses.mjs) >"$stage/meta/THIRD_PARTY_LICENSES.csv"

echo "=== generate SPDX SBOM"
(cd "$root" && syft scan dir:. --quiet --exclude './.git/**' --exclude './web/node_modules/**' \
	--exclude './dist/**' --source-name "esgw-${version}" -o "spdx-json=$stage/meta/esgw-${version}.spdx.json")

source_date_epoch=${SOURCE_DATE_EPOCH:-$(git -C "$root" log -1 --format=%ct)}
for arch in amd64 arm64; do
	name="esgw-${version}-linux-${arch}"
	prefix="$stage/$name"
	echo "=== build $name"
	install -d "$prefix/bin"
	(cd "$root" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath \
		-ldflags "-s -w -X github.com/linkinghack/envoy-standalone-gateway/internal/version.Version=${version}" \
		-o "$prefix/bin/esgw" ./cmd/esgw)
	cp "$root/LICENSE" "$root/README.md" "$prefix/"
	cp -R "$root/docs" "$root/packaging" "$root/protocol" "$prefix/"
	cp "$stage/meta/THIRD_PARTY_LICENSES.csv" "$stage/meta/esgw-${version}.spdx.json" "$prefix/"
	tar --sort=name --mtime="@${source_date_epoch}" --owner=0 --group=0 --numeric-owner \
		-C "$stage" -czf "$dist/${name}.tar.gz" "$name"
	done

cp "$stage/meta/THIRD_PARTY_LICENSES.csv" "$stage/meta/esgw-${version}.spdx.json" "$dist/"
(cd "$dist" && sha256sum ./*.tar.gz ./THIRD_PARTY_LICENSES.csv ./*.spdx.json >SHA256SUMS)
for archive in "$dist"/*.tar.gz; do
	archive_list="$stage/archive-list.txt"
	tar -tzf "$archive" >"$archive_list"
	grep -q '/bin/esgw$' "$archive_list"
	grep -q '/packaging/systemd/esgw.service$' "$archive_list"
	grep -q '/protocol/schema/v1alpha1.json$' "$archive_list"
	grep -q '/protocol/examples/valid/minimal-http/config.yaml$' "$archive_list"
	grep -q '/THIRD_PARTY_LICENSES.csv$' "$archive_list"
	grep -q '\.spdx\.json$' "$archive_list"
done
echo "release artifacts: $dist"

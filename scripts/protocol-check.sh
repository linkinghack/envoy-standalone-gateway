#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
task_tmp=$(mktemp -d)
cleanup() { rm -rf "$task_tmp"; }
trap cleanup EXIT

echo "=== build standalone conformance CLI"
(cd "$root" && CGO_ENABLED=0 go build -trimpath -o "$task_tmp/esgw" ./cmd/esgw)
mkdir -p "$task_tmp/bundle"
cp -R "$root/protocol/." "$task_tmp/bundle/"

cd "$task_tmp/bundle"
echo "=== schema clean-diff"
"$task_tmp/esgw" schema -o "$task_tmp/v1alpha1.json"
cmp schema/v1alpha1.json "$task_tmp/v1alpha1.json"

echo "=== valid examples"
for example in examples/valid/*; do
	[[ -d "$example" ]] || continue
	"$task_tmp/esgw" conformance -f "$example" -o "$task_tmp/report.json"
	cmp "$example/expected.json" "$task_tmp/report.json"
	echo "  OK $example"
done

echo "=== invalid examples"
for example in examples/invalid/*; do
	[[ -d "$example" ]] || continue
	set +e
	"$task_tmp/esgw" conformance -f "$example" -o "$task_tmp/report.json"
	status=$?
	set -e
	if [[ $status -ne 1 ]]; then
		echo "error: $example exited $status, want 1" >&2
		exit 1
	fi
	cmp "$example/expected.json" "$task_tmp/report.json"
	echo "  OK $example"
done

echo "protocol-check: schema and conformance bundle are reproducible outside the repository"

#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
stage=$(mktemp -d)
fixture=$(mktemp -d)
cleanup() { rm -rf "$stage" "$fixture"; }
trap cleanup EXIT

printf '#!/bin/sh\necho first\n' >"$fixture/esgw-v1"
printf '#!/bin/sh\necho second\n' >"$fixture/esgw-v2"
chmod 0755 "$fixture/esgw-v1" "$fixture/esgw-v2"

DESTDIR="$stage" "$root/packaging/scripts/install.sh" "$fixture/esgw-v1" >/dev/null
test -x "$stage/usr/local/bin/esgw"
test "$(stat -c %a "$stage/var/lib/esgw")" = 700
test "$(stat -c %a "$stage/etc/esgw/esgw.yaml")" = 640
grep -q '^KillMode=process$' "$stage/usr/lib/systemd/system/esgw.service"
grep -q '^ProtectSystem=strict$' "$stage/usr/lib/systemd/system/esgw.service"

printf '\n# operator change\n' >>"$stage/etc/esgw/esgw.yaml"
DESTDIR="$stage" "$root/packaging/scripts/install.sh" "$fixture/esgw-v1" >/dev/null
grep -q 'operator change' "$stage/etc/esgw/esgw.yaml"
DESTDIR="$stage" "$root/packaging/scripts/upgrade.sh" "$fixture/esgw-v2" >/dev/null
grep -q 'operator change' "$stage/etc/esgw/esgw.yaml"
previous=$(find "$stage/usr/local/bin" -maxdepth 1 -name 'esgw.previous.*' -type f)
test -n "$previous"
grep -q 'first' "$previous"
grep -q 'second' "$stage/usr/local/bin/esgw"

DESTDIR="$stage" "$root/packaging/scripts/uninstall.sh" >/dev/null
test ! -e "$stage/usr/local/bin/esgw"
test -e "$stage/etc/esgw/esgw.yaml"
test -d "$stage/var/lib/esgw"
DESTDIR="$stage" "$root/packaging/scripts/uninstall.sh" --purge >/dev/null
test ! -e "$stage/etc/esgw"
test ! -e "$stage/var/lib/esgw"
echo "packaging install lifecycle: ok"

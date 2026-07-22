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
printf '#!/bin/sh\nprintf "systemctl %%s\\n" "$*" >>"$LIFECYCLE_LOG"\n' >"$fixture/systemctl"
printf '#!/bin/sh\nprintf "tmpfiles %%s\\n" "$*" >>"$LIFECYCLE_LOG"\n' >"$fixture/systemd-tmpfiles"
chmod 0755 "$fixture/systemctl" "$fixture/systemd-tmpfiles"

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

LIFECYCLE_LOG="$fixture/lifecycle.log"
export LIFECYCLE_LOG
(
	PATH="$fixture:$PATH"
	destdir=
	. "$root/packaging/scripts/lib.sh"
	reload_and_restart
)
grep -q '^systemctl daemon-reload$' "$LIFECYCLE_LOG"
grep -q '^systemctl enable esgw.service$' "$LIFECYCLE_LOG"
grep -q '^systemctl restart esgw.service$' "$LIFECYCLE_LOG"
echo "packaging install lifecycle: ok"

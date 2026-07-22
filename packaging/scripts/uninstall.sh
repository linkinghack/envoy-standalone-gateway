#!/bin/sh
set -eu
. "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/lib.sh"

purge=false
[ "${1:-}" = "--purge" ] && purge=true
require_root
if [ -z "$destdir" ]; then
	systemctl disable --now esgw.service >/dev/null 2>&1 || true
fi
rm -f "$(root_path /usr/local/bin/esgw)" "$(root_path /usr/local/bin)"/esgw.previous.* \
	"$(root_path /usr/lib/systemd/system/esgw.service)" "$(root_path /usr/lib/tmpfiles.d/esgw.conf)"
if $purge; then
	rm -rf "$(root_path /etc/esgw)" "$(root_path /var/lib/esgw)"
	if [ -z "$destdir" ]; then
		userdel esgw >/dev/null 2>&1 || true
		groupdel esgw >/dev/null 2>&1 || true
	fi
fi
if [ -z "$destdir" ]; then
	systemctl daemon-reload
fi
echo "esgw uninstalled (configuration and data retained unless --purge was supplied)"

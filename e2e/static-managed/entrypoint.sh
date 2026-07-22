#!/bin/sh
set -eu

child=""
stop() {
	if [ -n "$child" ]; then
		kill -TERM "$child" 2>/dev/null || true
		wait "$child" 2>/dev/null || true
	fi
	exit 0
}
trap stop TERM INT

while true; do
	/usr/local/bin/esgw serve -c /etc/esgw/esgw.yaml &
	child=$!
	printf '%s\n' "$child" >/run/esgw-management.pid
	wait "$child" || true
	child=""
	sleep 0.2
done

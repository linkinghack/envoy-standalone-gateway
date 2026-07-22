#!/bin/sh
set -eu

config=$1
source_file=$2
mkdir -p /var/lib/esgw/config.d /var/lib/esgw/envoy /var/lib/esgw/run
if [ ! -e /var/lib/esgw/config.d/gateway.yaml ]; then
	cp "$source_file" /var/lib/esgw/config.d/gateway.yaml
fi
while true; do
	/usr/local/bin/esgw serve -c "$config" &
	child=$!
	printf '%s\n' "$child" >/run/esgw-management.pid
	wait "$child" || true
	sleep 0.2
done

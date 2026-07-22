#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
destdir=${DESTDIR:-}
root_path() { printf '%s%s\n' "$destdir" "$1"; }

atomic_install() {
	source=$1
	target=$2
	mode=$3
	temp="$target.new"
	install -m "$mode" "$source" "$temp"
	mv -f "$temp" "$target"
}

require_root() {
	if [ -z "$destdir" ] && [ "$(id -u)" -ne 0 ]; then
		echo "error: run as root (or set DESTDIR for a staged install)" >&2
		exit 1
	fi
}

ensure_account() {
	[ -n "$destdir" ] && return 0
	getent group esgw >/dev/null 2>&1 || groupadd --system esgw
	if ! getent passwd esgw >/dev/null 2>&1; then
		useradd --system --gid esgw --home-dir /var/lib/esgw --shell /usr/sbin/nologin esgw
	fi
}

install_layout() {
	install -d -m 0755 "$(root_path /usr/local/bin)" "$(root_path /usr/lib/systemd/system)" "$(root_path /usr/lib/tmpfiles.d)"
	install -d -m 0750 "$(root_path /etc/esgw)"
	install -d -m 0700 "$(root_path /var/lib/esgw)" "$(root_path /var/lib/esgw/config.d)" \
		"$(root_path /var/lib/esgw/certs)" "$(root_path /var/lib/esgw/envoy)" "$(root_path /var/lib/esgw/run)"
	if [ -z "$destdir" ]; then
		chown -R esgw:esgw /var/lib/esgw /etc/esgw
	fi
}

install_support_files() {
	atomic_install "$repo_root/packaging/systemd/esgw.service" "$(root_path /usr/lib/systemd/system/esgw.service)" 0644
	atomic_install "$repo_root/packaging/systemd/esgw.tmpfiles" "$(root_path /usr/lib/tmpfiles.d/esgw.conf)" 0644
	atomic_install "$repo_root/packaging/config/esgw.env.example" "$(root_path /etc/esgw/esgw.env.example)" 0644
	if [ ! -e "$(root_path /etc/esgw/esgw.yaml)" ]; then
		atomic_install "$repo_root/packaging/config/esgw.yaml" "$(root_path /etc/esgw/esgw.yaml)" 0640
	fi
}

reload_and_start() {
	[ -n "$destdir" ] && return 0
	systemctl daemon-reload
	systemd-tmpfiles --create esgw.conf
	systemctl enable --now esgw.service
}

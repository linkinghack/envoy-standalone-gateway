#!/bin/sh
set -eu
. "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/lib.sh"

binary=${1:-"$repo_root/bin/esgw"}
if [ ! -x "$binary" ]; then
	echo "error: esgw binary is not executable: $binary" >&2
	exit 1
fi
require_root
ensure_account
install_layout
installed=$(root_path /usr/local/bin/esgw)
if [ -e "$installed" ]; then
	backup="$installed.previous.$(date -u +%Y%m%d%H%M%S)"
	cp -p "$installed" "$backup"
fi
atomic_install "$binary" "$installed" 0755
install_support_files
reload_and_start
echo "esgw upgraded; previous binaries use /usr/local/bin/esgw.previous.<UTC timestamp>"

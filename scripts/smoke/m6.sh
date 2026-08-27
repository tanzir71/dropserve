#!/bin/sh
set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
binary=${1:-"$repository_root/bin/dropserve"}
case "$binary" in
  /*) ;;
  *) binary=$(CDPATH= cd -- "$(dirname -- "$binary")" && pwd)/$(basename -- "$binary") ;;
esac

work_directory=$(mktemp -d)
cleanup() {
  rm -rf -- "$work_directory"
}
trap cleanup EXIT INT TERM

config_directory="$work_directory/config"
fake_bin="$work_directory/bin"
mkdir -p "$config_directory" "$fake_bin"
ln -s /bin/true "$fake_bin/systemctl"

XDG_CONFIG_HOME="$config_directory" PATH="$fake_bin:$PATH" "$binary" autostart enable
unit_path="$config_directory/systemd/user/dropserve.service"
test -f "$unit_path"
systemd-analyze verify "$unit_path"
grep -F 'ExecStart=' "$unit_path" | grep -F -- '--background' >/dev/null
grep -Fx 'Restart=on-failure' "$unit_path" >/dev/null
grep -Fx 'WantedBy=default.target' "$unit_path" >/dev/null

XDG_CONFIG_HOME="$config_directory" PATH="$fake_bin:$PATH" "$binary" autostart disable
test ! -e "$unit_path"

echo "M6 Linux autostart smoke passed: the user unit was written, verified, enabled, and removed."

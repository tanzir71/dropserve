#!/usr/bin/env sh
set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
binary=${1:-"$repository_root/bin/dropserve"}
fixtures_root="$repository_root/testdata/fixtures"
work_dir=$(mktemp -d)
output_file="$work_dir/stdout.log"
error_file="$work_dir/stderr.log"
state_path="$work_dir/state.json"
server_pid=

cleanup() {
    if [ -n "$server_pid" ]; then
        kill "$server_pid" 2>/dev/null || true
        wait "$server_pid" 2>/dev/null || true
    fi
    rm -rf -- "$work_dir"
}
trap cleanup EXIT INT TERM

"$binary" serve --listen 127.0.0.1:0 --root "$fixtures_root" --state "$state_path" >"$output_file" 2>"$error_file" &
server_pid=$!

address=
attempt=0
while [ "$attempt" -lt 450 ]; do
    if ! kill -0 "$server_pid" 2>/dev/null; then
        echo "Dropserve exited before becoming ready:" >&2
        sed -n '1,80p' "$error_file" >&2
        exit 1
    fi
    address=$(sed -n 's/^Dropserve is ready at \(http:\/\/[^[:space:]]*\)$/\1/p' "$output_file" | head -n 1)
    [ -n "$address" ] && break
    attempt=$((attempt + 1))
    sleep 0.1
done
[ -n "$address" ] || { echo "Dropserve did not print a ready address within 45 seconds" >&2; exit 1; }

[ "$(curl --fail --silent --show-error "$address/node/")" = "Dropserve Node fixture" ]
[ "$(curl --fail --silent --show-error "$address/python/")" = "Dropserve Python fixture" ]
curl --fail --silent --show-error "$address/" | grep -q 'id="log-dialog"'
apps=$(curl --fail --silent --show-error "$address/_dropserve/api/apps")
printf '%s' "$apps" | grep -q '"slug":"broken"'
printf '%s' "$apps" | grep -q '"status":"crashed"'
logs=$(curl --fail --silent --show-error "$address/_dropserve/api/logs/broken")
printf '%s' "$logs" | grep -q '"status":"crashed"'
printf '%s' "$logs" | grep -q '"attempts":5'
printf '%s' "$logs" | grep -q 'intentional fixture failure'

echo "M4 smoke passed: Node and Python returned 200; broken was isolated after 5 starts with logs at $address/"

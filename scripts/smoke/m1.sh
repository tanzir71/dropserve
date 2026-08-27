#!/usr/bin/env sh
set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
binary=${1:-"$repository_root/bin/dropserve"}
fixtures_root="$repository_root/testdata/fixtures"
work_dir=$(mktemp -d)
output_file="$work_dir/stdout.log"
error_file="$work_dir/stderr.log"
server_pid=

cleanup() {
    if [ -n "$server_pid" ]; then
        kill "$server_pid" 2>/dev/null || true
        wait "$server_pid" 2>/dev/null || true
    fi
    rm -rf -- "$work_dir"
}
trap cleanup EXIT INT TERM

"$binary" serve --listen 127.0.0.1:0 --root "$fixtures_root" >"$output_file" 2>"$error_file" &
server_pid=$!

address=
attempt=0
while [ "$attempt" -lt 100 ]; do
    if ! kill -0 "$server_pid" 2>/dev/null; then
        echo "Dropserve exited before becoming ready:" >&2
        sed -n '1,40p' "$error_file" >&2
        exit 1
    fi
    address=$(sed -n 's/^Dropserve is ready at \(http:\/\/[^[:space:]]*\)$/\1/p' "$output_file" | head -n 1)
    [ -n "$address" ] && break
    attempt=$((attempt + 1))
    sleep 0.1
done
[ -n "$address" ] || {
    echo "Dropserve did not print a ready address within 10 seconds" >&2
    exit 1
}

body=$(curl --fail --silent --show-error "$address/static/")
printf '%s' "$body" | grep -q '<h1>Static fixture</h1>'
echo "M1 smoke passed: $address/static/ returned 200"


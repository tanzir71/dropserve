#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
binary=${1:-"$repo_root/bin/dropserve"}
fixtures="$repo_root/testdata/fixtures"
log_file=$(mktemp)
dashboard_file=$(mktemp)
apps_file=$(mktemp)
search_file=$(mktemp)
qr_file=$(mktemp)

"$binary" serve --listen 127.0.0.1:0 --root "$fixtures" >"$log_file" 2>&1 &
server_pid=$!
cleanup() {
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
    rm -f "$log_file" "$dashboard_file" "$apps_file" "$search_file" "$qr_file"
}
trap cleanup EXIT INT TERM

address=""
attempt=0
while [ "$attempt" -lt 450 ]; do
    address=$(sed -n 's/^Dropserve is ready at \(http:\/\/[^ ]*\)$/\1/p' "$log_file" | head -n 1)
    [ -n "$address" ] && break
    kill -0 "$server_pid" 2>/dev/null || { cat "$log_file" >&2; exit 1; }
    attempt=$((attempt + 1))
    sleep 0.1
done
[ -n "$address" ] || { echo "Dropserve did not become ready" >&2; exit 1; }

curl --fail --silent --show-error "$address/" -o "$dashboard_file"
grep -q 'id="app-search"' "$dashboard_file"
curl --fail --silent --show-error "$address/_dropserve/api/apps" -o "$apps_file"
for required_slug in field-notes invoice-desk kitchen-timer static; do
    grep -q "\"slug\":\"$required_slug\"" "$apps_file"
done
curl --fail --silent --show-error "$address/_dropserve/api/search?q=observations" -o "$search_file"
grep -q '"slug":"field-notes"' "$search_file"
curl --fail --silent --show-error --get --data-urlencode "url=$address/field-notes/" "$address/_dropserve/api/qr" -o "$qr_file"
[ "$(wc -c < "$qr_file" | tr -d ' ')" -ge 300 ]
[ "$(od -An -tx1 -N8 "$qr_file" | tr -d ' \n')" = "89504e470d0a1a0a" ]

echo "M2 smoke passed: dashboard rendered the required fixtures; README search and local QR succeeded at $address/"

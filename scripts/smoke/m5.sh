#!/bin/sh
set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
binary=${1:-"$repository_root/bin/dropserve"}
fixtures_root="$repository_root/testdata/fixtures"
work_directory=$(mktemp -d "${TMPDIR:-/tmp}/dropserve-m5-smoke.XXXXXX")
apps_root="$work_directory/apps"
state_path="$work_directory/state.json"
output_path="$work_directory/dropserve.out"
error_path="$work_directory/dropserve.err"
process_id=""

cleanup() {
    if [ -n "$process_id" ] && kill -0 "$process_id" 2>/dev/null; then
        kill "$process_id" 2>/dev/null || true
        wait "$process_id" 2>/dev/null || true
    fi
    case "$work_directory" in
        "${TMPDIR:-/tmp}"/dropserve-m5-smoke.*) rm -rf -- "$work_directory" ;;
    esac
}
trap cleanup EXIT INT TERM

mkdir "$apps_root"
cp -R "$fixtures_root/absolute-paths" "$apps_root/absolute-paths"
cp -R "$fixtures_root/subpath" "$apps_root/subpath"

start_dropserve() {
    : >"$output_path"
    : >"$error_path"
    "$binary" serve --listen 127.0.0.1:0 --root "$apps_root" --state "$state_path" >"$output_path" 2>"$error_path" &
    process_id=$!
    attempt=0
    while [ "$attempt" -lt 180 ]; do
        if ! kill -0 "$process_id" 2>/dev/null; then
            printf 'Dropserve exited before readiness:\n' >&2
            cat "$error_path" >&2
            exit 1
        fi
        address=$(sed -n 's/^Dropserve is ready at \(http:\/\/[^ ]*\)$/\1/p' "$output_path" | head -n 1)
        if [ -n "$address" ]; then
            return
        fi
        attempt=$((attempt + 1))
        sleep 0.25
    done
    printf 'Dropserve did not become ready\n' >&2
    exit 1
}

stop_dropserve() {
    if [ -n "$process_id" ] && kill -0 "$process_id" 2>/dev/null; then
        kill "$process_id"
        wait "$process_id" 2>/dev/null || true
    fi
    process_id=""
}

start_dropserve

headers=$(curl -fsS -D - -o /dev/null "$address/subpath/redirect")
printf '%s' "$headers" | grep -qi '^location: /subpath/login'
headers=$(curl -fsS -D - -o /dev/null "$address/subpath/cookie")
printf '%s' "$headers" | grep -qi '^set-cookie: .*Path=/subpath/'
curl -fsS "$address/subpath/html-no-base" | grep -q '<head><base href="/subpath/">'
[ "$(curl -fsS "$address/subpath/asset.json")" = '{"markup":"<head>json</head>"}' ]
curl -fsS "$address/subpath/headers" | grep -q '"prefix":"/subpath"'

node - "$address" <<'NODE'
const address = process.argv[2].replace(/^http/, "ws") + "/subpath/ws";
const socket = new WebSocket(address);
const timeout = setTimeout(() => {
  console.error("WebSocket smoke timed out");
  process.exit(1);
}, 5000);
socket.addEventListener("open", () => socket.send("m5 smoke echo"));
socket.addEventListener("message", event => {
  clearTimeout(timeout);
  if (event.data !== "m5 smoke echo") process.exit(1);
  socket.close();
});
socket.addEventListener("error", () => process.exit(1));
NODE

apps_json=$(curl -fsS "$address/_dropserve/api/apps")
first_port=$(printf '%s' "$apps_json" | node -e '
let data = "";
process.stdin.on("data", chunk => data += chunk);
process.stdin.on("end", () => {
  const app = JSON.parse(data).find(item => item.slug === "absolute-paths");
  if (!app || !app.prefers_own_port || !app.urls.own) process.exit(1);
  process.stdout.write(String(app.port));
});')
own_url=$(printf '%s' "$apps_json" | node -e '
let data = "";
process.stdin.on("data", chunk => data += chunk);
process.stdin.on("end", () => process.stdout.write(JSON.parse(data).find(item => item.slug === "absolute-paths").urls.own));')
curl -fsS "$own_url" | grep -q 'Absolute paths fixture'
dashboard_script=$(curl -fsS "$address/_dropserve/app.js")
printf '%s' "$dashboard_script" | grep -q 'This app expects to live at the root'
printf '%s' "$dashboard_script" | grep -q 'Use the short URL anyway'

stop_dropserve
start_dropserve
second_port=$(curl -fsS "$address/_dropserve/api/apps" | node -e '
let data = "";
process.stdin.on("data", chunk => data += chunk);
process.stdin.on("end", () => process.stdout.write(String(JSON.parse(data).find(item => item.slug === "absolute-paths").port)));')
[ "$second_port" = "$first_port" ]

printf 'M5 smoke passed: rewrites, headers, WebSocket echo, own-port rescue, and stable port %s worked at %s/\n' "$first_port" "$address"

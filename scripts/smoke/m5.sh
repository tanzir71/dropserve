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

fail() {
    printf 'M5 smoke failed: %s\n' "$1" >&2
    exit 1
}

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

redirect_headers="$work_directory/redirect.headers"
redirect_status=$(curl -sS -D "$redirect_headers" -o "$work_directory/redirect.body" -w '%{http_code}' "$address/subpath/redirect")
[ "$redirect_status" = "302" ] || fail "redirect returned HTTP $redirect_status instead of 302"
grep -Fqi 'location: /subpath/login' "$redirect_headers" || fail "redirect Location was not /subpath/login"

cookie_headers="$work_directory/cookie.headers"
curl -fsS -D "$cookie_headers" -o "$work_directory/cookie.body" "$address/subpath/cookie"
grep -Eqi '^set-cookie: .*Path=/subpath/' "$cookie_headers" || fail "cookie Path was not rewritten under /subpath/"

html_path="$work_directory/response.html"
curl -fsS -o "$html_path" "$address/subpath/html-no-base"
grep -Fq '<head><base href="/subpath/">' "$html_path" || fail "HTML base element was not injected"

asset_body=$(curl -fsS "$address/subpath/asset.json")
[ "$asset_body" = '{"markup":"<head>json</head>"}' ] || fail "non-HTML response changed through the proxy"

forwarded_headers="$work_directory/forwarded.json"
curl -fsS -o "$forwarded_headers" "$address/subpath/headers"
grep -Fq '"prefix":"/subpath"' "$forwarded_headers" || fail "X-Forwarded-Prefix was incorrect"
grep -Fq '"scriptName":"/subpath"' "$forwarded_headers" || fail "X-Script-Name was incorrect"
grep -Fq '"proto":"http"' "$forwarded_headers" || fail "X-Forwarded-Proto was incorrect"

node - "$address" <<'NODE'
const crypto = require("node:crypto");
const net = require("node:net");
const target = new URL(process.argv[2]);
const key = crypto.randomBytes(16).toString("base64");
const expectedAccept = crypto
  .createHash("sha1")
  .update(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
  .digest("base64");
const socket = net.createConnection(Number(target.port), target.hostname);
let buffered = Buffer.alloc(0);
let upgraded = false;
let complete = false;
const timeout = setTimeout(() => {
  console.error("WebSocket smoke timed out");
  socket.destroy();
  process.exitCode = 1;
}, 5000);

function fail(message) {
  if (complete) return;
  complete = true;
  clearTimeout(timeout);
  console.error(message);
  socket.destroy();
  process.exitCode = 1;
}

function sendText(message) {
  const payload = Buffer.from(message);
  const mask = crypto.randomBytes(4);
  const frame = Buffer.alloc(6 + payload.length);
  frame[0] = 0x81;
  frame[1] = 0x80 | payload.length;
  mask.copy(frame, 2);
  for (let index = 0; index < payload.length; index++) {
    frame[6 + index] = payload[index] ^ mask[index % 4];
  }
  socket.write(frame);
}

function readEcho() {
  if (buffered.length < 2) return;
  const length = buffered[1] & 0x7f;
  if ((buffered[0] & 0x0f) !== 1 || length > 125 || buffered.length < 2 + length) return;
  const echoed = buffered.subarray(2, 2 + length).toString("utf8");
  if (echoed !== "m5 smoke echo") {
    fail(`WebSocket echoed ${JSON.stringify(echoed)} instead of the expected text`);
    return;
  }
  complete = true;
  clearTimeout(timeout);
  socket.destroy();
}

socket.on("connect", () => {
  socket.write(
    "GET /subpath/ws HTTP/1.1\r\n" +
    `Host: ${target.host}\r\n` +
    "Upgrade: websocket\r\n" +
    "Connection: Upgrade\r\n" +
    `Sec-WebSocket-Key: ${key}\r\n` +
    "Sec-WebSocket-Version: 13\r\n\r\n"
  );
});
socket.on("data", chunk => {
  buffered = Buffer.concat([buffered, chunk]);
  if (!upgraded) {
    const boundary = buffered.indexOf("\r\n\r\n");
    if (boundary < 0) return;
    const headers = buffered.subarray(0, boundary).toString("latin1");
    buffered = buffered.subarray(boundary + 4);
    if (!headers.startsWith("HTTP/1.1 101") || !headers.toLowerCase().includes(`sec-websocket-accept: ${expectedAccept.toLowerCase()}`)) {
      fail(`WebSocket handshake was invalid: ${headers.split("\r\n")[0]}`);
      return;
    }
    upgraded = true;
    sendText("m5 smoke echo");
  }
  readEcho();
});
socket.on("error", error => fail(`WebSocket smoke failed: ${error.message}`));
socket.on("close", () => {
  if (!complete) fail("WebSocket closed before echoing the test frame");
});
NODE

apps_path="$work_directory/apps.json"
curl -fsS -o "$apps_path" "$address/_dropserve/api/apps"
first_port=$(node -e '
let data = "";
process.stdin.on("data", chunk => data += chunk);
process.stdin.on("end", () => {
  const app = JSON.parse(data).find(item => item.slug === "absolute-paths");
  if (!app || !app.prefers_own_port || !app.urls.own) {
    console.error("Absolute-path fixture did not prefer its own port");
    process.exit(1);
  }
  process.stdout.write(String(app.port));
});' <"$apps_path")
own_url=$(node -e '
let data = "";
process.stdin.on("data", chunk => data += chunk);
process.stdin.on("end", () => process.stdout.write(JSON.parse(data).find(item => item.slug === "absolute-paths").urls.own));' <"$apps_path")
own_body="$work_directory/own.html"
curl -fsS -o "$own_body" "$own_url"
grep -Fq 'Absolute paths fixture' "$own_body" || fail "own-port URL did not serve the fixture at its root"
dashboard_script="$work_directory/dashboard.js"
curl -fsS -o "$dashboard_script" "$address/_dropserve/app.js"
grep -Fq 'This app expects to live at the root' "$dashboard_script" || fail "dashboard omitted the own-port explanation"
grep -Fq 'Use the short URL anyway' "$dashboard_script" || fail "dashboard omitted the short-URL rescue action"

stop_dropserve
start_dropserve
curl -fsS -o "$apps_path" "$address/_dropserve/api/apps"
second_port=$(node -e '
let data = "";
process.stdin.on("data", chunk => data += chunk);
process.stdin.on("end", () => process.stdout.write(String(JSON.parse(data).find(item => item.slug === "absolute-paths").port)));' <"$apps_path")
[ "$second_port" = "$first_port" ] || fail "own-port assignment changed after restart"

printf 'M5 smoke passed: rewrites, headers, WebSocket echo, own-port rescue, and stable port %s worked at %s/\n' "$first_port" "$address"

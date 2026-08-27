const crypto = require("node:crypto");
const http = require("node:http");

const assets = {
  "/asset.json": ["application/json", Buffer.from('{"markup":"<head>json</head>"}')],
  "/asset.js": ["application/javascript", Buffer.from('window.fixture = "<head>js</head>";')],
  "/asset.css": ["text/css", Buffer.from('body::before { content: "<head>css</head>"; }')],
  "/asset.png": ["image/png", Buffer.from("89504e470d0a1a0a0000000d49484452", "hex")],
};

const server = http.createServer((request, response) => {
  if (assets[request.url]) {
    const [contentType, body] = assets[request.url];
    response.writeHead(200, { "content-type": contentType, "content-length": body.length });
    response.end(body);
    return;
  }
  if (request.url === "/redirect") {
    response.writeHead(302, { location: "/login" });
    response.end();
    return;
  }
  if (request.url === "/login") {
    response.end("subpath login");
    return;
  }
  if (request.url === "/cookie") {
    response.writeHead(200, { "set-cookie": "s=1; Path=/; HttpOnly" });
    response.end("cookie set");
    return;
  }
  if (request.url === "/html-no-base") {
    response.setHeader("content-type", "text/html; charset=utf-8");
    response.end("<!doctype html><html><head><title>No base</title></head><body>plain</body></html>");
    return;
  }
  if (request.url === "/html-with-base") {
    response.setHeader("content-type", "text/html; charset=utf-8");
    response.end("<!doctype html><html><head><base href=\"/custom/\"><title>Base</title></head><body>kept</body></html>");
    return;
  }
  if (request.url === "/large-html") {
    const prefix = Buffer.from("<!doctype html><html><head><title>Large</title></head><body>");
    const suffix = Buffer.from("</body></html>");
    const fillBytes = (5 << 20) - prefix.length - suffix.length;
    response.writeHead(200, { "content-type": "text/html; charset=utf-8" });
    response.write(prefix);
    response.write(Buffer.alloc(fillBytes, "x"));
    response.end(suffix);
    return;
  }
  if (request.url === "/headers") {
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({
      prefix: request.headers["x-forwarded-prefix"] || "",
      scriptName: request.headers["x-script-name"] || "",
      host: request.headers["x-forwarded-host"] || "",
      proto: request.headers["x-forwarded-proto"] || "",
    }));
    return;
  }
  response.end("subpath fixture");
});

server.on("upgrade", (request, socket) => {
  if (request.url !== "/ws") {
    socket.destroy();
    return;
  }
  const accept = crypto
    .createHash("sha1")
    .update(request.headers["sec-websocket-key"] + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
    .digest("base64");
  socket.write(
    "HTTP/1.1 101 Switching Protocols\r\n" +
    "Upgrade: websocket\r\n" +
    "Connection: Upgrade\r\n" +
    `Sec-WebSocket-Accept: ${accept}\r\n\r\n`
  );
  let buffered = Buffer.alloc(0);
  socket.on("data", chunk => {
    buffered = Buffer.concat([buffered, chunk]);
    if (buffered.length < 6) return;
    const length = buffered[1] & 0x7f;
    if (length > 125 || buffered.length < 6 + length) {
      if (length > 125) socket.destroy();
      return;
    }
    const mask = buffered.subarray(2, 6);
    const payload = Buffer.from(buffered.subarray(6, 6 + length));
    for (let index = 0; index < payload.length; index++) payload[index] ^= mask[index % 4];
    socket.end(Buffer.concat([Buffer.from([0x81, payload.length]), payload]));
  });
});

server.listen(Number(process.env.PORT), process.env.HOST);

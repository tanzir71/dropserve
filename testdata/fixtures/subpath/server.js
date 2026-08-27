const http = require("node:http");

http.createServer((request, response) => {
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
  response.end("subpath fixture");
}).listen(Number(process.env.PORT), process.env.HOST);

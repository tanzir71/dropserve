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
  response.end("subpath fixture");
}).listen(Number(process.env.PORT), process.env.HOST);

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
  response.end("subpath fixture");
}).listen(Number(process.env.PORT), process.env.HOST);

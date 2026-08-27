const http = require("node:http");

const index = '<!doctype html><html><head><title>Absolute paths</title></head><body><script src="/app.js"></script><h1>Absolute paths fixture</h1></body></html>';

http.createServer((request, response) => {
  if (request.url === "/app.js") {
    response.writeHead(200, { "content-type": "application/javascript" });
    response.end("window.absolutePathsFixture = true;");
    return;
  }
  response.writeHead(200, { "content-type": "text/html; charset=utf-8" });
  response.end(index);
}).listen(Number(process.env.PORT), process.env.HOST);

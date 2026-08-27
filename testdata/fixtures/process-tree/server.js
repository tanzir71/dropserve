const http = require("node:http");
const { spawn } = require("node:child_process");
const path = require("node:path");

const child = spawn(process.execPath, [path.join(__dirname, "grandchild.js")], {
  stdio: "ignore",
  windowsHide: true,
});

http
  .createServer((_request, response) => {
    response.writeHead(200, { "content-type": "text/plain" });
    response.end(String(child.pid));
  })
  .listen(Number(process.env.PORT), process.env.HOST);

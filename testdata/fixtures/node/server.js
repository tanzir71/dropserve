const http = require('http');
const port = Number(process.env.PORT);
const host = process.env.HOST || '127.0.0.1';

http.createServer((request, response) => {
  response.writeHead(200, { 'Content-Type': 'text/plain; charset=utf-8' });
  if (request.url === '/pid') {
    response.end(String(process.pid));
    return;
  }
  response.end('Dropserve Node fixture');
}).listen(port, host);

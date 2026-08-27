const http = require('http');
const port = Number(process.env.PORT);
const host = process.env.HOST || '127.0.0.1';

http.createServer((_request, response) => {
  response.writeHead(200, { 'Content-Type': 'text/plain; charset=utf-8' });
  response.end('Dropserve Node fixture');
}).listen(port, host);

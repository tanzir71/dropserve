import os
from http.server import BaseHTTPRequestHandler, HTTPServer

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"Dropserve Python fixture")

    def log_message(self, _format, *_args):
        return

HTTPServer((os.environ.get("HOST", "127.0.0.1"), int(os.environ["PORT"])), Handler).serve_forever()

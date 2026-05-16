#!/usr/bin/env python3
import http.server
import socketserver
import urllib.parse
from http import HTTPStatus


class TinyHTTPServer(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        # Parse path and query
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path
        query = urllib.parse.parse_qs(parsed.query)

        if path == "/ping":
            self.send_response(HTTPStatus.OK)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(b"pong")

        elif path == "/sum":
            # Check for required parameters - they must exist and have non-empty values
            if "a" not in query or "b" not in query:
                self.send_response(HTTPStatus.BAD_REQUEST)
                self.send_header("Content-Type", "text/plain")
                self.end_headers()
                self.wfile.write(b"Missing parameters")
                return

            # Get parameter values
            a_str = query["a"][0] if query["a"] else ""
            b_str = query["b"][0] if query["b"] else ""

            # Check if values are non-empty
            if not a_str or not b_str:
                self.send_response(HTTPStatus.BAD_REQUEST)
                self.send_header("Content-Type", "text/plain")
                self.end_headers()
                self.wfile.write(b"Invalid parameters")
                return

            try:
                a = int(a_str)
                b = int(b_str)
            except ValueError:
                self.send_response(HTTPStatus.BAD_REQUEST)
                self.send_header("Content-Type", "text/plain")
                self.end_headers()
                self.wfile.write(b"Invalid parameters")
                return

            result = a + b
            self.send_response(HTTPStatus.OK)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(str(result).encode())

        else:
            self.send_response(HTTPStatus.NOT_FOUND)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(b"Not found")


def run_server():
    host = "127.0.0.1"
    port = 8765

    # Create server with allow_reuse_address to avoid "address already in use"
    socketserver.TCPServer.allow_reuse_address = True
    with socketserver.TCPServer((host, port), TinyHTTPServer) as httpd:
        print(f"Serving on http://{host}:{port}")
        httpd.serve_forever()


if __name__ == "__main__":
    run_server()

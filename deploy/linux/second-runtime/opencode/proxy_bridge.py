#!/usr/bin/env python3
"""Expose one fixed OpenAI-compatible route from a host-owned Unix socket."""

import http.client
import os
import signal
import socket
import subprocess
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


SOCKET_PATH = os.environ.get("ZOE_OPENCODE_PROVIDER_SOCKET", "/run/zoe/provider.sock")
ALLOWED = {
    ("GET", "/v1/models"),
    ("POST", "/v1/chat/completions"),
}


class UnixHTTPConnection(http.client.HTTPConnection):
    def __init__(self, socket_path):
        super().__init__("localhost")
        self.socket_path = socket_path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect(self.socket_path)


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        self.forward()

    def do_POST(self):
        self.forward()

    def forward(self):
        if (self.command, self.path.split("?", 1)[0]) not in ALLOWED:
            self.send_error(404)
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            self.send_error(400)
            return
        if length < 0 or length > 32 * 1024 * 1024:
            self.send_error(413)
            return
        body = self.rfile.read(length) if length else None
        headers = {
            key: value
            for key, value in self.headers.items()
            if key.lower() not in {"host", "connection", "proxy-authorization", "transfer-encoding"}
        }
        headers["Connection"] = "close"
        conn = UnixHTTPConnection(SOCKET_PATH)
        response_started = False
        try:
            conn.request(self.command, self.path, body=body, headers=headers)
            response = conn.getresponse()
            self.send_response(response.status, response.reason)
            for key, value in response.getheaders():
                if key.lower() not in {"connection", "transfer-encoding", "keep-alive", "proxy-authenticate"}:
                    self.send_header(key, value)
            self.send_header("Connection", "close")
            self.end_headers()
            response_started = True
            while True:
                # read1 returns bytes available from the current socket read;
                # read(amt) can wait for the entire buffer on a live SSE stream.
                chunk = response.read1(64 * 1024)
                if not chunk:
                    break
                self.wfile.write(chunk)
                self.wfile.flush()
        except (OSError, http.client.HTTPException):
            if not response_started and not self.wfile.closed:
                try:
                    self.send_error(502)
                except OSError:
                    pass
        finally:
            conn.close()

    def log_message(self, _format, *_args):
        # Request paths are fixed, but bodies and authorization values remain private.
        return


def main(argv):
    try:
        separator = argv.index("--")
    except ValueError:
        print("OpenCode runtime bridge requires -- followed by the ACP command", file=sys.stderr)
        return 2
    command = argv[separator + 1 :]
    if not command:
        print("OpenCode runtime bridge requires an ACP command", file=sys.stderr)
        return 2
    server = ThreadingHTTPServer(("127.0.0.1", 47632), Handler)
    server.daemon_threads = True
    thread = threading.Thread(target=server.serve_forever, name="opencode-provider-bridge", daemon=True)
    thread.start()
    child = subprocess.Popen(command, start_new_session=True)

    def stop(_signum, _frame):
        try:
            os.killpg(child.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    try:
        return child.wait()
    finally:
        try:
            os.killpg(child.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))

import http.client
import socket
import tempfile
import threading
import unittest
from http.server import ThreadingHTTPServer
from pathlib import Path

import proxy_bridge


class ProxyBridgeStreamingTest(unittest.TestCase):
    def test_forwards_first_sse_chunk_while_upstream_remains_open(self):
        with tempfile.TemporaryDirectory() as directory:
            listener = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            proxy_bridge.SOCKET_PATH = str(Path(directory) / "upstream.sock")
            listener.bind(proxy_bridge.SOCKET_PATH)
            listener.listen(1)
            release = threading.Event()
            first_chunk = b"data: first\n\n"

            def upstream():
                connection, _ = listener.accept()
                with connection:
                    request = b""
                    while b"\r\n\r\n" not in request:
                        request += connection.recv(4096)
                    connection.sendall(
                        b"HTTP/1.1 200 OK\r\n"
                        b"Content-Type: text/event-stream\r\n"
                        b"Connection: close\r\n\r\n" + first_chunk
                    )
                    release.wait(3)

            upstream_thread = threading.Thread(target=upstream)
            upstream_thread.start()
            server = ThreadingHTTPServer(("127.0.0.1", 0), proxy_bridge.Handler)
            server_thread = threading.Thread(target=server.serve_forever)
            server_thread.start()
            client = http.client.HTTPConnection("127.0.0.1", server.server_port, timeout=1)
            try:
                client.request("POST", "/v1/chat/completions", body=b"{}", headers={"Content-Type": "application/json"})
                response = client.getresponse()
                self.assertEqual(response.status, 200)
                self.assertEqual(response.read(len(first_chunk)), first_chunk)
                self.assertFalse(release.is_set(), "upstream closed before the first chunk was forwarded")
            finally:
                release.set()
                client.close()
                server.shutdown()
                server.server_close()
                server_thread.join()
                upstream_thread.join()
                listener.close()


if __name__ == "__main__":
    unittest.main()

#!/usr/bin/env python3
"""Research only: isolated app-server -> official Linux TUI -> app-server.

A loopback fake Responses SSE provider supplies deterministic text. No account,
model token, external network service, tool execution or approvals are used.
Temporary evidence/store are retained for inspection; not a Cylo runtime.
"""
import fcntl
import http.server
import json
import os
import pathlib
import pty
import select
import struct
import subprocess
import tempfile
import termios
import threading
import time

root = pathlib.Path(tempfile.mkdtemp(prefix="cylo-handoff-poc-"))
home, repo = root / "codex-home", root / "repo"
home.mkdir()
repo.mkdir()
subprocess.run(["git", "init", "-q", str(repo)], check=True)
requests = []


class FakeProvider(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        requests.append(body)
        n = len(requests)
        serialized = json.dumps(body)
        marker = (3 if "CYLO_THIRD_USER_MARKER" in serialized else
                  2 if "CLI_SECOND_USER_MARKER" in serialized else 1)
        reply = "FAKE_PROVIDER_REPLY_%d" % marker
        item = {"id": "msg_%d" % n, "type": "message", "role": "assistant",
                "status": "completed", "content": [{"type": "output_text", "text": reply}]}
        events = [
            {"type": "response.created", "response": {"id": "resp_%d" % n}},
            {"type": "response.output_item.added", "output_index": 0,
             "item": {**item, "status": "in_progress", "content": []}},
            {"type": "response.output_text.delta", "item_id": item["id"],
             "output_index": 0, "content_index": 0, "delta": reply},
            {"type": "response.output_item.done", "output_index": 0, "item": item},
            {"type": "response.completed", "response": {"id": "resp_%d" % n,
             "status": "completed", "output": [item],
             "usage": {"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}},
        ]
        data = "".join("event: %s\ndata: %s\n\n" % (e["type"], json.dumps(e))
                       for e in events).encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), FakeProvider)
threading.Thread(target=server.serve_forever, daemon=True).start()
(home / "config.toml").write_text(
    'model="fake-handoff"\nmodel_provider="cylo_fake"\n'
    '[model_providers.cylo_fake]\nname="Cylo fake loopback"\n'
    'base_url="http://127.0.0.1:%d/v1"\n' % server.server_port +
    'wire_api="responses"\nrequires_openai_auth=false\n'
    'request_max_retries=0\nstream_max_retries=0\n'
    '[projects."%s"]\ntrust_level="trusted"\n' % repo)
env = {k: v for k, v in os.environ.items()
       if not k.startswith(("OPENAI_", "CODEX_", "AZURE_OPENAI_"))}
env.update(CODEX_HOME=str(home), TERM="xterm-256color")
events = []


def start():
    p = subprocess.Popen(["codex", "app-server"], cwd=repo, env=env,
                         stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                         stderr=open(root / "app-server.stderr", "a"))
    return p


buffers = {}


def read(p, timeout=20):
    end = time.monotonic() + timeout
    buf = buffers.get(p.pid, b"")
    while time.monotonic() < end:
        if b"\n" in buf:
            line, buf = buf.split(b"\n", 1)
            buffers[p.pid] = buf
            value = json.loads(line)
            events.append(value)
            return value
        if select.select([p.stdout], [], [], .1)[0]:
            part = os.read(p.stdout.fileno(), 65536)
            if not part:
                raise RuntimeError("app-server exited")
            buf += part
    raise TimeoutError("app-server event")


counter = 0


def rpc(p, method, params):
    global counter
    counter += 1
    request_id = counter
    p.stdin.write((json.dumps({"id": request_id, "method": method, "params": params}) + "\n").encode())
    p.stdin.flush()
    while True:
        value = read(p)
        if value.get("id") == request_id:
            if "error" in value:
                raise RuntimeError(value)
            return value["result"]


def initialize(p):
    rpc(p, "initialize", {"clientInfo": {"name": "cylo_handoff_poc", "version": "0.0.0"}})
    p.stdin.write(b'{"method":"initialized"}\n')
    p.stdin.flush()


def turn(p, tid, prompt):
    rpc(p, "turn/start", {"threadId": tid, "input": [{"type": "text", "text": prompt}]})
    while True:
        value = read(p)
        if value.get("method") == "turn/completed":
            assert value["params"]["turn"]["status"] == "completed", value
            return


def stop(p):
    p.terminate()
    p.wait(timeout=15)


p = cli = None
try:
    p = start()
    initialize(p)
    tid = rpc(p, "thread/start", {"cwd": str(repo), "approvalPolicy": "never",
              "sandbox": "read-only", "ephemeral": False})["thread"]["id"]
    turn(p, tid, "CYLO_FIRST_USER_MARKER")
    stop(p)

    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
    cli = subprocess.Popen(["codex", "resume", tid, "CLI_SECOND_USER_MARKER",
                           "--no-daemon", "--no-alt-screen", "-C", str(repo)],
                          stdin=slave, stdout=slave, stderr=slave, env=env)
    os.close(slave)
    output = b""
    deadline = time.monotonic() + 25
    while time.monotonic() < deadline:
        if select.select([master], [], [], .2)[0]:
            part = os.read(master, 65536)
            output += part
            if b"\x1b[6n" in part:
                os.write(master, b"\x1b[1;1R")
            if b"FAKE_PROVIDER_REPLY_2" in output:
                break
    time.sleep(1)  # allow completed response to flush to the native store
    cli.terminate()
    cli.wait(timeout=15)
    os.close(master)
    (root / "cli-terminal.log").write_bytes(output)
    assert b"CYLO_FIRST_USER_MARKER" in output, "original history not visible"

    p = start()
    initialize(p)
    rpc(p, "thread/resume", {"threadId": tid})
    history = rpc(p, "thread/turns/list", {"threadId": tid, "itemsView": "full"})
    before = json.dumps(history)
    assert "CLI_SECOND_USER_MARKER" in before, history
    assert "FAKE_PROVIDER_REPLY_2" in before, history
    turn(p, tid, "CYLO_THIRD_USER_MARKER")
    history = rpc(p, "thread/turns/list", {"threadId": tid, "itemsView": "full"})
    stop(p)
    (root / "history.json").write_text(json.dumps(history, indent=2))
    (root / "events.json").write_text(json.dumps(events, indent=2))
    (root / "requests.json").write_text(json.dumps(requests, indent=2))
    assert len(history["data"]) == 3, history
    assert all(t["status"] == "completed" for t in history["data"]), history
    raw = json.dumps(history)
    for marker in ["CYLO_FIRST_USER_MARKER", "CLI_SECOND_USER_MARKER", "CYLO_THIRD_USER_MARKER",
                   "FAKE_PROVIDER_REPLY_1", "FAKE_PROVIDER_REPLY_2", "FAKE_PROVIDER_REPLY_3"]:
        assert marker in raw, marker
    result = {"passed": True, "thread_id": tid, "completed_turns": 3,
              "loopback_provider_requests": len(requests), "evidence_dir": str(root),
              "scope": "Linux same host/store sequential ownership, synthetic text only"}
    print(json.dumps(result, indent=2))
finally:
    for process in [cli, p]:
        if process is not None and process.poll() is None:
            process.terminate()
            process.wait(timeout=15)
    server.shutdown()

#!/usr/bin/env python3
"""Run one batch of Apple Xcode MCP calls over an existing SSH connection."""

import argparse
import json
import os
import selectors
import subprocess
import sys
import time


class Bridge:
    def __init__(self, host, timeout):
        self.timeout = timeout
        self.sequence = 0
        self.buffer = b""
        self.process = subprocess.Popen(
            ["ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=8",
             "--", host, "xcrun mcpbridge"],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE,
            # Keep SSH authentication and connection errors on stderr.
            stderr=None,
        )
        self.selector = selectors.DefaultSelector()
        self.selector.register(self.process.stdout, selectors.EVENT_READ)

    def send(self, message):
        self.process.stdin.write((json.dumps(message) + "\n").encode())
        self.process.stdin.flush()

    def request(self, method, params):
        self.sequence += 1
        request_id = self.sequence
        self.send({"jsonrpc": "2.0", "id": request_id,
                   "method": method, "params": params})
        deadline = time.monotonic() + self.timeout
        while True:
            if b"\n" in self.buffer:
                line, self.buffer = self.buffer.split(b"\n", 1)
                if not line.strip():
                    continue
                message = json.loads(line)
                if message.get("id") == request_id:
                    if "error" in message:
                        raise RuntimeError(json.dumps(message["error"]))
                    return message["result"]
                # This client offers no sampling, roots, or elicitation support.
                if "method" in message and "id" in message:
                    self.send({"jsonrpc": "2.0", "id": message["id"],
                               "error": {"code": -32601, "message": "Unsupported method"}})
                continue
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not self.selector.select(remaining):
                raise TimeoutError(f"Timed out waiting for {method}; outcome may be incomplete")
            chunk = os.read(self.process.stdout.fileno(), 65536)
            if not chunk:
                raise RuntimeError("SSH/MCP closed before returning a response")
            self.buffer += chunk

    def close(self):
        self.selector.close()
        self.process.stdin.close()
        if self.process.poll() is None:
            self.process.terminate()
        try:
            self.process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait()
        self.process.stdout.close()


def content_object(result):
    if isinstance(result.get("structuredContent"), dict):
        return result["structuredContent"]
    for block in result.get("content", []):
        if block.get("type") == "text":
            try:
                value = json.loads(block["text"])
            except (ValueError, KeyError):
                continue
            if isinstance(value, dict):
                return value
    return {}


def resolve(value, workspace):
    if value == "$workspace":
        if not workspace:
            raise ValueError("$workspace requires an earlier successful XcodeOpenWorkspace")
        return workspace
    if isinstance(value, dict):
        return {k: resolve(v, workspace) for k, v in value.items()}
    if isinstance(value, list):
        return [resolve(v, workspace) for v in value]
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", default=os.environ.get("XCODE_SSH_HOST"))
    parser.add_argument("--timeout", type=float, default=90,
                        help="Deadline per MCP response in seconds (default: 90)")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--list-tools", action="store_true")
    mode.add_argument("--schema", metavar="TOOL")
    args = parser.parse_args()
    if not args.host or args.host.startswith("-") or args.timeout <= 0:
        parser.error("Set XCODE_SSH_HOST or --host and use a positive timeout")
    calls = []
    if not (args.list_tools or args.schema):
        calls = json.load(sys.stdin)
        if not isinstance(calls, list) or not calls:
            parser.error("stdin must contain a nonempty JSON array of [tool, arguments] pairs")
        for call in calls:
            if (not isinstance(call, list) or len(call) != 2
                    or not isinstance(call[0], str) or not isinstance(call[1], dict)):
                parser.error("Each call must be [tool_name, arguments_object]")
    bridge = Bridge(args.host, args.timeout)
    try:
        bridge.request("initialize", {
            "protocolVersion": "2024-11-05", "capabilities": {},
            "clientInfo": {"name": "Codex", "title": "Codex", "version": "1.0"},
        })
        bridge.send({"jsonrpc": "2.0", "method": "notifications/initialized"})
        if args.list_tools or args.schema:
            all_tools = []
            params = {}
            while True:
                page = bridge.request("tools/list", params)
                all_tools.extend(page.get("tools", []))
                if not page.get("nextCursor"):
                    break
                params = {"cursor": page["nextCursor"]}
            if args.schema:
                matches = [t for t in all_tools if t["name"] == args.schema]
                if not matches:
                    raise ValueError(f"Unknown tool: {args.schema}")
                print(json.dumps(matches[0], ensure_ascii=False))
            else:
                print(json.dumps([t["name"] for t in all_tools]))
            return 0
        workspace = None
        for name, arguments in calls:
            result = bridge.request("tools/call", {
                "name": name, "arguments": resolve(arguments, workspace),
            })
            print(json.dumps({"tool": name, "result": result}, ensure_ascii=False), flush=True)
            if result.get("isError"):
                return 1
            if name == "XcodeOpenWorkspace":
                workspace = content_object(result).get("workspaceIdentifier")
        return 0
    finally:
        bridge.close()


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError, TimeoutError) as exc:
        print(f"Xcode MCP: {exc}", file=sys.stderr)
        sys.exit(1)

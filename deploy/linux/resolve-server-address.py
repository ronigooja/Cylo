#!/usr/bin/env python3
"""Resolve a Zoe service hostname to a local IPv4 publish address."""

import ipaddress
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import sys


def main():
    hostname, env_file = sys.argv[1:]
    local = {
        item["local"]
        for interface in json.loads(subprocess.check_output(["ip", "-j", "-4", "address"]))
        for item in interface.get("addr_info", [])
        if item.get("family") == "inet"
    }
    resolved = {
        result[4][0]
        for result in socket.getaddrinfo(hostname, None, socket.AF_INET, socket.SOCK_STREAM)
    }
    candidates = sorted(
        address for address in resolved & local
        if not ipaddress.ip_address(address).is_loopback
    )
    if len(candidates) != 1:
        raise SystemExit("Zoe hostname must resolve to exactly one local non-loopback IPv4 address")
    address = candidates[0]
    path = Path(env_file)
    content = path.read_text()
    content, count = re.subn(
        r"^ZOE_SERVER_LAN_ADDRESS=.*$",
        "ZOE_SERVER_LAN_ADDRESS=" + address,
        content,
        flags=re.MULTILINE,
    )
    if count != 1:
        raise SystemExit("Deployment must define ZOE_SERVER_LAN_ADDRESS exactly once")
    with path.open("w") as output:
        output.write(content)
        output.flush()
        os.fsync(output.fileno())
    print(f"Zoe publish address: {hostname} -> {address}")


if __name__ == "__main__":
    main()

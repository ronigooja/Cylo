#!/usr/bin/env python3
"""Create the restricted downstream key once and save it root-only."""

import http.cookiejar
import json
import os
import time
import urllib.request
from pathlib import Path


BASE_URL = "http://127.0.0.1:18090"
ENV_FILE = Path("/etc/cylo/voice.env")
KEY_FILE = Path("/etc/cylo/voice-api-key.env")


def read_env(path: Path) -> dict[str, str]:
    values = {}
    for line in path.read_text().splitlines():
        if line and not line.startswith("#") and "=" in line:
            key, value = line.split("=", 1)
            values[key] = value
    return values


def wait_for_gateway() -> None:
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(BASE_URL + "/login", timeout=3):
                return
        except Exception:
            time.sleep(1)
    raise SystemExit("voice gateway did not become reachable on loopback")


def main() -> None:
    if os.geteuid() != 0:
        raise SystemExit("Run API key provisioning as root.")
    if KEY_FILE.exists():
        print("Downstream API key already exists at /etc/cylo/voice-api-key.env.")
        return
    env = read_env(ENV_FILE)
    wait_for_gateway()
    cookies = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookies))
    login = urllib.request.Request(
        BASE_URL + "/api/auth/login",
        data=json.dumps(
            {
                "username": env["VOICE_AUTH_USERNAME"],
                "password": env["VOICE_AUTH_PASSWORD"],
            }
        ).encode(),
        headers={"Content-Type": "application/json"},
    )
    with opener.open(login, timeout=10) as response:
        if response.status != 200:
            raise SystemExit(f"gateway admin login failed: HTTP {response.status}")
    csrf = next((cookie.value for cookie in cookies if cookie.name == "voice_gateway_csrf"), None)
    if not csrf:
        raise SystemExit("gateway admin login did not issue a CSRF cookie")
    create = urllib.request.Request(
        BASE_URL + "/api/keys",
        data=json.dumps({"name": "cylo-control-plane"}).encode(),
        headers={"Content-Type": "application/json", "X-CSRF-Token": csrf},
        method="POST",
    )
    with opener.open(create, timeout=10) as response:
        if response.status != 201:
            raise SystemExit(f"gateway API key creation failed: HTTP {response.status}")
        created = json.load(response)
    secret = created.get("secret")
    if not isinstance(secret, str) or not secret.startswith("vgw_live_"):
        raise SystemExit("gateway API key response was unexpected")
    fd = os.open(KEY_FILE, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as output:
        output.write("VOICE_GATEWAY_API_KEY=" + secret + "\n")
        output.flush()
        os.fsync(output.fileno())
    logout = urllib.request.Request(
        BASE_URL + "/api/auth/logout",
        data=b"{}",
        headers={"Content-Type": "application/json", "X-CSRF-Token": csrf},
        method="POST",
    )
    try:
        opener.open(logout, timeout=10).close()
    except Exception:
        pass
    print("Downstream API key created at /etc/cylo/voice-api-key.env (root-readable only).")


if __name__ == "__main__":
    main()

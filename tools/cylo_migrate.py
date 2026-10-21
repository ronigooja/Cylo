#!/usr/bin/env python3
"""Create source-isolation evidence after an operator has stopped and masked Cylo units.

This helper is deliberately local-host scoped. Transfer the encrypted backup and
proof using the deployment's approved channel; this command does not administer
SSH peers or change systemd state.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import hmac
import json
import os
from pathlib import Path
import socket
import stat
import subprocess
import sys
from datetime import datetime, timezone

from data_lifecycle.common import fail


def private_key(path: Path) -> bytes:
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o600:
        fail("proof key must be a current-user-owned regular file with mode 0600")
    key = path.read_bytes()
    if len(key) != 32:
        fail("proof key must contain exactly 32 bytes")
    return key


def maintenance_status(binary: Path, database: Path) -> dict:
    result = subprocess.run([str(binary), "status", "--database", str(database)], check=True, capture_output=True, text=True)
    data = json.loads(result.stdout)
    if data.get("dispatch_enabled") or not data.get("migration_ready"):
        fail("source instance must be fenced and migration readiness must be clear")
    readiness = data
    for field in ("unresolved_runs", "unknown_effects", "active_deployment_jobs", "live_deployments", "open_voice_sessions"):
        if readiness.get(field) != 0:
            fail(f"source migration readiness is not clear: {field}")
    return data


def unit_state(name: str) -> dict[str, str]:
    active = subprocess.run(["systemctl", "show", name, "--property=ActiveState", "--value"], check=True, capture_output=True, text=True).stdout.strip()
    file_state = subprocess.run(["systemctl", "show", name, "--property=UnitFileState", "--value"], check=True, capture_output=True, text=True).stdout.strip()
    if active != "inactive" or file_state != "masked":
        fail(f"systemd unit {name} must be inactive and masked (state={active}/{file_state})")
    return {"name": name, "active_state": active, "unit_file_state": file_state}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--maintenance", type=Path, required=True)
    parser.add_argument("--database", type=Path, required=True)
    parser.add_argument("--proof-key-file", type=Path, required=True)
    parser.add_argument("--proof-output", type=Path, required=True)
    parser.add_argument("--unit", action="append", required=True, help="source systemd unit; every unit must be inactive and masked")
    args = parser.parse_args()

    state = maintenance_status(args.maintenance.absolute(), args.database.absolute())
    units = [unit_state(name) for name in sorted(set(args.unit))]
    payload = {
        "schema_version": 1,
        "old_instance_id": state["instance_id"],
        "old_epoch": state["epoch"],
        "source_host": socket.getfqdn(),
        "units": units,
        "active_runs": state["unresolved_runs"],
        "unknown_effects": state["unknown_effects"],
        "active_deployment_jobs": state["active_deployment_jobs"],
        "live_deployments": state["live_deployments"],
        "open_voice_sessions": state["open_voice_sessions"],
        "verified_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
    }
    raw = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
    envelope = {"payload": base64.urlsafe_b64encode(raw).decode().rstrip("="), "hmac": hmac.new(private_key(args.proof_key_file), raw, hashlib.sha256).hexdigest()}
    output = args.proof_output.absolute()
    if output.exists() or output.is_symlink():
        fail("proof output already exists; refusing to overwrite")
    output.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    fd = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        json.dump(envelope, stream, sort_keys=True)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())
    print(f"isolation proof created: {output}")
    print(f"source instance: {state['instance_id']} epoch {state['epoch']}; verified units: {len(units)}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"cylo-migrate: {exc}", file=sys.stderr)
        raise SystemExit(1)

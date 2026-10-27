#!/usr/bin/env python3
"""Plan or execute complete destruction of a manifest-owned Linux Zoe instance.

Execution requires a deployment-owned cleanup adapter through AO's authorized
API. No generic Docker, AO, Qdrant, or external proxy cleanup is inferred.
"""
from __future__ import annotations
import argparse
import fcntl
import hashlib
import json
import os
import re
from pathlib import Path
import secrets
import shutil
import sqlite3
import stat
import subprocess

ALLOWED_UNITS = {"zoe-compose.service", "zoe-runner.service", "zoe-backup.service", "zoe-backup.timer"}
REQUIRED_CATEGORIES = {"data", "artifacts", "backups", "config"}
RESOURCE_KINDS = {"ao_runtime", "capability_container", "managed_volume", "qdrant_index", "mem0_store"}

def canonical(path: str) -> Path:
    value = Path(path)
    if not value.is_absolute() or value != value.resolve() or ".." in value.parts:
        raise ValueError("paths must be absolute and free of symlinks or parent traversal")
    return value

def mounted_paths() -> set[Path]:
    # ismount alone misses bind mounts on the same filesystem. Linux mountinfo
    # is authoritative for the only supported deployment topology.
    result = set()
    for line in Path("/proc/self/mountinfo").read_text().splitlines():
        fields = line.split()
        if len(fields) < 6:
            raise ValueError("cannot inspect Linux mount boundaries")
        decoded = re.sub(r"\\([0-7]{3})", lambda match: chr(int(match.group(1), 8)), fields[4])
        result.add(Path(decoded))
    if not result:
        raise ValueError("cannot inspect Linux mount boundaries")
    return result


def protected_file(path: Path, executable: bool = False) -> None:
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & (0o022 if executable else 0o077):
        raise ValueError("operator-owned protected regular file required")
    if executable and not os.access(path, os.X_OK):
        raise ValueError("operator adapter is not executable")

def read_state(database: Path) -> tuple[str, int]:
    with sqlite3.connect(database.as_uri() + "?mode=ro", uri=True) as connection:
        row = connection.execute("SELECT instance_id,dispatch_enabled FROM zoe_instance_state WHERE singleton=1").fetchone()
        if row is None:
            raise ValueError("instance authority is missing")
        return str(row[0]), int(row[1])

def validate_manifest(manifest: dict, receipt: Path) -> dict:
    expected = {"schema_version", "topology", "instance_id", "deployment_root", "database", "roots", "service_units", "external_resources", "cleanup_adapter", "maintenance_binary"}
    if set(manifest) != expected or manifest["schema_version"] != 1 or manifest["topology"] != "linux-systemd-compose":
        raise ValueError("unsupported or incomplete destruction manifest")
    root = canonical(manifest["deployment_root"])
    # Require a dedicated instance directory, never a shared host root.
    if len(root.parts) < 4 or not root.is_dir() or root.is_mount() or root.stat().st_uid != os.geteuid() or root.stat().st_mode & 0o022:
        raise ValueError("deployment root must be an operator-owned dedicated instance directory")
    if any(mount == root or root in mount.parents for mount in mounted_paths()):
        raise ValueError("dedicated deployment root contains mounted resources; detach them before destruction")
    roots = manifest["roots"]
    if not isinstance(roots, list) or {item.get("category") for item in roots} != REQUIRED_CATEGORIES:
        raise ValueError("enumerate data, artifacts, backups, and config roots explicitly")
    paths = []
    for item in roots:
        if set(item) != {"category", "path"}:
            raise ValueError("invalid owned root entry")
        path = canonical(item["path"])
        if path.parent != root or not path.is_dir() or path.is_mount() or path.stat().st_uid != os.geteuid() or path.stat().st_mode & 0o022:
            raise ValueError("owned deletion root is outside the dedicated instance boundary")
        if any(path == prior or path in prior.parents or prior in path.parents for prior in paths):
            raise ValueError("owned deletion roots overlap")
        for current, directories, files in os.walk(path, followlinks=False):
            for name in directories + files:
                child = Path(current) / name
                if child.is_symlink() or child.is_mount() or child.lstat().st_uid != os.geteuid() or (child.is_dir() and child.lstat().st_mode & 0o022):
                    raise ValueError("deletion tree contains symlink, mount, or foreign-owned input")
        paths.append(path)
    if set(root.iterdir()) != set(paths):
        raise ValueError("dedicated deployment root contains unaccounted paths")
    database = canonical(manifest["database"])
    if not any(item["category"] == "data" and Path(item["path"]) in database.parents for item in roots):
        raise ValueError("database must be inside an owned data root")
    protected_file(database)
    instance, _ = read_state(database)
    if not manifest["instance_id"] or instance != manifest["instance_id"]:
        raise ValueError("manifest instance does not match authoritative database")
    units = manifest["service_units"]
    if not isinstance(units, list) or len(set(units)) != len(units) or set(units) != ALLOWED_UNITS:
        raise ValueError("manifest must enumerate supported Zoe systemd units")
    resources = manifest["external_resources"]
    if not isinstance(resources, list):
        raise ValueError("external resource inventory is required")
    identities = set()
    for item in resources:
        if set(item) != {"kind", "id"} or item["kind"] not in RESOURCE_KINDS or not isinstance(item["id"], str) or not item["id"]:
            raise ValueError("invalid external resource inventory")
        identity = (item["kind"], item["id"])
        if identity in identities:
            raise ValueError("duplicate external resource")
        identities.add(identity)
    for name in ("cleanup_adapter", "maintenance_binary"):
        path = canonical(manifest[name])
        protected_file(path, executable=True)
        if any(path == owned or owned in path.parents for owned in paths):
            raise ValueError("operator adapters must survive outside deletion roots")
    receipt = canonical(str(receipt))
    if receipt.exists() or any(receipt == owned or owned in receipt.parents for owned in paths):
        raise ValueError("receipt must be a new protected path outside deletion roots")
    return {"database": database, "paths": paths, "resources": identities, "units": units}

def run_process(command: list[str], *, input: str | None = None) -> str:
    result = subprocess.run(command, input=input, text=True, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=180, check=False)
    if result.returncode != 0:
        raise ValueError("required lifecycle adapter failed; owned files retained")
    if len(result.stdout) > 1 << 20:
        raise ValueError("adapter proof exceeds size limit")
    return result.stdout

def destroy(manifest: dict, receipt: Path, execute: bool, confirm_instance: str, runner=run_process) -> dict:
    checked = validate_manifest(manifest, receipt)
    plan = {"instance_id": manifest["instance_id"], "topology": manifest["topology"], "owned_roots": [str(path) for path in checked["paths"]], "external_resource_count": len(checked["resources"]), "service_units": checked["units"], "executed": False}
    if not execute:
        return plan
    if confirm_instance != manifest["instance_id"]:
        raise ValueError("--confirm-instance must exactly match the authoritative instance")
    # Reserve durable evidence before any service or resource mutation.
    fd = os.open(receipt, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(fd, "w") as evidence:
        evidence.write(json.dumps({"status": "started", "instance_id": confirm_instance}) + "\n")
        evidence.flush(); os.fsync(evidence.fileno())
        runner(["systemctl", "mask", "--now", *checked["units"]])
        for unit in checked["units"]:
            if runner(["systemctl", "show", "--property=ActiveState", "--value", unit]).strip() != "inactive":
                raise ValueError("service termination is not confirmed; owned files retained")
        runner([manifest["maintenance_binary"], "fence", "--database", str(checked["database"]), "--reason", "operator_instance_destruction"])
        with open(checked["database"], "rb") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            if read_state(checked["database"]) != (confirm_instance, 0):
                raise ValueError("dispatch fencing is not confirmed; owned files retained")
            challenge = secrets.token_hex(32)
            proof = json.loads(runner([manifest["cleanup_adapter"], "verify-and-cleanup"], input=json.dumps({"challenge": challenge, "manifest": manifest})))
            if set(proof) != {"instance_id", "challenge", "inventory_complete", "unaccounted_resources", "active_runtimes", "removed_resources"} or proof["instance_id"] != confirm_instance or proof["challenge"] != challenge or proof["inventory_complete"] is not True or proof["unaccounted_resources"] != [] or proof["active_runtimes"] != []:
                raise ValueError("AO/runtime/service cleanup proof is incomplete; owned files retained")
            removed = {(item["kind"], item["id"]) for item in proof["removed_resources"] if set(item) == {"kind", "id"}}
            if removed != checked["resources"] or len(proof["removed_resources"]) != len(removed):
                raise ValueError("cleanup proof does not cover the exact resource inventory; owned files retained")
            # Repeat all path/identity checks after adapters and before deletion.
            validate_manifest({**manifest}, receipt.with_name(receipt.name + ".recheck"))
            evidence.write(json.dumps({"status": "cleanup_verified", "proof_sha256": hashlib.sha256(json.dumps(proof, sort_keys=True).encode()).hexdigest()}) + "\n")
            evidence.flush(); os.fsync(evidence.fileno())
            for path in checked["paths"]:
                shutil.rmtree(path)
            Path(manifest["deployment_root"]).rmdir()
        evidence.write(json.dumps({"status": "destroyed", "instance_id": confirm_instance}) + "\n")
        evidence.flush(); os.fsync(evidence.fileno())
    return {**plan, "executed": True, "receipt": str(receipt)}

def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True, help="operator-owned mode-0600 manifest")
    parser.add_argument("--receipt", type=Path, required=True, help="new private receipt outside deletion roots")
    parser.add_argument("--execute", action="store_true", help="stop/fence, verify complete external cleanup, then delete owned roots")
    parser.add_argument("--confirm-instance", default="")
    args = parser.parse_args()
    protected_file(args.manifest)
    if args.manifest.stat().st_size > 1 << 20:
        raise ValueError("manifest exceeds size limit")
    manifest = json.loads(args.manifest.read_text())
    print(json.dumps(destroy(manifest, args.receipt, args.execute, args.confirm_instance), sort_keys=True))
    return 0

if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception:
        raise SystemExit("destruction did not complete; inspect protected receipt and verify service/resource state before retrying")

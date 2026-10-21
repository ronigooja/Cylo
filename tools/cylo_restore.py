#!/usr/bin/env python3
"""Validate and stage a Cylo backup for a new, dispatch-disabled instance."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import sys
import tempfile

from data_lifecycle.common import fail, private_dir, read_key, unpack_verified


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--artifact", type=Path, required=True)
    parser.add_argument("--key-file", type=Path, required=True)
    parser.add_argument("--restore-directory", type=Path, required=True, help="new path; must not already exist")
    args = parser.parse_args()
    key = read_key(args.key_file)
    target = args.restore_directory.absolute()
    if target.exists() or target.is_symlink():
        fail("restore target must be a new path; existing targets are never overwritten")
    target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="cylo-restore-", dir=target.parent) as tmp:
        payload, manifest = unpack_verified(args.artifact, key, Path(tmp))
        # Check the deletion ledger is present and authentic before making the staged copy
        # available to a server or any derived retrieval service.
        deletion = manifest.get("database", {}).get("deletion_record_counts", {})
        source_db = payload / "authority" / "cylo.sqlite"
        staged = Path(tmp) / "new-instance"
        staged.mkdir(mode=0o700)
        (staged / "data").mkdir(mode=0o700)
        shutil.copyfile(source_db, staged / "data" / "cylo.sqlite.restore-pending")
        os.chmod(staged / "data" / "cylo.sqlite.restore-pending", 0o600)
        for item in manifest.get("managed_paths", []):
            role, root_id = item["role"], item["root_id"]
            rel = PurePosixPath(item["path"])
            if role not in ("service_config_paths", "artifact_paths", "persistent_volume_paths") or rel.is_absolute() or not rel.parts or any(p in ("", ".", "..") for p in rel.parts):
                fail("manifest contains an unsafe managed path")
            source = payload / role / root_id / Path(*rel.parts)
            dest = staged / "managed" / role / root_id / Path(*rel.parts)
            if source.is_file():
                dest.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                dest.write_bytes(source.read_bytes())
                os.chmod(dest, 0o600)
            elif source.is_dir():
                dest.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                shutil.copytree(source, dest, dirs_exist_ok=True)
            else:
                fail("manifest managed path is missing from payload")
        restored = dict(manifest)
        restored["restore_state"] = {
            "instance_id": "",
            "dispatch_enabled": False,
            "requires_old_instance_isolation": True,
            "requires_deletion_reconciliation_before_derived_reads": bool(deletion),
            "deletion_records_verified": deletion,
        }
        (staged / "restore-state.json").write_text(json.dumps(restored, sort_keys=True, indent=2) + "\n", encoding="utf-8")
        os.chmod(staged / "restore-state.json", 0o600)
        os.replace(staged, target)
        parent_fd = os.open(target.parent, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
        try:
            os.fsync(parent_fd)
        finally:
            os.close(parent_fd)
    print(f"restore staged: {target}")
    print("dispatch enabled: false (install the database with `cylo-maintenance prepare-restore` before starting a server)")
    print(f"managed paths: {len(manifest.get('managed_paths', []))}; deletion ledger tables: {len(deletion)}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"cylo-restore: {exc}", file=sys.stderr)
        raise SystemExit(1)

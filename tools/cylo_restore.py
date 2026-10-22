#!/usr/bin/env python3
"""Validate and stage a Cylo backup for a new, dispatch-disabled instance."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import sqlite3
import sys
import tempfile

from data_lifecycle.common import fail, private_dir, read_key, unpack_verified


MANAGED_ROLES = {"service_config_paths", "artifact_paths", "persistent_volume_paths"}


def restore_managed_paths(payload: Path, staged: Path, manifest: dict) -> int:
    roots = manifest.get("roots")
    entries = manifest.get("managed_paths")
    if not isinstance(roots, dict) or not isinstance(entries, list):
        fail("manifest managed path inventory is malformed")
    root_ids = set(roots)
    if not all(isinstance(root_id, str) and root_id.isidentifier() for root_id in root_ids):
        fail("manifest contains an unsafe managed root ID")
    seen = set()
    payload_root = payload.resolve(strict=True)
    staged_root = staged.resolve(strict=True)
    for item in entries:
        if not isinstance(item, dict) or set(item) != {"role", "root_id", "path"}:
            fail("manifest contains a malformed managed path")
        role, root_id = item["role"], item["root_id"]
        if role not in MANAGED_ROLES or not isinstance(root_id, str) or not root_id.isidentifier() or root_id not in root_ids:
            fail("manifest contains an unsafe managed role or root ID")
        rel = PurePosixPath(item["path"] if isinstance(item["path"], str) else "")
        if rel.is_absolute() or not rel.parts or any(p in ("", ".", "..") for p in rel.parts):
            fail("manifest contains an unsafe managed path")
        identity = (role, root_id, rel.as_posix())
        if identity in seen:
            fail("manifest contains a duplicate managed path")
        seen.add(identity)
        source = payload_root / role / root_id / Path(*rel.parts)
        try:
            source = source.resolve(strict=True)
            source.relative_to(payload_root)
        except (OSError, ValueError):
            fail("manifest managed source escapes the verified payload")
        dest = staged_root / "managed" / role / root_id / Path(*rel.parts)
        try:
            dest.resolve(strict=False).relative_to(staged_root)
        except ValueError:
            fail("manifest managed destination escapes the staging directory")
        if source.is_file():
            dest.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            try:
                dest.parent.resolve(strict=True).relative_to(staged_root)
            except ValueError:
                fail("managed destination parent escapes the staging directory")
            dest.write_bytes(source.read_bytes())
            os.chmod(dest, 0o600)
        elif source.is_dir():
            dest.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            try:
                dest.parent.resolve(strict=True).relative_to(staged_root)
            except ValueError:
                fail("managed destination parent escapes the staging directory")
            shutil.copytree(source, dest, dirs_exist_ok=True)
        else:
            fail("manifest managed path is missing from payload")
    return len(entries)


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
        database_manifest = manifest.get("database", {})
        deletion = manifest.get("database", {}).get("deletion_ledger")
        trusted_head = database_manifest.get("trusted_deletion_head")
        source_db = payload / "authority" / "cylo.sqlite"
        source_journal = payload / "authority" / "cylo.sqlite.deletion-ledger"
        staged = Path(tmp) / "new-instance"
        staged.mkdir(mode=0o700)
        (staged / "data").mkdir(mode=0o700)
        shutil.copyfile(source_db, staged / "data" / "cylo.sqlite.restore-pending")
        os.chmod(staged / "data" / "cylo.sqlite.restore-pending", 0o600)
        shutil.copyfile(source_journal, staged / "data" / "cylo.sqlite.restore-pending.deletion-ledger")
        os.chmod(staged / "data" / "cylo.sqlite.restore-pending.deletion-ledger", 0o600)
        # Keep the restore-only closed state inside the database so renaming
        # the staged file cannot bypass the adjacent restore-state.json.
        with sqlite3.connect(staged / "data" / "cylo.sqlite.restore-pending") as database:
            database.execute(
                "CREATE TABLE IF NOT EXISTS cylo_restore_stage_guard ("
                "singleton INTEGER PRIMARY KEY CHECK(singleton=1), "
                "pending INTEGER NOT NULL CHECK(pending IN (0,1)))"
            )
            database.execute(
                "INSERT INTO cylo_restore_stage_guard(singleton,pending) VALUES(1,1) "
                "ON CONFLICT(singleton) DO UPDATE SET pending=1"
            )
        managed_count = restore_managed_paths(payload, staged, manifest)
        restored = dict(manifest)
        restored["restore_state"] = {
            "instance_id": "",
            "dispatch_enabled": False,
            "requires_old_instance_isolation": True,
            "requires_deletion_reconciliation_before_derived_reads": bool(deletion),
            "deletion_records_verified": deletion,
            "trusted_deletion_head": {
                "snapshot_lineage_id": trusted_head["snapshot_lineage_id"],
                "snapshot_generation": trusted_head["snapshot_generation"],
                "snapshot_sequence": trusted_head["snapshot_sequence"],
                "snapshot_event_count": trusted_head["snapshot_event_count"],
                "snapshot_event_cursor_sum": trusted_head["snapshot_event_cursor_sum"],
                "journal_lineage_id": trusted_head["lineage_id"],
                "journal_generation": trusted_head["generation"],
                "journal_sequence": trusted_head["sequence"],
                "journal_head_sha256": trusted_head["head_hash"],
            } if isinstance(trusted_head, dict) else None,
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
    print(f"managed paths: {managed_count}; deletion ledger tables: {len(deletion or {})}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"cylo-restore: {exc}", file=sys.stderr)
        raise SystemExit(1)

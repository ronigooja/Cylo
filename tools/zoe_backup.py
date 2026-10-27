#!/usr/bin/env python3
"""Create an encrypted offline SQLite and managed-data backup."""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path, PurePosixPath
import secrets
import sys
import tempfile

from data_lifecycle.common import (
    FORMAT, CHUNK, copy_payload_path, create_archive, database_fence, encrypt_file,
    file_record, load_inventory, private_dir, read_key, run_volume_quiesce_adapter,
    sqlite_snapshot, independent_deletion_journal_metadata,
)
from data_lifecycle.retention import purge_expired, read_backup_delete_manifest, register_backup


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--database", type=Path, required=True)
    parser.add_argument("--destination", type=Path, required=True, help="private backup directory")
    parser.add_argument("--retention-days", type=int, default=30, help="retain registered backups for this many days (default: 30)")
    parser.add_argument("--backup-delete-manifest", type=Path, default=os.environ.get("ZOE_BACKUP_DELETE_MANIFEST_PATH"), help="configured independent durable deletion manifest written by the memory worker")
    parser.add_argument("--key-file", type=Path, required=True, help="32-byte deployment-managed key, mode 0600")
    parser.add_argument("--data-root", action="append", default=[], metavar="ID=PATH", help="allowed source root; repeat for configs/artifacts/volumes")
    parser.add_argument("--inventory", type=Path, help="JSON list of relative managed paths and secret references")
    parser.add_argument("--volume-quiesce-adapter", type=Path, help="absolute executable that confirms persistent volumes are quiesced")
    args = parser.parse_args()
    if args.retention_days < 1:
        parser.error("--retention-days must be a positive whole number of days")

    inventory = load_inventory(args.inventory)
    roots = parse_roots(args.data_root, inventory["roots"])
    volume_entries = inventory["persistent_volume_paths"]
    volume_evidence = None
    if volume_entries:
        if args.volume_quiesce_adapter is None:
            raise ValueError("persistent volumes require --volume-quiesce-adapter; online volume consistency is never assumed")
        if args.inventory is None:
            raise ValueError("persistent volume adapter requires an explicit inventory file")
        volume_evidence = run_volume_quiesce_adapter(args.volume_quiesce_adapter.absolute(), args.inventory, volume_entries)
    key = read_key(args.key_file)
    destination = args.destination.absolute()
    private_dir(destination)
    created_at = datetime.now(timezone.utc)
    timestamp = created_at.strftime("%Y%m%dT%H%M%SZ")
    artifact = destination / f"zoe-{timestamp}-{secrets.token_hex(4)}.zoe-backup"
    with database_fence(args.database) as source_db:
        with tempfile.TemporaryDirectory(prefix="zoe-backup-", dir=destination) as tmp:
            stage = Path(tmp) / "payload"
            stage.mkdir(mode=0o700)
            (stage / "authority").mkdir(mode=0o700)
            db_record = sqlite_snapshot(source_db, stage / "authority" / "zoe.sqlite")
            sidecar = Path(str(source_db) + ".deletion-ledger")
            journal_record = independent_deletion_journal_metadata(sidecar, db_record.get("instance_state"))
            journal_target = stage / "authority" / "zoe.sqlite.deletion-ledger"
            journal_target.write_bytes(sidecar.read_bytes() if sidecar.exists() else b"")
            os.chmod(journal_target, 0o600)
            db_record["trusted_deletion_head"] = journal_record
            backup_delete_manifest = read_backup_delete_manifest(args.backup_delete_manifest, journal_target, journal_record)
            entries = []
            for role in ("service_config_paths", "artifact_paths", "persistent_volume_paths"):
                for entry in inventory[role]:
                    root_id, relative = validate_entry(entry, roots)
                    copy_payload_path(roots[root_id], roots[root_id] / relative, role, root_id, stage)
                    entries.append({"role": role, "root_id": root_id, "path": relative.as_posix()})
            # Rebuild inventory directly from staged paths. This handles file and directory entries uniformly.
            records = []
            for path in sorted(stage.rglob("*")):
                if path.is_file():
                    role = path.relative_to(stage).parts[0]
                    records.append(file_record(path, role, path.relative_to(stage).as_posix()))
            manifest = {
                "format": FORMAT,
                "schema_version": 1,
                "created_at": datetime.now(timezone.utc).isoformat(),
                "source": {"database_path": str(source_db), "host": os.uname().nodename},
                "database": db_record,
                "roots": {key: str(value) for key, value in roots.items()},
                "managed_paths": entries,
                "consistency": {
                    "database": "offline exclusive writer lock plus SQLite backup API and integrity_check",
                    "persistent_volumes": "adapter-confirmed quiesced" if volume_evidence else "none included",
                    "volume_adapter_evidence": volume_evidence,
                },
                "secret_refs": inventory["secret_refs"],
                "files": records,
                "backup_delete_manifest": backup_delete_manifest,
                "encryption": {"algorithm": "AES-256-GCM", "kdf": "scrypt", "scrypt_n": 2**18, "scrypt_r": 8, "scrypt_p": 1},
                "retention_days": args.retention_days,
            }
            (stage / "manifest.json").write_text(json.dumps(manifest, sort_keys=True, indent=2) + "\n", encoding="utf-8")
            os.chmod(stage / "manifest.json", 0o600)
            tar_path = Path(tmp) / "backup.tar"
            create_archive(stage, tar_path)
            temp_artifact = Path(tmp) / "artifact"
            encrypt_file(tar_path, temp_artifact, key)
            os.link(temp_artifact, artifact)
            temp_artifact.unlink()
            register_backup(destination, artifact, created_at, {
                "snapshot_lineage_id": journal_record["snapshot_lineage_id"],
                "snapshot_sequence": journal_record["snapshot_sequence"],
                "cleanup_targets": manifest["backup_delete_manifest"]["targets"],
            }, key)
            dfd = os.open(destination, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
            try:
                os.fsync(dfd)
            finally:
                os.close(dfd)
    print(f"backup created: {artifact}")
    print(f"encrypted bytes: {artifact.stat().st_size}")
    removed = purge_expired(destination, key, args.retention_days)
    if removed:
        print(f"expired backups removed: {removed}")
    return 0


def parse_roots(values: list[str], inventory_roots: dict[str, str]) -> dict[str, Path]:
    roots = {}
    for root_id, path in inventory_roots.items():
        roots[root_id] = resolve_root(path)
    for value in values:
        if "=" not in value:
            raise ValueError("--data-root must use ID=PATH")
        root_id, path = value.split("=", 1)
        if not root_id.isidentifier() or root_id in roots:
            raise ValueError("data-root IDs must be unique identifiers")
        roots[root_id] = resolve_root(path)
    return roots


def resolve_root(path: str) -> Path:
    root_path = Path(path).absolute()
    if root_path.is_symlink():
        raise ValueError("data roots cannot be symlinks")
    resolved = root_path.resolve(strict=True)
    if not resolved.is_dir():
        raise ValueError("data roots must resolve to existing directories")
    return resolved


def validate_entry(entry: dict, roots: dict[str, Path]):
    if not isinstance(entry, dict) or set(entry) != {"root_id", "path"}:
        raise ValueError("each managed path requires root_id and relative path")
    root_id = entry["root_id"]
    rel = PurePosixPath(entry["path"])
    if root_id not in roots or rel.is_absolute() or not rel.parts or any(x in ("", ".", "..") for x in rel.parts):
        raise ValueError("managed path is outside an allowed root")
    source = roots[root_id] / Path(*rel.parts)
    if source.absolute().is_symlink():
        raise ValueError("managed path cannot be a symlink")
    if not source.resolve(strict=True).is_relative_to(roots[root_id]):
        raise ValueError("managed path escapes its allowed root")
    return root_id, rel


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"zoe-backup: {exc}", file=sys.stderr)
        raise SystemExit(1)

"""Authenticated ownership manifest and fail-closed backup retention purge."""

from __future__ import annotations

import contextlib
from datetime import datetime, timedelta, timezone
import fcntl
import hmac
import json
import hashlib
import os
from pathlib import Path
import re
import stat
import tempfile
from typing import Any

MANIFEST_NAME = ".zoe-retention.json"
LOCK_NAME = ".zoe-retention.lock"
FORMAT = "zoe-backup-retention-v1"
ARTIFACT_RE = re.compile(r"zoe-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}\.zoe-backup\Z")
ENTRY_FIELDS = {
    "artifact", "sha256", "size", "created_at", "deletion_baseline",
    "deletion_sequence", "deletion_targets", "deletion_version",
}


def fail(message: str) -> None:
    raise RuntimeError(message)


def private_dir(path: Path) -> None:
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    st = path.lstat()
    if not stat.S_ISDIR(st.st_mode) or st.st_uid != os.getuid():
        fail("backup directory must be owned by the current user")
    path.chmod(0o700)


def register_backup(directory: Path, artifact: Path, created_at: datetime, deletion: dict[str, Any] | None, key: bytes) -> None:
    """Durably register a completed artifact before pruning can consider it."""
    directory = _safe_directory(directory)
    if artifact.parent.resolve(strict=True) != directory or not ARTIFACT_RE.fullmatch(artifact.name):
        fail("backup artifact is outside the configured backup directory or has an unsupported name")
    if artifact.is_symlink() or not artifact.is_file():
        fail("backup artifact must be a regular non-symlink file")
    record = {
        "artifact": artifact.name,
        "sha256": _hash_file(artifact),
        "size": artifact.stat().st_size,
        "created_at": _iso(created_at),
        "deletion_baseline": _deletion_value(deletion, "snapshot_lineage_id", ""),
        "deletion_sequence": _deletion_value(deletion, "snapshot_sequence", 0),
        "deletion_targets": _deletion_targets(deletion),
        "deletion_version": 1,
    }
    with _manifest_lock(directory):
        manifest = _read_manifest(directory, key)
        if any(entry["artifact"] == artifact.name for entry in manifest["entries"]):
            fail("backup artifact is already registered")
        manifest["entries"].append(record)
        _write_manifest(directory, manifest, key)


def purge_expired(directory: Path, key: bytes, retention_days: int = 30, now: datetime | None = None) -> int:
    """Remove only expired artifacts exactly bound by the authenticated manifest."""
    if isinstance(retention_days, bool) or not isinstance(retention_days, int) or retention_days < 1:
        fail("retention period must be a positive whole number of days")
    directory = _safe_directory(directory)
    now = now or datetime.now(timezone.utc)
    if now.tzinfo is None:
        fail("retention clock must include a timezone")
    now = now.astimezone(timezone.utc)
    with _manifest_lock(directory):
        manifest = _read_manifest(directory, key)
        cutoff = now - timedelta(days=retention_days)
        expired: list[dict[str, Any]] = []
        keep: list[dict[str, Any]] = []
        for entry in manifest["entries"]:
            created = _parse_timestamp(entry["created_at"])
            if created > now:
                fail("retention manifest contains a future backup timestamp")
            artifact = directory / entry["artifact"]
            if artifact.is_symlink():
                fail("registered backup artifact is a symlink")
            if artifact.exists():
                if not artifact.is_file() or artifact.stat().st_size != entry["size"] or _hash_file(artifact) != entry["sha256"]:
                    fail("registered backup artifact does not match its durable ownership record")
            elif created >= cutoff:
                fail("registered unexpired backup artifact is missing")
            (expired if created < cutoff else keep).append(entry)

        removed = 0
        for entry in expired:
            artifact = directory / entry["artifact"]
            if artifact.exists():
                artifact.unlink()
                removed += 1
        if expired:
            manifest["entries"] = keep
            _write_manifest(directory, manifest, key)
        return removed


def read_backup_delete_manifest(path: Path | None, journal_path: Path, journal_metadata: dict[str, Any]) -> dict[str, Any]:
    """Require the configured manifest to cover the exact frozen journal head."""
    journal_records = [json.loads(line) for line in journal_path.read_text(encoding="utf-8").splitlines()]
    if not journal_records and journal_metadata.get("snapshot_sequence", 0) != 0:
        fail("trusted deletion snapshot has records but its journal is empty")
    if path is None:
        if journal_records:
            fail("backup deletion manifest is not configured while the deletion journal has records")
        return {
            "baseline": journal_metadata["snapshot_lineage_id"],
            "sequence": 0,
            "targets": [],
            "version": 1,
        }
    manifest_path = Path(path).absolute()
    if manifest_path.is_symlink():
        fail("backup deletion manifest cannot be a symlink")
    if manifest_path.parent.resolve(strict=True) != manifest_path.parent:
        fail("backup deletion manifest directory cannot contain symlinks")
    parent_stat = manifest_path.parent.stat()
    if not stat.S_ISDIR(parent_stat.st_mode) or parent_stat.st_uid != os.getuid() or stat.S_IMODE(parent_stat.st_mode) & 0o077:
        fail("backup deletion manifest directory must be current-user-owned and private")
    if not manifest_path.exists():
        if journal_records:
            fail("backup deletion manifest is missing while the deletion journal has records")
        rows = []
    else:
        st = manifest_path.lstat()
        if not stat.S_ISREG(st.st_mode) or st.st_uid != os.getuid() or stat.S_IMODE(st.st_mode) != 0o600:
            fail("backup deletion manifest must be a current-user-owned regular file with mode 0600")
        try:
            rows = [json.loads(line) for line in manifest_path.read_text(encoding="utf-8").splitlines()]
        except (OSError, UnicodeError, json.JSONDecodeError) as exc:
            fail(f"backup deletion manifest is invalid: {exc}")
    if len(rows) != len(journal_records):
        fail("backup deletion manifest is not complete for the frozen journal head")
    targets: set[str] = set()
    by_sequence: dict[int, dict[str, Any]] = {}
    for row in rows:
        if not isinstance(row, dict) or set(row) != {"version", "sequence", "baseline", "targets", "source_hash"}:
            fail("backup deletion manifest has an unsupported record schema")
        sequence = row["sequence"]
        if isinstance(sequence, bool) or not isinstance(sequence, int) or sequence < 1 or sequence in by_sequence:
            fail("backup deletion manifest sequence is invalid or duplicated")
        by_sequence[sequence] = row
    if set(by_sequence) != set(range(1, len(journal_records) + 1)):
        fail("backup deletion manifest is not complete for the frozen journal head")
    for index, journal_record in enumerate(journal_records, 1):
        row = by_sequence[index]
        if (
            row["version"] != 1
            or row["baseline"] != journal_record["lineage_id"]
            or row["targets"] != journal_record["cleanup_targets"]
            or row["source_hash"] != journal_record["hash"]
            or not isinstance(row["targets"], list)
            or not isinstance(row["source_hash"], str)
            or not re.fullmatch(r"[0-9a-f]{64}", row["source_hash"])
        ):
            fail("backup deletion manifest does not match the trusted deletion journal")
        targets.update(journal_record["cleanup_targets"])
    return {
        "baseline": journal_metadata["snapshot_lineage_id"],
        "sequence": len(rows),
        "targets": sorted(targets),
        "version": 1,
    }


def _safe_directory(directory: Path) -> Path:
    directory = directory.absolute()
    if directory.is_symlink():
        fail("configured backup directory cannot be a symlink")
    private_dir(directory)
    return directory.resolve(strict=True)


@contextlib.contextmanager
def _manifest_lock(directory: Path):
    path = directory / LOCK_NAME
    fd = os.open(path, os.O_RDWR | os.O_CREAT | getattr(os, "O_NOFOLLOW", 0), 0o600)
    try:
        st = os.fstat(fd)
        if not stat.S_ISREG(st.st_mode) or st.st_uid != os.getuid():
            fail("retention lock must be a current-user-owned regular file")
        os.fchmod(fd, 0o600)
        fcntl.flock(fd, fcntl.LOCK_EX)
        yield
    finally:
        os.close(fd)


def _read_manifest(directory: Path, key: bytes) -> dict[str, Any]:
    path = directory / MANIFEST_NAME
    if len(key) != 32:
        fail("retention authentication key must contain exactly 32 bytes")
    if not path.exists():
        return {"format": FORMAT, "entries": []}
    st = path.lstat()
    if not stat.S_ISREG(st.st_mode) or st.st_uid != os.getuid() or stat.S_IMODE(st.st_mode) != 0o600:
        fail("retention manifest must be a current-user-owned regular file with mode 0600")
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        fail(f"retention manifest is invalid: {exc}")
    if not isinstance(document, dict) or set(document) != {"format", "entries", "hmac_sha256"} or document["format"] != FORMAT or not isinstance(document["entries"], list):
        fail("retention manifest has an unsupported schema")
    signature = document.pop("hmac_sha256")
    expected = hmac.new(key, _canonical(document), hashlib.sha256).hexdigest()
    if not isinstance(signature, str) or not hmac.compare_digest(signature, expected):
        fail("retention manifest authentication failed")
    seen: set[str] = set()
    for entry in document["entries"]:
        _validate_entry(entry)
        if entry["artifact"] in seen:
            fail("retention manifest has duplicate artifact records")
        seen.add(entry["artifact"])
    return document


def _write_manifest(directory: Path, document: dict[str, Any], key: bytes) -> None:
    if len(key) != 32:
        fail("retention authentication key must contain exactly 32 bytes")
    unsigned = {"format": FORMAT, "entries": document["entries"]}
    signed = {**unsigned, "hmac_sha256": hmac.new(key, _canonical(unsigned), hashlib.sha256).hexdigest()}
    fd, temp_name = tempfile.mkstemp(prefix=f".{MANIFEST_NAME}.", suffix=".tmp", dir=directory)
    temp = Path(temp_name)
    try:
        raw = (json.dumps(signed, sort_keys=True, separators=(",", ":")) + "\n").encode()
        with os.fdopen(fd, "wb") as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temp, directory / MANIFEST_NAME)
        _fsync_dir(directory)
    finally:
        try:
            temp.unlink()
        except FileNotFoundError:
            pass


def _validate_entry(entry: Any) -> None:
    if not isinstance(entry, dict) or set(entry) != ENTRY_FIELDS:
        fail("retention manifest entry has an unsupported schema")
    if not isinstance(entry["artifact"], str) or not ARTIFACT_RE.fullmatch(entry["artifact"]):
        fail("retention manifest artifact path is unsafe")
    if not isinstance(entry["sha256"], str) or not re.fullmatch(r"[0-9a-f]{64}", entry["sha256"]):
        fail("retention manifest artifact hash is invalid")
    if isinstance(entry["size"], bool) or not isinstance(entry["size"], int) or entry["size"] < 0:
        fail("retention manifest artifact size is invalid")
    _parse_timestamp(entry["created_at"])
    if not isinstance(entry["deletion_baseline"], str) or isinstance(entry["deletion_sequence"], bool) or not isinstance(entry["deletion_sequence"], int) or entry["deletion_sequence"] < 0:
        fail("retention manifest deletion baseline is invalid")
    if not isinstance(entry["deletion_targets"], list) or not all(isinstance(x, str) for x in entry["deletion_targets"]):
        fail("retention manifest deletion targets are invalid")
    if entry["deletion_version"] != 1:
        fail("retention manifest deletion version is unsupported")


def _deletion_value(deletion: dict[str, Any] | None, key: str, default: Any) -> Any:
    if deletion is None:
        return default
    return deletion.get(key, default)


def _deletion_targets(deletion: dict[str, Any] | None) -> list[str]:
    if not deletion:
        return []
    targets = deletion.get("cleanup_targets", [])
    if not isinstance(targets, list) or not all(isinstance(target, str) for target in targets):
        fail("backup deletion targets are malformed")
    return sorted(set(targets))


def _parse_timestamp(value: Any) -> datetime:
    if not isinstance(value, str):
        fail("retention manifest timestamp is invalid")
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        fail("retention manifest timestamp is invalid")
    if parsed.tzinfo is None:
        fail("retention manifest timestamp must include a timezone")
    return parsed.astimezone(timezone.utc)


def _iso(value: datetime) -> str:
    if value.tzinfo is None:
        fail("backup creation time must include a timezone")
    return value.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")


def _canonical(value: dict[str, Any]) -> bytes:
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def _hash_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def _fsync_dir(directory: Path) -> None:
    fd = os.open(directory, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
    try:
        os.fsync(fd)
    finally:
        os.close(fd)

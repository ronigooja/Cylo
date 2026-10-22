from __future__ import annotations

from datetime import datetime, timedelta, timezone
import json
import os
from pathlib import Path
import tempfile
import unittest

from data_lifecycle.retention import MANIFEST_NAME, purge_expired, register_backup


class BackupRetentionTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.directory = Path(self.temp.name) / "configured-backups"
        self.directory.mkdir(mode=0o700)
        self.key = b"k" * 32
        self.now = datetime(2026, 10, 22, tzinfo=timezone.utc)

    def tearDown(self) -> None:
        self.temp.cleanup()

    def backup(self, name: str, created: datetime, content: bytes = b"fixture") -> Path:
        artifact = self.directory / name
        artifact.write_bytes(content)
        os.chmod(artifact, 0o600)
        register_backup(self.directory, artifact, created, {
            "snapshot_lineage_id": "fixture-lineage",
            "snapshot_sequence": 7,
            "cleanup_targets": ["qdrant", "backup_delete_manifest"],
        }, self.key)
        return artifact

    def test_default_policy_removes_only_manifest_owned_expired_backup(self):
        expired = self.backup("cylo-20260901T000000Z-aabbccdd.cylo-backup", self.now - timedelta(days=31))
        fresh = self.backup("cylo-20261001T000000Z-aabbccdd.cylo-backup", self.now - timedelta(days=21))
        unknown = self.directory / "cylo-20200101T000000Z-ffffffff.cylo-backup"
        unknown.write_bytes(b"existing unmanaged backup")

        removed = purge_expired(self.directory, self.key, now=self.now)

        self.assertEqual(removed, 1)
        self.assertFalse(expired.exists())
        self.assertTrue(fresh.exists())
        self.assertTrue(unknown.exists())
        manifest = json.loads((self.directory / MANIFEST_NAME).read_text())
        self.assertEqual([row["artifact"] for row in manifest["entries"]], [fresh.name])
        self.assertEqual(manifest["entries"][0]["deletion_baseline"], "fixture-lineage")
        self.assertEqual(manifest["entries"][0]["deletion_sequence"], 7)
        self.assertEqual(manifest["entries"][0]["deletion_targets"], ["backup_delete_manifest", "qdrant"])
        self.assertEqual(manifest["entries"][0]["deletion_version"], 1)

    def test_authenticated_manifest_tampering_fails_before_deletion(self):
        artifact = self.backup("cylo-20260901T000000Z-aabbccdd.cylo-backup", self.now - timedelta(days=31))
        path = self.directory / MANIFEST_NAME
        document = json.loads(path.read_text())
        document["entries"][0]["artifact"] = "../valuable.cylo-backup"
        path.write_text(json.dumps(document))
        os.chmod(path, 0o600)

        with self.assertRaisesRegex(RuntimeError, "authentication failed"):
            purge_expired(self.directory, self.key, now=self.now)
        self.assertTrue(artifact.exists())

    def test_future_timestamp_fails_closed(self):
        future = self.backup("cylo-20261101T000000Z-aabbccdd.cylo-backup", self.now + timedelta(days=1))
        with self.assertRaisesRegex(RuntimeError, "future backup timestamp"):
            purge_expired(self.directory, self.key, now=self.now)
        self.assertTrue(future.exists())

    def test_modified_artifact_and_symlink_fail_closed(self):
        artifact = self.backup("cylo-20260901T000000Z-aabbccdd.cylo-backup", self.now - timedelta(days=31))
        artifact.write_bytes(b"changed")
        with self.assertRaisesRegex(RuntimeError, "does not match"):
            purge_expired(self.directory, self.key, now=self.now)
        artifact.unlink()
        artifact.symlink_to(self.directory / "unknown")
        with self.assertRaisesRegex(RuntimeError, "symlink"):
            purge_expired(self.directory, self.key, now=self.now)

    def test_path_traversal_record_fails_closed(self):
        artifact = self.backup("cylo-20260901T000000Z-aabbccdd.cylo-backup", self.now - timedelta(days=31))
        path = self.directory / MANIFEST_NAME
        document = json.loads(path.read_text())
        document["entries"][0]["artifact"] = "../outside"
        path.write_text(json.dumps(document))
        os.chmod(path, 0o600)
        with self.assertRaisesRegex(RuntimeError, "authentication failed"):
            purge_expired(self.directory, self.key, now=self.now)
        self.assertTrue(artifact.exists())


if __name__ == "__main__":
    unittest.main()

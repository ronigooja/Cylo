import unittest
import sqlite3
import tempfile
from pathlib import Path

from data_lifecycle.common import load_inventory, sqlite_snapshot


class LoadInventoryTests(unittest.TestCase):
    def test_missing_inventory_is_an_empty_managed_inventory(self):
        self.assertEqual(
            load_inventory(None),
            {
                "roots": {},
                "service_config_paths": [],
                "artifact_paths": [],
                "persistent_volume_paths": [],
                "secret_refs": [],
            },
        )


class SnapshotDeletionLineageTests(unittest.TestCase):
    def test_ledger_watermark_and_instance_lineage_come_from_snapshot(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "source.sqlite"
            snapshot = Path(directory) / "snapshot.sqlite"
            with sqlite3.connect(source) as db:
                db.executescript(
                    """
                    CREATE TABLE cylo_instance_state(singleton INTEGER PRIMARY KEY, instance_id TEXT, epoch INTEGER, dispatch_enabled INTEGER, derived_retrieval_enabled INTEGER, fence_reason TEXT, fenced_at TEXT);
                    INSERT INTO cylo_instance_state VALUES(1,'source-a',7,1,1,'','');
                    CREATE TABLE memory_deletion_ledger_state(singleton INTEGER PRIMARY KEY, ledger_id TEXT, ledger_generation INTEGER, baseline_kind TEXT, initialized_at TEXT);
                    INSERT INTO memory_deletion_ledger_state VALUES(1,'ledger-a',1,'legacy_snapshot_only','2026-10-22T00:00:00Z');
                    CREATE TABLE memory_deletion_events(sequence INTEGER PRIMARY KEY AUTOINCREMENT, ledger_generation INTEGER, event_type TEXT, workspace_id TEXT, claim_id TEXT, revision INTEGER, deleted_at TEXT);
                    INSERT INTO memory_deletion_events VALUES(1,1,'claim_deleted','default','claim-a',2,'2026-10-22T00:01:00Z');
                    INSERT INTO memory_deletion_events VALUES(2,1,'claim_deleted','default','claim-b',4,'2026-10-22T00:02:00Z');
                    """
                )
            metadata = sqlite_snapshot(source, snapshot)
            self.assertEqual(metadata["instance_state"]["instance_id"], "source-a")
            self.assertEqual(metadata["instance_state"]["epoch"], 7)
            self.assertEqual(
                metadata["deletion_ledger"],
                {
                    "ledger_id": "ledger-a",
                    "generation": 1,
                    "baseline_kind": "legacy_snapshot_only",
                    "initialized_at": "2026-10-22T00:00:00Z",
                    "watermark": 2,
                    "event_count": 2,
                },
            )

    def test_snapshot_rejects_non_contiguous_ledger(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "source.sqlite"
            snapshot = Path(directory) / "snapshot.sqlite"
            with sqlite3.connect(source) as db:
                db.executescript(
                    """
                    CREATE TABLE memory_deletion_ledger_state(singleton INTEGER PRIMARY KEY, ledger_id TEXT, ledger_generation INTEGER, baseline_kind TEXT, initialized_at TEXT);
                    INSERT INTO memory_deletion_ledger_state VALUES(1,'ledger-a',1,'legacy_snapshot_only','now');
                    CREATE TABLE memory_deletion_events(sequence INTEGER PRIMARY KEY AUTOINCREMENT, ledger_generation INTEGER, event_type TEXT, workspace_id TEXT, claim_id TEXT, revision INTEGER, deleted_at TEXT);
                    INSERT INTO memory_deletion_events VALUES(2,1,'claim_deleted','default','claim-b',4,'now');
                    """
                )
            with self.assertRaisesRegex(RuntimeError, "not contiguous"):
                sqlite_snapshot(source, snapshot)


if __name__ == "__main__":
    unittest.main()

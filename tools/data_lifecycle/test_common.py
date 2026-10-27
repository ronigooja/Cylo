import unittest
import sqlite3
import tempfile
import json
from pathlib import Path

from data_lifecycle.common import (
    FORMAT,
    deletion_ledger_metadata,
    independent_deletion_journal_metadata,
    file_record,
    load_inventory,
    sqlite_snapshot,
    verify_manifest,
)


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
                    CREATE TABLE zoe_instance_state(singleton INTEGER PRIMARY KEY, instance_id TEXT, epoch INTEGER, dispatch_enabled INTEGER, derived_retrieval_enabled INTEGER, fence_reason TEXT, fenced_at TEXT, lineage_id TEXT, generation INTEGER, deletion_sequence INTEGER);
                    INSERT INTO zoe_instance_state VALUES(1,'source-a',7,1,1,'','','source-lineage-a',2,0);
                    CREATE TABLE events(event_id TEXT, cursor INTEGER);
                    INSERT INTO events VALUES('event-a',3);
                    INSERT INTO events VALUES('event-b',5);
                    CREATE TABLE memory_deletion_ledger_state(singleton INTEGER PRIMARY KEY, ledger_id TEXT, ledger_generation INTEGER, baseline_kind TEXT, initialized_at TEXT);
                    INSERT INTO memory_deletion_ledger_state VALUES(1,'11111111-1111-4111-8111-111111111111',1,'legacy_snapshot_only','2026-10-22T00:00:00Z');
                    CREATE TABLE memory_deletion_events(sequence INTEGER PRIMARY KEY AUTOINCREMENT, ledger_generation INTEGER, event_type TEXT, workspace_id TEXT, claim_id TEXT, revision INTEGER, deleted_at TEXT);
                    INSERT INTO memory_deletion_events VALUES(1,1,'claim_deleted','default','claim-a',2,'2026-10-22T00:01:00Z');
                    INSERT INTO memory_deletion_events VALUES(2,1,'claim_deleted','default','claim-b',4,'2026-10-22T00:02:00Z');
                    """
                )
            metadata = sqlite_snapshot(source, snapshot)
            self.assertEqual(metadata["instance_state"]["instance_id"], "source-a")
            self.assertEqual(metadata["instance_state"]["epoch"], 7)
            self.assertEqual(metadata["instance_state"]["lineage_id"], "source-lineage-a")
            self.assertEqual(metadata["instance_state"]["event_count"], 2)
            self.assertEqual(metadata["instance_state"]["event_cursor_sum"], 8)
            self.assertEqual(
                metadata["deletion_ledger"],
                {
                    "ledger_id": "11111111-1111-4111-8111-111111111111",
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
                    INSERT INTO memory_deletion_ledger_state VALUES(1,'11111111-1111-4111-8111-111111111111',1,'legacy_snapshot_only','2026-10-22T00:00:00Z');
                    CREATE TABLE memory_deletion_events(sequence INTEGER PRIMARY KEY AUTOINCREMENT, ledger_generation INTEGER, event_type TEXT, workspace_id TEXT, claim_id TEXT, revision INTEGER, deleted_at TEXT);
                    INSERT INTO memory_deletion_events VALUES(2,1,'claim_deleted','default','claim-b',4,'2026-10-22T00:01:00Z');
                    """
                )
            with self.assertRaisesRegex(RuntimeError, "not contiguous"):
                sqlite_snapshot(source, snapshot)


class RestoreDeletionLedgerMetadataTests(unittest.TestCase):
    def make_payload(self, base: Path, *, with_ledger: bool = True) -> tuple[Path, dict]:
        payload = base / "payload"
        (payload / "authority").mkdir(parents=True)
        database_path = payload / "authority" / "zoe.sqlite"
        sidecar_path = payload / "authority" / "zoe.sqlite.deletion-ledger"
        sidecar_path.write_bytes(b"")
        sidecar_path.chmod(0o600)
        with sqlite3.connect(database_path) as db:
            db.executescript(
                """
                CREATE TABLE events(event_id TEXT,cursor INTEGER);
                CREATE TABLE zoe_instance_state(singleton INTEGER PRIMARY KEY,lineage_id TEXT,generation INTEGER,deletion_sequence INTEGER);
                INSERT INTO zoe_instance_state VALUES(1,'lineage-fixture',1,0);
                """
            )
            if with_ledger:
                db.executescript(
                    """
                    CREATE TABLE memory_deletion_ledger_state(singleton INTEGER PRIMARY KEY, ledger_id TEXT, ledger_generation INTEGER, baseline_kind TEXT, initialized_at TEXT);
                    INSERT INTO memory_deletion_ledger_state VALUES(1,'22222222-2222-4222-8222-222222222222',1,'legacy_snapshot_only','2026-10-22T00:00:00Z');
                    CREATE TABLE memory_deletion_events(sequence INTEGER PRIMARY KEY AUTOINCREMENT, ledger_generation INTEGER, event_type TEXT, workspace_id TEXT, claim_id TEXT, revision INTEGER, deleted_at TEXT);
                    INSERT INTO memory_deletion_events VALUES(1,1,'claim_deleted','default','claim-a',2,'2026-10-22T00:01:00Z');
                    """
                )
            ledger_metadata = deletion_ledger_metadata(db)
        manifest = {
            "format": FORMAT,
            "schema_version": 1,
            "database": {
                "integrity": "ok",
                "migrations": [],
                "deletion_ledger": ledger_metadata,
                "trusted_deletion_head": independent_deletion_journal_metadata(
                    sidecar_path,
                    {"lineage_id": "lineage-fixture", "generation": 1, "deletion_sequence": 0, "event_count": 0, "event_cursor_sum": 0},
                ),
            },
            "files": [file_record(database_path, "authority", "authority/zoe.sqlite"), file_record(sidecar_path, "authority", "authority/zoe.sqlite.deletion-ledger")],
        }
        (payload / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
        return payload, manifest

    def update_manifest_files(self, manifest, payload, database_path):
        sidecar = payload / "authority" / "zoe.sqlite.deletion-ledger"
        manifest["files"] = [
            file_record(database_path, "authority", "authority/zoe.sqlite"),
            file_record(sidecar, "authority", "authority/zoe.sqlite.deletion-ledger"),
        ]

    def test_restore_recomputes_and_accepts_matching_ledger_metadata(self):
        with tempfile.TemporaryDirectory() as directory:
            payload, _ = self.make_payload(Path(directory))
            verified = verify_manifest(payload)
            self.assertEqual(verified["database"]["deletion_ledger"]["watermark"], 1)

    def test_restore_rejects_manifest_watermark_mismatch(self):
        with tempfile.TemporaryDirectory() as directory:
            payload, manifest = self.make_payload(Path(directory))
            manifest["database"]["deletion_ledger"]["watermark"] = 0
            (payload / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
            with self.assertRaisesRegex(RuntimeError, "does not match SQLite"):
                verify_manifest(payload)

    def test_restore_rejects_missing_or_wrongly_typed_ledger_metadata(self):
        with tempfile.TemporaryDirectory() as directory:
            payload, manifest = self.make_payload(Path(directory))
            del manifest["database"]["deletion_ledger"]
            (payload / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
            with self.assertRaisesRegex(RuntimeError, "missing or malformed"):
                verify_manifest(payload)

        with tempfile.TemporaryDirectory() as directory:
            payload, manifest = self.make_payload(Path(directory))
            manifest["database"]["deletion_ledger"]["generation"] = True
            (payload / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
            with self.assertRaisesRegex(RuntimeError, "invalid field types"):
                verify_manifest(payload)

    def test_restore_rejects_malformed_database_ledger_rows(self):
        with tempfile.TemporaryDirectory() as directory:
            payload, _ = self.make_payload(Path(directory))
            database_path = payload / "authority" / "zoe.sqlite"
            with sqlite3.connect(database_path) as db:
                db.execute("UPDATE memory_deletion_events SET ledger_generation=2")
            manifest = json.loads((payload / "manifest.json").read_text(encoding="utf-8"))
            self.update_manifest_files(manifest, payload, database_path)
            (payload / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
            with self.assertRaisesRegex(RuntimeError, "malformed event"):
                verify_manifest(payload)

    def test_restore_rejects_unsupported_database_ledger_baseline(self):
        with tempfile.TemporaryDirectory() as directory:
            payload, _ = self.make_payload(Path(directory))
            database_path = payload / "authority" / "zoe.sqlite"
            with sqlite3.connect(database_path) as db:
                db.execute("UPDATE memory_deletion_ledger_state SET baseline_kind='future_baseline'")
            manifest = json.loads((payload / "manifest.json").read_text(encoding="utf-8"))
            self.update_manifest_files(manifest, payload, database_path)
            (payload / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
            with self.assertRaisesRegex(RuntimeError, "baseline is unsupported"):
                verify_manifest(payload)

    def test_restore_rejects_duplicate_event_sequences_in_malformed_table(self):
        with tempfile.TemporaryDirectory() as directory:
            payload, manifest = self.make_payload(Path(directory))
            database_path = payload / "authority" / "zoe.sqlite"
            with sqlite3.connect(database_path) as db:
                db.executescript(
                    """
                    DROP TABLE memory_deletion_events;
                    CREATE TABLE memory_deletion_events(sequence INTEGER, ledger_generation INTEGER, event_type TEXT, workspace_id TEXT, claim_id TEXT, revision INTEGER, deleted_at TEXT);
                    INSERT INTO memory_deletion_events VALUES(1,1,'claim_deleted','default','claim-a',2,'2026-10-22T00:01:00Z');
                    INSERT INTO memory_deletion_events VALUES(1,1,'claim_deleted','default','claim-b',3,'2026-10-22T00:02:00Z');
                    INSERT INTO memory_deletion_events VALUES(3,1,'claim_deleted','default','claim-c',4,'2026-10-22T00:03:00Z');
                    INSERT INTO sqlite_sequence(name,seq) VALUES('memory_deletion_events',3);
                    """
                )
            self.update_manifest_files(manifest, payload, database_path)
            (payload / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
            with self.assertRaisesRegex(RuntimeError, "sequence is not contiguous"):
                verify_manifest(payload)

    def test_restore_rejects_fractional_and_duplicate_sequence_high_waters(self):
        for sequence_updates, expected_error in (
            ("UPDATE sqlite_sequence SET seq=1.5 WHERE name='memory_deletion_events';", "high-water is invalid"),
            ("INSERT INTO sqlite_sequence(name,seq) VALUES('memory_deletion_events',1);", "high-water is ambiguous"),
        ):
            with self.subTest(expected_error=expected_error), tempfile.TemporaryDirectory() as directory:
                payload, manifest = self.make_payload(Path(directory))
                database_path = payload / "authority" / "zoe.sqlite"
                with sqlite3.connect(database_path) as db:
                    db.execute(sequence_updates)
                self.update_manifest_files(manifest, payload, database_path)
                (payload / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
                with self.assertRaisesRegex(RuntimeError, expected_error):
                    verify_manifest(payload)

    def test_restore_rejects_ledger_tables_missing_after_migration(self):
        with tempfile.TemporaryDirectory() as directory:
            payload, manifest = self.make_payload(Path(directory))
            database_path = payload / "authority" / "zoe.sqlite"
            with sqlite3.connect(database_path) as db:
                db.executescript(
                    """
                    DROP TABLE memory_deletion_events;
                    DROP TABLE memory_deletion_ledger_state;
                    CREATE TABLE schema_migrations(namespace TEXT, version INTEGER, name TEXT);
                    INSERT INTO schema_migrations VALUES('memory',102,'memory_claim_deletion_ledger_v1');
                    """
                )
            manifest["database"]["migrations"] = [
                {"namespace": "memory", "version": 102, "name": "memory_claim_deletion_ledger_v1"}
            ]
            manifest["database"].pop("deletion_ledger")
            self.update_manifest_files(manifest, payload, database_path)
            (payload / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
            with self.assertRaisesRegex(RuntimeError, "missing after their migration"):
                verify_manifest(payload)

    def test_restore_accepts_legacy_database_only_as_explicit_missing_ledger(self):
        with tempfile.TemporaryDirectory() as directory:
            payload, manifest = self.make_payload(Path(directory), with_ledger=False)
            verified = verify_manifest(payload)
            self.assertIsNone(verified["database"]["deletion_ledger"])

            manifest["database"]["deletion_ledger"] = {
                "ledger_id": "22222222-2222-4222-8222-222222222222",
                "generation": 1,
                "baseline_kind": "legacy_snapshot_only",
                "initialized_at": "2026-10-22T00:00:00Z",
                "watermark": 0,
                "event_count": 0,
            }
            (payload / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
            with self.assertRaisesRegex(RuntimeError, "legacy SQLite database"):
                verify_manifest(payload)


if __name__ == "__main__":
    unittest.main()

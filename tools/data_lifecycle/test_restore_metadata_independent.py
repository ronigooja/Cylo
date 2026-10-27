"""Independent restore validation negatives on consistent archive payloads."""
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest

from data_lifecycle.common import file_record, verify_manifest
from data_lifecycle import test_common as fixtures


class IndependentLedgerCorruptionTests(unittest.TestCase):
    def assert_rejected(self, sql, *, watermark=None):
        with tempfile.TemporaryDirectory() as directory:
            payload, manifest = fixtures.RestoreDeletionLedgerMetadataTests().make_payload(Path(directory))
            database = payload / "authority" / "zoe.sqlite"
            with sqlite3.connect(database) as connection:
                connection.executescript(sql)
            if watermark is not None:
                manifest["database"]["deletion_ledger"]["watermark"] = watermark
                manifest["database"]["deletion_ledger"]["event_count"] = watermark
            sidecar = payload / "authority" / "zoe.sqlite.deletion-ledger"
            manifest["files"] = [file_record(database, "authority", "authority/zoe.sqlite"), file_record(sidecar, "authority", "authority/zoe.sqlite.deletion-ledger")]
            (payload / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
            with self.assertRaises((RuntimeError, sqlite3.DatabaseError)):
                verify_manifest(payload)

    def test_duplicate_sequence_hides_a_missing_event(self):
        self.assert_rejected("""
            ALTER TABLE memory_deletion_events RENAME TO old_events;
            CREATE TABLE memory_deletion_events AS SELECT * FROM old_events;
            INSERT INTO memory_deletion_events SELECT * FROM old_events;
            INSERT INTO memory_deletion_events SELECT 3,ledger_generation,event_type,workspace_id,'claim-c',revision,deleted_at FROM old_events;
            INSERT INTO sqlite_sequence(name,seq) VALUES('memory_deletion_events',3);
            DROP TABLE old_events;
        """, watermark=3)

    def test_fractional_high_water_is_not_truncated(self):
        self.assert_rejected("UPDATE sqlite_sequence SET seq=1.5 WHERE name='memory_deletion_events';")

    def test_duplicate_high_water_rows_are_not_ignored(self):
        self.assert_rejected("INSERT INTO sqlite_sequence(name,seq) VALUES('memory_deletion_events',2);")

    def test_partial_ledger_schema_is_rejected(self):
        self.assert_rejected("DROP TABLE memory_deletion_events;")

    def test_missing_state_singleton_is_rejected(self):
        self.assert_rejected("DELETE FROM memory_deletion_ledger_state;")

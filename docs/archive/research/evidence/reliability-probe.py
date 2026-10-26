"""Isolated research probes; stdlib only, no network or product runtime.

Run: python3 docs/research/evidence/reliability-probe.py
No power-loss, provider, sandbox, or production performance claim is made.
"""
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile


def connect(path):
    db = sqlite3.connect(path)
    db.execute("PRAGMA journal_mode=WAL")
    db.execute("PRAGMA synchronous=FULL")
    return db


def child(root, mode):
    db = connect(root / "control.db")
    if mode in ("commit", "rollback"):
        db.execute("BEGIN IMMEDIATE")
        for table in ("command", "event", "job"):
            db.execute(f"INSERT INTO {table} VALUES (?)", (mode,))
        if mode == "commit":
            db.commit()
        os._exit(17)  # bypass interpreter cleanup, after/before commit
    if mode == "effect":
        provider = connect(root / "provider.db")
        provider.execute("INSERT OR IGNORE INTO effect VALUES ('op-1')")
        provider.commit()
        os._exit(18)  # effect happened; control receipt has not been saved


def run():
    results = {"sqlite_version": sqlite3.sqlite_version}
    with tempfile.TemporaryDirectory(prefix="cylo-reliability-") as directory:
        root = Path(directory)
        db = connect(root / "control.db")
        for table in ("command", "event", "job"):
            db.execute(f"CREATE TABLE {table}(id TEXT PRIMARY KEY)")
        db.execute("CREATE TABLE lease(resource TEXT PRIMARY KEY, epoch INTEGER)")
        db.execute("INSERT INTO lease VALUES ('workspace', 1)")
        db.execute("CREATE TABLE receipt(id TEXT PRIMARY KEY)")
        db.commit()
        for mode in ("commit", "rollback"):
            p = subprocess.run([sys.executable, __file__, directory, mode])
            assert p.returncode == 17
        counts = {t: db.execute(f"SELECT id FROM {t}").fetchall()
                  for t in ("command", "event", "job")}
        assert all(value == [("commit",)] for value in counts.values())
        results["crash_atomic_command_event_job"] = "PASS"

        # Online backup while source remains open in WAL mode.
        backup = sqlite3.connect(root / "backup.db")
        db.backup(backup)
        assert backup.execute("PRAGMA integrity_check").fetchone() == ("ok",)
        assert backup.execute("SELECT id FROM job").fetchall() == [("commit",)]
        backup.close()
        results["online_backup_open_wal_database"] = "PASS"

        # An old direct filesystem writer is not controlled by a DB epoch.
        old_epoch = 1
        db.execute("UPDATE lease SET epoch=2 WHERE resource='workspace'")
        db.commit()
        accepted = db.execute("UPDATE lease SET epoch=epoch WHERE resource=? AND epoch=?",
                              ("workspace", old_epoch)).rowcount
        assert accepted == 0
        (root / "old-writer.txt").write_text("write after losing DB lease")
        assert (root / "old-writer.txt").exists()
        results["stale_epoch_rejected_by_db_but_direct_file_write_succeeds"] = "PASS"

        provider = connect(root / "provider.db")
        provider.execute("CREATE TABLE effect(id TEXT PRIMARY KEY)")
        provider.commit()
        p = subprocess.run([sys.executable, __file__, directory, "effect"])
        assert p.returncode == 18
        assert db.execute("SELECT count(*) FROM receipt").fetchone()[0] == 0
        assert provider.execute("SELECT count(*) FROM effect").fetchone()[0] == 1
        provider.execute("INSERT OR IGNORE INTO effect VALUES ('op-1')")
        provider.commit()
        assert provider.execute("SELECT count(*) FROM effect").fetchone()[0] == 1
        provider.execute("INSERT OR IGNORE INTO effect VALUES ('new-key')")
        provider.commit()
        assert provider.execute("SELECT count(*) FROM effect").fetchone()[0] == 2
        results["effect_receipt_gap_same_key_one_effect_new_key_two_effects"] = "PASS"
        db.close()
        provider.close()
    print(json.dumps(results, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    if len(sys.argv) == 3:
        child(Path(sys.argv[1]), sys.argv[2])
    else:
        run()

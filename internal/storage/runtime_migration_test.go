package storage

import (
	"context"
	"testing"
)

func TestRuntimeMigrationBackfillsAndNormalizesHarnesses(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, Config{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `CREATE TABLE tasks(id TEXT PRIMARY KEY, harness TEXT NOT NULL DEFAULT 'codex'); CREATE TABLE runs(id TEXT PRIMARY KEY, harness TEXT NOT NULL DEFAULT 'codex', isolation_policy TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO tasks(id,harness) VALUES('empty',''),('codex','codex'),('unknown','aider'); INSERT INTO runs(id,harness) VALUES('empty',''),('codex','codex'),('unknown','aider')`); err != nil {
		t.Fatal(err)
	}
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.migrations[12].Up(ctx, tx); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	rows, err := store.DB().QueryContext(ctx, `SELECT t.harness,r.harness,r.runtime_session_id FROM tasks t JOIN runs r ON r.id=t.id ORDER BY t.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := [][3]string{{"opencode", "opencode", ""}, {"opencode", "opencode", ""}, {"aider", "aider", ""}}
	for _, expected := range want {
		var got [3]string
		if !rows.Next() || rows.Scan(&got[0], &got[1], &got[2]) != nil || got != expected {
			t.Fatalf("migration row=%v want=%v", got, expected)
		}
	}
	if rows.Next() {
		t.Fatal("migration returned more rows than expected")
	}
	var quarantined int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM runtime_harness_quarantine WHERE harness='aider'`).Scan(&quarantined); err != nil || quarantined != 2 {
		t.Fatalf("unknown harness quarantine count=%d err=%v", quarantined, err)
	}
}

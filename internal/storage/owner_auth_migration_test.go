package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOwnerMigrationPreservesHistoricalSubjectAndRejectsLegacySessions(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, Config{Path: filepath.Join(t.TempDir(), "owner.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	// Exercise an actual upgrade from the pre-owner schema, with consumed setup.
	delete(store.migrations, 8)
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = store.DB().ExecContext(ctx, `INSERT INTO devices(id,workspace_id,subject_id,name,status,token_hash,created_at) VALUES('legacy','default','historical-owner','legacy','approved',X'01','2026-01-01T00:00:00Z');
 INSERT INTO bootstrap_state(id,code_hash,consumed_by_device_id,consumed_at) VALUES(1,X'02','legacy','2026-01-01T00:00:00Z');
 INSERT INTO projects(id,workspace_id,name,status,version,created_at,updated_at,owner_subject_id) VALUES('kept','default','kept','active',1,'now','now','historical-owner');`)
	if err != nil {
		t.Fatal(err)
	}
	if err = RegisterOwnerAuthMigration(store); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var subject, status, projectSubject string
	var setup bool
	if err = store.DB().QueryRow(`SELECT subject_id,password_hash IS NULL FROM owner_auth WHERE id=1`).Scan(&subject, &setup); err != nil {
		t.Fatal(err)
	}
	if err = store.DB().QueryRow(`SELECT status FROM devices WHERE id='legacy'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err = store.DB().QueryRow(`SELECT owner_subject_id FROM projects WHERE id='kept'`).Scan(&projectSubject); err != nil {
		t.Fatal(err)
	}
	if subject != "historical-owner" || projectSubject != subject || !setup || status != "revoked" {
		t.Fatalf("upgrade lost owner/data or trusted legacy session: %q %q %v %q", subject, projectSubject, setup, status)
	}
}

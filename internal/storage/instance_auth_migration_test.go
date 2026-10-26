package storage

import (
	"context"
	"strings"
	"testing"
)

func TestInstanceMigrationRejectsLegacyAuthority(t *testing.T) {
	for _, schema := range []string{`CREATE TABLE owner_auth(id INTEGER PRIMARY KEY)`, `CREATE TABLE devices(id TEXT PRIMARY KEY)`} {
		t.Run(schema, func(t *testing.T) {
			ctx := context.Background()
			store, err := Open(ctx, Config{Path: ":memory:"})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if _, err = store.DB().Exec(schema); err != nil {
				t.Fatal(err)
			}
			if err = RegisterCoreMigrations(store); err != nil {
				t.Fatal(err)
			}
			if err = store.Migrate(ctx); err == nil || !strings.Contains(err.Error(), "fresh") {
				t.Fatalf("legacy accepted: %v", err)
			}
		})
	}
}

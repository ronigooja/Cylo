package memory

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/ronigooja/Zoe/contracts"
	"github.com/ronigooja/Zoe/internal/control"
	"github.com/ronigooja/Zoe/internal/storage"
)

func TestRepositoryAndWorkerRetrievalFailClosedForRestore(t *testing.T) {
	ctx := context.Background()
	store, repo := newMemoryTestRepo(t)
	if err := control.RegisterInstanceMaintenanceMigration(store); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	scope := Scope{Kind: ScopePersonal, PrincipalID: "restore-owner"}
	claim := writeMemory(t, ctx, store.DB(), repo, scope, "snapshot-only claim", "restore-gate-source", 0)
	principal := contracts.Principal{WorkspaceID: "default", SubjectID: scope.PrincipalID, Scopes: []string{"memory:read:personal"}}
	if _, err := repo.Search(ctx, principal, "snapshot-only", SearchFilter{}); err != nil {
		t.Fatalf("fresh authority memory search unexpectedly failed: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE zoe_instance_state SET restore_deletion_reconciliation_required=1,derived_retrieval_enabled=0 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Search(ctx, principal, "snapshot-only", SearchFilter{}); !errors.Is(err, storage.ErrRestoreDeletionReconciliationRequired) {
		t.Fatalf("direct repository search error=%v", err)
	}
	if _, _, err := repo.AssembleContext(ctx, principal, "", principal.SubjectID, "snapshot-only", 256); !errors.Is(err, storage.ErrRestoreDeletionReconciliationRequired) {
		t.Fatalf("direct context assembly error=%v", err)
	}
	var manifests int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM memory_context_manifests`).Scan(&manifests); err != nil {
		t.Fatal(err)
	}
	if manifests != 0 {
		t.Fatalf("context manifest persisted behind restore gate: count=%d", manifests)
	}
	tx, err := store.DB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	service := &derivedService{db: store.DB(), cfg: DerivedConfig{}.withDefaults()}
	job := &derivedJob{WorkspaceID: "default", Kind: "index", EntityID: claim.ID, EntityVersion: claim.Revision}
	if _, _, err := service.materializeTx(ctx, tx, job); !errors.Is(err, storage.ErrRestoreDeletionReconciliationRequired) {
		t.Fatalf("direct worker materialization error=%v", err)
	}
}

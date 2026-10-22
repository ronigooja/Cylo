package memory

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/storage"
)

func TestDeleteReplaysFsyncedJournalWhenPriorAuthorityTransactionRolledBack(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "memory.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = RegisterMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = control.RegisterInstanceMaintenanceMigration(store); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewRepositoryWithStore(store)
	scope := Scope{Kind: ScopePersonal, PrincipalID: "replay-owner"}
	first := writeMemory(t, ctx, store.DB(), repo, scope, "must stay deleted", "source-before-crash", 0)
	second := writeMemory(t, ctx, store.DB(), repo, scope, "new deletion after restart", "source-after-crash", 0)
	if _, err = store.DB().ExecContext(ctx, `INSERT INTO memory_proposals(id,workspace_id,version,source_id,source_version,extractor_version,proposed_scope_kind,proposed_scope_id,proposed_principal_id,subject,claim_key,content,status,created_at)
VALUES('proposal-before-crash','default',1,'source-before-crash',1,'extractor-v1','personal','',?,'subject','key','private proposed content','pending',?)`, scope.PrincipalID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	var lineage string
	var generation int64
	if err = store.DB().QueryRowContext(ctx, `SELECT lineage_id,generation FROM cylo_instance_state WHERE singleton=1`).Scan(&lineage, &generation); err != nil {
		t.Fatal(err)
	}
	journal, err := storage.NewDeletionJournal(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	deletedAt := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = journal.Append(storage.DeletionRecord{
		Sequence: 1, LineageID: lineage, Generation: generation, WorkspaceID: "default",
		EntityKind: "memory_claim", EntityID: first.ID, EntityVersion: first.Revision + 1,
		ActorID: scope.PrincipalID, ReasonCode: "user_requested", DeletedAt: deletedAt,
		CleanupTargets: []string{"claim", "source_links", "proposals", "mem0", "qdrant", "cache", "export_temp", "backup_delete_manifest", "source_ledger"},
	})
	if err != nil {
		t.Fatal(err)
	}
	readTx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := &derivedService{db: store.DB(), cfg: DerivedConfig{}.withDefaults(), deletionJournal: journal}
	job := &derivedJob{ID: "old-index-job", WorkspaceID: "default", Kind: "index", EntityID: first.ID, EntityVersion: first.Revision}
	if _, disposition, materializeErr := service.materializeTx(ctx, readTx, job); materializeErr != nil || disposition != "stale_generation" {
		_ = readTx.Rollback()
		t.Fatalf("journalled claim reached derived worker: disposition=%q err=%v", disposition, materializeErr)
	}
	_ = readTx.Rollback()
	readTx, err = store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	linked, err := sourceHasJournalledDeletionTx(ctx, readTx, journal, "default", "source-before-crash", 1)
	_ = readTx.Rollback()
	if err != nil || !linked {
		t.Fatalf("journalled source remained eligible for proposals: linked=%v err=%v", linked, err)
	}
	if _, err = runDelete(ctx, store.DB(), repo, DeleteInput{WorkspaceID: "default", ActorID: scope.PrincipalID, ClaimID: second.ID, Expected: second.Revision, ReasonCode: "user_requested"}); err != nil {
		t.Fatalf("next delete did not repair prior rolled-back authority transaction: %v", err)
	}
	var sequence int64
	if err = store.DB().QueryRowContext(ctx, `SELECT deletion_sequence FROM cylo_instance_state WHERE singleton=1`).Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	if sequence != 2 {
		t.Fatalf("deletion sequence=%d, want both journal records applied", sequence)
	}
	principal := contracts.Principal{WorkspaceID: "default", SubjectID: scope.PrincipalID, Scopes: []string{"memory:read:personal"}}
	search, err := repo.Search(ctx, principal, "must stay deleted", SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Items) != 0 {
		t.Fatalf("replayed deletion became searchable: %#v", search.Items)
	}
	var status string
	var revision int64
	if err = store.DB().QueryRowContext(ctx, `SELECT status,revision FROM memory_claims WHERE id=?`, first.ID).Scan(&status, &revision); err != nil {
		t.Fatal(err)
	}
	if status != string(ClaimTombstoned) || revision != first.Revision+1 {
		t.Fatalf("replayed claim state=%s revision=%d", status, revision)
	}
	var proposalStatus, proposalContent string
	if err = store.DB().QueryRowContext(ctx, `SELECT status,content FROM memory_proposals WHERE id='proposal-before-crash'`).Scan(&proposalStatus, &proposalContent); err != nil {
		t.Fatal(err)
	}
	if proposalStatus != "deleted" || proposalContent != "" {
		t.Fatalf("replayed proposal retained content: status=%s content=%q", proposalStatus, proposalContent)
	}
	var sourceDeletedAt string
	if err = store.DB().QueryRowContext(ctx, `SELECT deleted_at FROM memory_sources WHERE workspace_id='default' AND id='source-before-crash' AND version=1`).Scan(&sourceDeletedAt); err != nil {
		t.Fatal(err)
	}
	if sourceDeletedAt == "" {
		t.Fatal("replayed deletion left the source live")
	}
}

func TestReconcileRequiresCompleteJournalAndKeepsMissingAdaptersClosed(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "restore.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = RegisterMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = control.RegisterInstanceMaintenanceMigration(store); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewRepositoryWithStore(store)
	scope := Scope{Kind: ScopePersonal, PrincipalID: "restore-owner"}
	claim := writeMemory(t, ctx, store.DB(), repo, scope, "restore tombstone", "restore-source", 0)
	var lineage string
	var generation int64
	if err = store.DB().QueryRowContext(ctx, `SELECT lineage_id,generation FROM cylo_instance_state WHERE singleton=1`).Scan(&lineage, &generation); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `UPDATE cylo_instance_state SET restore_deletion_reconciliation_required=1,derived_retrieval_enabled=0 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	journal, err := storage.NewDeletionJournal(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	appended, err := journal.Append(storage.DeletionRecord{Sequence: 1, LineageID: lineage, Generation: generation, WorkspaceID: "default", EntityKind: "memory_claim", EntityID: claim.ID, EntityVersion: claim.Revision + 1, ActorID: scope.PrincipalID, ReasonCode: "user_requested", DeletedAt: time.Now().UTC().Format(time.RFC3339Nano), CleanupTargets: []string{"claim", "source_links", "proposals", "mem0", "qdrant", "cache", "export_temp", "backup_delete_manifest", "source_ledger"}})
	if err != nil {
		t.Fatal(err)
	}
	trusted := TrustedDeletionHead{SnapshotLineageID: lineage, SnapshotGeneration: generation, SnapshotSequence: 0, SnapshotEventCount: 0, SnapshotEventSum: 0, JournalLineageID: lineage, JournalGeneration: generation, JournalSequence: 1, JournalHeadSHA256: appended.Hash}
	oldHead := trusted
	oldHead.JournalSequence = 0
	oldHead.JournalHeadSHA256 = ""
	if _, err = ReconcileDeletionJournalWithTrustedHead(ctx, store, oldHead); err == nil {
		t.Fatal("caller-supplied old journal head was accepted")
	}
	result, err := ReconcileDeletionJournalWithTrustedHead(ctx, store, trusted)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sequence != 1 || result.Replayed != 1 || result.GateCleared || len(result.BlockedTargets) == 0 {
		t.Fatalf("reconciliation result=%#v; missing cleanup adapters must keep restore gate closed", result)
	}
	if _, err = repo.Search(ctx, contracts.Principal{WorkspaceID: "default", SubjectID: scope.PrincipalID, Scopes: []string{"memory:read:personal"}}, "restore tombstone", SearchFilter{}); !errors.Is(err, storage.ErrRestoreDeletionReconciliationRequired) {
		t.Fatalf("restore gate opened with unresolved external cleanup: %v", err)
	}
	var completed int64
	if err = store.DB().QueryRowContext(ctx, `SELECT completed_sequence FROM cylo_deletion_reconciliation_checkpoints WHERE target='source_ledger'`).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatalf("local source ledger checkpoint=%d, want 1", completed)
	}
}

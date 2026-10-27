package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ronigooja/Zoe/contracts"
	"github.com/ronigooja/Zoe/internal/control"
	"github.com/ronigooja/Zoe/internal/storage"
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
	if err = store.DB().QueryRowContext(ctx, `SELECT lineage_id,generation FROM zoe_instance_state WHERE singleton=1`).Scan(&lineage, &generation); err != nil {
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
	if err = store.DB().QueryRowContext(ctx, `SELECT deletion_sequence FROM zoe_instance_state WHERE singleton=1`).Scan(&sequence); err != nil {
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
	if err = store.DB().QueryRowContext(ctx, `SELECT lineage_id,generation FROM zoe_instance_state WHERE singleton=1`).Scan(&lineage, &generation); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `UPDATE zoe_instance_state SET restore_deletion_reconciliation_required=1,derived_retrieval_enabled=0 WHERE singleton=1`); err != nil {
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
	journalBytes, err := os.ReadFile(store.Path() + ".deletion-ledger")
	if err != nil {
		t.Fatal(err)
	}
	fileHash := sha256.Sum256(journalBytes)
	trusted := TrustedDeletionHead{SnapshotLineageID: lineage, SnapshotGeneration: generation, SnapshotSequence: 0, SnapshotEventCount: 0, SnapshotEventSum: 0, JournalLineageID: lineage, JournalGeneration: generation, JournalRecordGeneration: generation, JournalSequence: 1, JournalHeadSHA256: appended.Hash, JournalFileSHA256: hex.EncodeToString(fileHash[:])}
	oldHead := trusted
	oldHead.JournalSequence = 0
	oldHead.JournalRecordGeneration = 0
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
	if err = store.DB().QueryRowContext(ctx, `SELECT completed_sequence FROM zoe_deletion_reconciliation_checkpoints WHERE target='source_ledger'`).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatalf("local source ledger checkpoint=%d, want 1", completed)
	}
	continued, err := ReconcileDeletionJournalWithTrustedHead(ctx, store, trusted)
	if err != nil {
		t.Fatalf("same authenticated restore state was not resumable: %v", err)
	}
	if continued.Sequence != 1 || continued.Replayed != 0 || continued.GateCleared {
		t.Fatalf("resumed reconciliation=%#v; it must preserve applied sequence and missing adapter blocks", continued)
	}
}

func TestStaleBackupReplaysLaterIndependentDeletion(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.db")
	store, err := storage.Open(ctx, storage.Config{Path: sourcePath})
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
	scope := Scope{Kind: ScopePersonal, PrincipalID: "stale-backup-owner"}
	claim := writeMemory(t, ctx, store.DB(), repo, scope, "must not return from a stale backup", "stale-backup-source", 0)
	var lineage string
	var generation, sequence, eventCount, eventSum int64
	if err = store.DB().QueryRowContext(ctx, `SELECT lineage_id,generation,deletion_sequence FROM zoe_instance_state WHERE singleton=1`).Scan(&lineage, &generation, &sequence); err != nil {
		t.Fatal(err)
	}
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*),coalesce(sum(cursor),0) FROM events`).Scan(&eventCount, &eventSum); err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(directory, "snapshot.db")
	if _, err = store.DB().ExecContext(ctx, `VACUUM INTO ?`, snapshotPath); err != nil {
		t.Fatal(err)
	}
	if _, err = runDelete(ctx, store.DB(), repo, DeleteInput{WorkspaceID: "default", ActorID: scope.PrincipalID, ClaimID: claim.ID, Expected: claim.Revision, ReasonCode: "user_requested"}); err != nil {
		t.Fatal(err)
	}
	journalBytes, err := os.ReadFile(sourcePath + ".deletion-ledger")
	if err != nil {
		t.Fatal(err)
	}
	fileHash := sha256.Sum256(journalBytes)
	journal, err := storage.NewDeletionJournal(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	records, err := journal.ReadAll()
	if err != nil || len(records) != 1 {
		t.Fatalf("retained deletion records=%d err=%v", len(records), err)
	}
	if err = os.WriteFile(snapshotPath+".deletion-ledger", journalBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := storage.Open(ctx, storage.Config{Path: snapshotPath})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if _, err = restored.DB().ExecContext(ctx, `UPDATE zoe_instance_state SET restore_deletion_reconciliation_required=1,derived_retrieval_enabled=0 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	trusted := TrustedDeletionHead{
		SnapshotLineageID: lineage, SnapshotGeneration: generation, SnapshotSequence: sequence,
		SnapshotEventCount: eventCount, SnapshotEventSum: eventSum,
		JournalLineageID: lineage, JournalGeneration: generation, JournalRecordGeneration: records[0].Generation,
		JournalSequence: 1, JournalHeadSHA256: records[0].Hash, JournalFileSHA256: hex.EncodeToString(fileHash[:]),
	}
	if _, err = ReconcileDeletionJournalWithTrustedHead(ctx, restored, trusted); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = restored.DB().QueryRowContext(ctx, `SELECT status FROM memory_claims WHERE workspace_id='default' AND id=?`, claim.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(ClaimTombstoned) {
		t.Fatalf("restored claim status=%q; deletion after backup was not replayed", status)
	}
	var liveClaims int
	if err = restored.DB().QueryRowContext(ctx, `SELECT count(*) FROM memory_claims WHERE workspace_id='default' AND id=? AND status<>'tombstoned'`, claim.ID).Scan(&liveClaims); err != nil {
		t.Fatal(err)
	}
	if liveClaims != 0 {
		t.Fatalf("stale backup restore retained %d live copies of deleted claim", liveClaims)
	}
}

func TestRestoreGenerationsAdvanceAcrossNoDeleteRestoresAndNextDelete(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "generation.db")})
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
	if err = os.WriteFile(store.Path()+".deletion-ledger", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	repo := NewRepositoryWithStore(store)
	scope := Scope{Kind: ScopePersonal, PrincipalID: "generation-owner"}
	claim := writeMemory(t, ctx, store.DB(), repo, scope, "generation test claim", "generation-source", 0)
	var lineage string
	var generation int64
	if err = store.DB().QueryRowContext(ctx, `SELECT lineage_id,generation FROM zoe_instance_state WHERE singleton=1`).Scan(&lineage, &generation); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `UPDATE zoe_instance_state SET restore_deletion_reconciliation_required=1,derived_retrieval_enabled=0,dispatch_enabled=0 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	trusted := TrustedDeletionHead{SnapshotLineageID: lineage, SnapshotGeneration: generation, JournalLineageID: lineage, JournalGeneration: generation}
	emptyHash := sha256.Sum256(nil)
	trusted.JournalFileSHA256 = hex.EncodeToString(emptyHash[:])
	first, err := ReconcileDeletionJournalWithTrustedHead(ctx, store, trusted)
	if err != nil || !first.GateCleared || first.Generation != generation+1 {
		t.Fatalf("first empty-head restore=%#v err=%v", first, err)
	}
	prepared, err := control.PrepareRestoredInstance(ctx, store, "second-restored-instance", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.RestoreDeletionReconciliationRequired {
		t.Fatalf("second restore was not closed: %#v", prepared)
	}
	var eventCount, eventSum int64
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*),coalesce(sum(cursor),0) FROM events`).Scan(&eventCount, &eventSum); err != nil {
		t.Fatal(err)
	}
	trusted.SnapshotGeneration = first.Generation
	trusted.SnapshotEventCount = eventCount
	trusted.SnapshotEventSum = eventSum
	trusted.JournalGeneration = first.Generation
	second, err := ReconcileDeletionJournalWithTrustedHead(ctx, store, trusted)
	if err != nil || !second.GateCleared || second.Generation != first.Generation+1 {
		t.Fatalf("second empty-head restore=%#v err=%v", second, err)
	}
	deleted, err := runDelete(ctx, store.DB(), repo, DeleteInput{WorkspaceID: "default", ActorID: scope.PrincipalID, ClaimID: claim.ID, Expected: claim.Revision, ReasonCode: "user_requested"})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Status != ClaimTombstoned {
		t.Fatalf("new-generation deletion status=%q", deleted.Status)
	}
	journal, err := storage.NewDeletionJournal(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	records, err := journal.ReadAll()
	if err != nil || len(records) != 1 || records[0].Generation != second.Generation || records[0].Sequence != 1 {
		t.Fatalf("first deletion after repeated restore records=%#v err=%v", records, err)
	}
}

func TestOldDeletionGenerationSurvivesRepeatedRestoreBeforeNextDelete(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "old-record-generation.db")})
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
	scope := Scope{Kind: ScopePersonal, PrincipalID: "old-generation-owner"}
	deletedClaim := writeMemory(t, ctx, store.DB(), repo, scope, "deleted before restore", "old-deletion-source", 0)
	newClaim := writeMemory(t, ctx, store.DB(), repo, Scope{Kind: ScopePersonal, PrincipalID: "old-generation-owner-next"}, "deleted after repeated restore", "new-deletion-source", 0)
	var lineage string
	var generation int64
	if err = store.DB().QueryRowContext(ctx, `SELECT lineage_id,generation FROM zoe_instance_state WHERE singleton=1`).Scan(&lineage, &generation); err != nil {
		t.Fatal(err)
	}
	if _, err = runDelete(ctx, store.DB(), repo, DeleteInput{WorkspaceID: "default", ActorID: scope.PrincipalID, ClaimID: deletedClaim.ID, Expected: deletedClaim.Revision, ReasonCode: "user_requested"}); err != nil {
		t.Fatal(err)
	}
	journal, err := storage.NewDeletionJournal(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	records, err := journal.ReadAll()
	if err != nil || len(records) != 1 {
		t.Fatalf("initial deletion journal records=%d err=%v", len(records), err)
	}
	fileBytes, err := os.ReadFile(store.Path() + ".deletion-ledger")
	if err != nil {
		t.Fatal(err)
	}
	fileHash := sha256.Sum256(fileBytes)
	markPrepared := func() {
		t.Helper()
		if _, err := store.DB().ExecContext(ctx, `UPDATE zoe_instance_state SET restore_deletion_reconciliation_required=1,derived_retrieval_enabled=0,dispatch_enabled=0 WHERE singleton=1`); err != nil {
			t.Fatal(err)
		}
	}
	markPrepared()
	if _, err = store.DB().ExecContext(ctx, `UPDATE zoe_deletion_reconciliation_checkpoints SET completed_sequence=1,last_error=''`); err != nil {
		t.Fatal(err)
	}
	trusted := TrustedDeletionHead{SnapshotLineageID: lineage, SnapshotGeneration: generation, SnapshotSequence: 1, JournalLineageID: lineage, JournalGeneration: generation, JournalRecordGeneration: records[0].Generation, JournalSequence: 1, JournalHeadSHA256: records[0].Hash, JournalFileSHA256: hex.EncodeToString(fileHash[:])}
	first, err := ReconcileDeletionJournalWithTrustedHead(ctx, store, trusted)
	if err != nil || !first.GateCleared || first.Generation != generation+1 {
		t.Fatalf("first restore with prior deletion=%#v err=%v", first, err)
	}
	if _, err = control.PrepareRestoredInstance(ctx, store, "old-deletion-second-restore", 1); err != nil {
		t.Fatal(err)
	}
	trusted.SnapshotGeneration = first.Generation
	trusted.JournalGeneration = first.Generation
	second, err := ReconcileDeletionJournalWithTrustedHead(ctx, store, trusted)
	if err != nil || !second.GateCleared || second.Generation != first.Generation+1 {
		t.Fatalf("second restore with older deletion record generation=%d trusted current generation=%d: result=%#v err=%v", records[0].Generation, trusted.JournalGeneration, second, err)
	}
	deleted, err := runDelete(ctx, store.DB(), repo, DeleteInput{WorkspaceID: "default", ActorID: "old-generation-owner-next", ClaimID: newClaim.ID, Expected: newClaim.Revision, ReasonCode: "user_requested"})
	if err != nil || deleted.Status != ClaimTombstoned {
		t.Fatalf("first post-restore deletion=%#v err=%v", deleted, err)
	}
	records, err = journal.ReadAll()
	if err != nil || len(records) != 2 || records[0].Generation != generation || records[1].Generation != second.Generation || records[1].Sequence != 2 {
		t.Fatalf("journal did not preserve old record and bind next deletion to active generation: records=%#v err=%v", records, err)
	}
}

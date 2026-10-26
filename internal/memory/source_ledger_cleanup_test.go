package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
)

func seedSourceLedgerTombstone(t *testing.T, ctx context.Context, store *sql.DB, repo *Repository) (Claim, Claim, int64) {
	t.Helper()
	scope := Scope{Kind: ScopePersonal, PrincipalID: "source-ledger-owner"}
	first := writeMemory(t, ctx, store, repo, scope, "private old claim content", "source-ledger-shared-source", 0)
	otherTx, err := store.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sharedSource := Source{ID: "source-ledger-shared-source", Version: 1, Scope: scope, Kind: "user_note", Reference: "command:source-ledger-shared-source", RecordedAt: now, Producer: "user"}
	second, err := repo.PutTx(ctx, otherTx, WriteInput{WorkspaceID: "default", ActorID: scope.PrincipalID, Source: sharedSource, Claim: Claim{WorkspaceID: "default", Scope: scope, Subject: "different subject", Key: "different key", Content: "other claim content", Authority: AuthorityUserConfirmed, Status: ClaimActive, Revision: 1, Sources: []Source{sharedSource}}})
	if err != nil {
		_ = otherTx.Rollback()
		t.Fatal(err)
	}
	if err := otherTx.Commit(); err != nil {
		t.Fatal(err)
	}
	tombstone, err := runDelete(ctx, store, repo, DeleteInput{WorkspaceID: "default", ActorID: scope.PrincipalID, ClaimID: first.ID, Expected: first.Revision, ReasonCode: "user_requested"})
	if err != nil {
		t.Fatal(err)
	}
	var tombstoneRevision int64
	if err := store.QueryRowContext(ctx, `SELECT revision FROM memory_tombstones WHERE workspace_id='default' AND claim_id=?`, first.ID).Scan(&tombstoneRevision); err != nil {
		t.Fatal(err)
	}
	return tombstone, second, tombstoneRevision
}

func addLegacySourceLedgerRows(t *testing.T, ctx context.Context, store *sql.DB, claimID, sharedSourceClaim string, revision int64) {
	t.Helper()
	if _, err := store.ExecContext(ctx, `INSERT INTO memory_revisions(claim_id,revision,content,authority_kind,status,recorded_at,actor_id) VALUES(?,?, 'legacy private text','user_confirmed','active','2026-01-01T00:00:00Z','fixture')`, claimID, revision); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExecContext(ctx, `INSERT INTO memory_revision_sources(claim_id,revision,workspace_id,source_id,source_version) VALUES(?,?,'default','source-ledger-shared-source',1)`, claimID, revision); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExecContext(ctx, `INSERT INTO memory_terms(workspace_id,claim_id,revision,term) VALUES('default',?,?, 'legacy')`, claimID, revision); err != nil {
		t.Fatal(err)
	}
	if sharedSourceClaim != "" {
		var count int
		if err := store.QueryRowContext(ctx, `SELECT count(*) FROM memory_revision_sources WHERE workspace_id='default' AND claim_id=? AND source_id='source-ledger-shared-source'`, sharedSourceClaim).Scan(&count); err != nil || count != 1 {
			t.Fatalf("shared source link missing for other claim: count=%d err=%v", count, err)
		}
	}
}

func createDistinctClaimSharingSource(t *testing.T, ctx context.Context, store *sql.DB, repo *Repository) Claim {
	t.Helper()
	scope := Scope{Kind: ScopePersonal, PrincipalID: "source-ledger-owner"}
	tx, err := store.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "source-ledger-shared-source", Version: 1, Scope: scope, Kind: "user_note", Reference: "command:source-ledger-shared-source", RecordedAt: time.Now().UTC(), Producer: "user"}
	claim, err := repo.PutTx(ctx, tx, WriteInput{WorkspaceID: "default", ActorID: scope.PrincipalID, Source: source, Claim: Claim{WorkspaceID: "default", Scope: scope, Subject: "third subject", Key: "third key", Content: "third claim content", Authority: AuthorityUserConfirmed, Status: ClaimActive, Revision: 1, Sources: []Source{source}}})
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return claim
}

func insertSourceLedgerJob(t *testing.T, ctx context.Context, store *sql.DB, id, claimID string, version int64, status, leaseOwner, leaseUntil, code string, attempts int) {
	t.Helper()
	if _, err := store.ExecContext(ctx, `DELETE FROM memory_jobs WHERE workspace_id='default' AND kind='delete_cleanup' AND entity_id=? AND entity_version=? AND target='source_ledger'`, claimID, version); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"claim_id": claimID, "target": "source_ledger"})
	_, err := store.ExecContext(ctx, `INSERT INTO memory_jobs(id,workspace_id,kind,entity_id,entity_version,target,payload,status,attempts,available_at,lease_owner,lease_until,last_error_code,created_at,updated_at) VALUES(?,'default','delete_cleanup',?,?,'source_ledger',?,?,?,?,?,?,?,?,?)`, id, claimID, version, payload, status, attempts, "2026-01-01T00:00:00Z", leaseOwner, nullableSQLString(leaseUntil), code, "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
}

func nullableSQLString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func TestSourceLedgerReconcileErasesOnlyMatchingClaimLedgerAndFencesOldLease(t *testing.T) {
	ctx := context.Background()
	store, repo := newMemoryTestRepo(t)
	tombstone, other, revision := seedSourceLedgerTombstone(t, ctx, store.DB(), repo)
	addLegacySourceLedgerRows(t, ctx, store.DB(), tombstone.ID, other.ID, revision-1)
	insertSourceLedgerJob(t, ctx, store.DB(), "source-ledger-active-lease", tombstone.ID, revision, "leased", "old-runner", time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano), "adapter_missing", 4)
	blockedClaim := createDistinctClaimSharingSource(t, ctx, store.DB(), repo)
	otherTombstone, err := runDelete(ctx, store.DB(), repo, DeleteInput{WorkspaceID: "default", ActorID: "source-ledger-owner", ClaimID: blockedClaim.ID, Expected: blockedClaim.Revision, ReasonCode: "user_requested"})
	if err != nil {
		t.Fatal(err)
	}
	var otherRevision int64
	if err := store.DB().QueryRowContext(ctx, `SELECT revision FROM memory_tombstones WHERE workspace_id='default' AND claim_id=?`, otherTombstone.ID).Scan(&otherRevision); err != nil {
		t.Fatal(err)
	}
	addLegacySourceLedgerRows(t, ctx, store.DB(), otherTombstone.ID, "", otherRevision-1)
	insertSourceLedgerJob(t, ctx, store.DB(), "source-ledger-blocked-adapter", otherTombstone.ID, otherRevision, "blocked", "", "", "adapter_missing", 8)

	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := &derivedService{db: store.DB(), cfg: DerivedConfig{}.withDefaults()}
	if err := service.reconcileTx(ctx, tx, "default", time.Now().UTC()); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var status, owner, disposition string
	var leaseUntil sql.NullString
	if err := store.DB().QueryRowContext(ctx, `SELECT status,lease_owner,lease_until,last_error_code FROM memory_jobs WHERE id='source-ledger-active-lease'`).Scan(&status, &owner, &leaseUntil, &disposition); err != nil {
		t.Fatal(err)
	}
	if status != "done" || owner != "" || leaseUntil.Valid || disposition != "source_ledger_cleaned" {
		t.Fatalf("Go cleanup did not settle and fence the old worker lease: status=%s owner=%q until=%v disposition=%s", status, owner, leaseUntil, disposition)
	}
	var blockedStatus, blockedError string
	if err := store.DB().QueryRowContext(ctx, `SELECT status,last_error_code FROM memory_jobs WHERE id='source-ledger-blocked-adapter'`).Scan(&blockedStatus, &blockedError); err != nil {
		t.Fatal(err)
	}
	if blockedStatus != "done" || blockedError != "source_ledger_cleaned" {
		t.Fatalf("previously blocked adapter job was not repaired by Go: status=%s error=%s", blockedStatus, blockedError)
	}
	for _, table := range []string{"memory_revisions", "memory_revision_sources", "memory_terms"} {
		var count int
		query := `SELECT count(*) FROM ` + table + ` WHERE claim_id=? AND revision<?`
		if err := store.DB().QueryRowContext(ctx, query, tombstone.ID, revision).Scan(&count); err != nil || count != 0 {
			t.Errorf("legacy %s rows remain for deleted claim: count=%d err=%v", table, count, err)
		}
	}
	var sourceCount, otherLinkCount, tombstoneRevisionCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM memory_sources WHERE workspace_id='default' AND id='source-ledger-shared-source'`).Scan(&sourceCount); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM memory_revision_sources WHERE workspace_id='default' AND claim_id=? AND source_id='source-ledger-shared-source'`, other.ID).Scan(&otherLinkCount); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM memory_revisions WHERE claim_id=? AND revision=? AND status='tombstoned' AND content=''`, tombstone.ID, revision).Scan(&tombstoneRevisionCount); err != nil {
		t.Fatal(err)
	}
	if sourceCount != 1 || otherLinkCount != 1 || tombstoneRevisionCount != 1 {
		t.Fatalf("cleanup crossed claim/source boundary: source=%d otherLink=%d tombstoneRevision=%d", sourceCount, otherLinkCount, tombstoneRevisionCount)
	}

	p := contracts.RunnerPrincipal{RunnerID: "old-runner", WorkspaceIDs: []string{"default"}, Scopes: []string{derivedCompleteScope}}
	for _, complete := range []bool{false, true} {
		if _, err := service.finish(ctx, p, "source-ledger-active-lease", 4, "adapter_missing", true, complete); err == nil {
			t.Fatalf("delayed worker result complete=%v was allowed to overwrite Go cleanup completion", complete)
		} else {
			var apiErr *control.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != "lease_lost" {
				t.Fatalf("delayed worker result complete=%v returned %v, want lease_lost", complete, err)
			}
		}
	}
}

func TestSourceLedgerStaleJobDoesNotEraseCurrentGeneration(t *testing.T) {
	ctx := context.Background()
	store, repo := newMemoryTestRepo(t)
	tombstone, other, revision := seedSourceLedgerTombstone(t, ctx, store.DB(), repo)
	addLegacySourceLedgerRows(t, ctx, store.DB(), tombstone.ID, other.ID, revision-1)
	insertSourceLedgerJob(t, ctx, store.DB(), "source-ledger-stale-generation", tombstone.ID, revision-1, "pending", "", "", "", 0)

	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	disposition, err := settleSourceLedgerJobTx(ctx, tx, "default", "source-ledger-stale-generation", tombstone.ID, revision-1, 0, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if disposition != "stale_generation" {
		t.Fatalf("stale generation disposition=%q", disposition)
	}
	var rows int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM memory_revisions WHERE claim_id=? AND revision<?`, tombstone.ID, revision).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("stale job erased current-generation ledger rows: rows=%d", rows)
	}
}

func TestSourceLedgerFutureJobDoesNotEraseOrSettle(t *testing.T) {
	ctx := context.Background()
	store, repo := newMemoryTestRepo(t)
	tombstone, other, revision := seedSourceLedgerTombstone(t, ctx, store.DB(), repo)
	addLegacySourceLedgerRows(t, ctx, store.DB(), tombstone.ID, other.ID, revision-1)
	insertSourceLedgerJob(t, ctx, store.DB(), "source-ledger-future-generation", tombstone.ID, revision+1, "pending", "", "", "", 0)
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	disposition, err := settleSourceLedgerJobTx(ctx, tx, "default", "source-ledger-future-generation", tombstone.ID, revision+1, 0, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := store.DB().QueryRowContext(ctx, `SELECT status FROM memory_jobs WHERE id='source-ledger-future-generation'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if disposition != "deletion_tombstone_mismatch" || status != "blocked" {
		t.Fatalf("future job was accepted: disposition=%q status=%q", disposition, status)
	}
	for _, table := range []string{"memory_revisions", "memory_revision_sources", "memory_terms"} {
		var count int
		query := `SELECT count(*) FROM ` + table + ` WHERE claim_id=? AND revision<?`
		if err := store.DB().QueryRowContext(ctx, query, tombstone.ID, revision).Scan(&count); err != nil || count != 1 {
			t.Errorf("future job changed old %s rows: count=%d err=%v", table, count, err)
		}
	}
}

func TestSourceLedgerMissingOrCorruptTombstoneFailsClosed(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name       string
		corruptSQL string
		missing    bool
	}{
		{name: "missing", missing: true},
		{name: "current revision missing", corruptSQL: `DELETE FROM memory_revisions WHERE claim_id=? AND revision=?`},
		{name: "current revision content", corruptSQL: `UPDATE memory_revisions SET content='not empty' WHERE claim_id=? AND revision=?`},
		{name: "current revision status", corruptSQL: `UPDATE memory_revisions SET status='active' WHERE claim_id=? AND revision=?`},
		{name: "deletion timestamp", corruptSQL: `UPDATE memory_tombstones SET deleted_at='2026-01-01T00:00:00Z' WHERE claim_id=? AND revision=?`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, repo := newMemoryTestRepo(t)
			tombstone, other, revision := seedSourceLedgerTombstone(t, ctx, store.DB(), repo)
			addLegacySourceLedgerRows(t, ctx, store.DB(), tombstone.ID, other.ID, revision-1)
			if tc.missing {
				if _, err := store.DB().ExecContext(ctx, `DELETE FROM memory_tombstones WHERE workspace_id='default' AND claim_id=?`, tombstone.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := store.DB().ExecContext(ctx, tc.corruptSQL, tombstone.ID, revision); err != nil {
					t.Fatal(err)
				}
			}
			insertSourceLedgerJob(t, ctx, store.DB(), "source-ledger-invalid-tombstone", tombstone.ID, revision, "pending", "", "", "", 0)
			tx, err := store.DB().BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			disposition, err := settleSourceLedgerJobTx(ctx, tx, "default", "source-ledger-invalid-tombstone", tombstone.ID, revision, 0, time.Now().UTC().Format(time.RFC3339Nano))
			if err != nil {
				_ = tx.Rollback()
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			var status, lastError string
			if err := store.DB().QueryRowContext(ctx, `SELECT status,last_error_code FROM memory_jobs WHERE id='source-ledger-invalid-tombstone'`).Scan(&status, &lastError); err != nil {
				t.Fatal(err)
			}
			wantError := "deletion_tombstone_mismatch"
			if tc.missing {
				wantError = "deletion_tombstone_missing"
			}
			if status != "blocked" || disposition != wantError || lastError != wantError {
				t.Fatalf("invalid tombstone was not fail-closed: status=%s disposition=%s last_error=%s", status, disposition, lastError)
			}
			for _, table := range []string{"memory_revisions", "memory_revision_sources", "memory_terms"} {
				var count int
				query := `SELECT count(*) FROM ` + table + ` WHERE claim_id=? AND revision<?`
				if err := store.DB().QueryRowContext(ctx, query, tombstone.ID, revision).Scan(&count); err != nil || count != 1 {
					t.Errorf("invalid tombstone cleanup changed old %s rows: count=%d err=%v", table, count, err)
				}
			}
		})
	}
}

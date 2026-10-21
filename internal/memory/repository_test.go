package memory

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/storage"
)

func newMemoryTestRepo(t *testing.T) (*storage.Store, *Repository) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err := RegisterMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return store, NewRepository(store.DB())
}

func TestMemoryChineseVersionConflictAndDeletion(t *testing.T) {
	ctx := context.Background()
	store, repo := newMemoryTestRepo(t)
	principal := contracts.Principal{WorkspaceID: "default", SubjectID: "user-1", Scopes: []string{"memory:read:personal", "memory:write:personal", "memory:delete:personal"}}
	scope := Scope{Kind: ScopePersonal, PrincipalID: principal.SubjectID}

	first := writeMemory(t, ctx, store.DB(), repo, scope, "数据库改为 SQLite，因为本地事务需要稳定恢复", "source-1", 0)
	got, err := repo.Search(ctx, principal, "SQLite 原因", SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Content != first.Content || len(got.Items[0].Sources) != 1 {
		t.Fatalf("Chinese source-linked search returned %#v", got)
	}
	other := principal
	other.SubjectID = "user-2"
	got, err = repo.Search(ctx, other, "SQLite", SearchFilter{})
	if err != nil || len(got.Items) != 0 {
		t.Fatalf("personal memory crossed subject boundary: result=%#v err=%v", got, err)
	}

	updated := writeMemory(t, ctx, store.DB(), repo, scope, "数据库改为 PostgreSQL，理由在来源中另有记录", "source-2", first.Revision)
	if updated.ID != first.ID || updated.Revision != first.Revision+1 || updated.Status != ClaimActive {
		t.Fatalf("confirmed revision did not update in place: %#v", updated)
	}
	if _, err := writeMemoryErr(ctx, store.DB(), repo, scope, "错误的过期覆盖", "source-stale", first.Revision); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale write error=%v, want ErrVersionConflict", err)
	}

	conflict := writeMemory(t, ctx, store.DB(), repo, scope, "数据库是 MySQL", "source-conflict", 0)
	if conflict.Status != ClaimConflicted {
		t.Fatalf("unversioned conflicting write status=%q", conflict.Status)
	}
	if _, err := writeMemoryErr(ctx, store.DB(), repo, scope, "数据库是 MariaDB", "source-third", 0); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("third unversioned value bypassed unresolved conflict: err=%v", err)
	}
	got, err = repo.Search(ctx, principal, "数据库", SearchFilter{})
	if err != nil || len(got.Conflicts) != 1 || len(got.Items) < 2 {
		t.Fatalf("conflict was hidden or overwritten: result=%#v err=%v", got, err)
	}

	// Delete both sides of the conflict. Each tombstone blocks retrieval before
	// its derived cleanup jobs have run.
	var latestRevision int64
	if err := store.DB().QueryRowContext(ctx, `SELECT revision FROM memory_claims WHERE id=?`, first.ID).Scan(&latestRevision); err != nil {
		t.Fatal(err)
	}
	deleted, err := runDelete(ctx, store.DB(), repo, DeleteInput{WorkspaceID: "default", ActorID: "user-1", ClaimID: first.ID, Expected: latestRevision, ReasonCode: "user_requested"})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Status != ClaimTombstoned || deleted.DeletedAt == nil {
		t.Fatalf("delete did not return authoritative tombstone: %#v", deleted)
	}
	if _, err := runDelete(ctx, store.DB(), repo, DeleteInput{WorkspaceID: "default", ActorID: "user-1", ClaimID: conflict.ID, Expected: conflict.Revision, ReasonCode: "user_requested"}); err != nil {
		t.Fatal(err)
	}
	got, err = repo.Search(ctx, principal, "PostgreSQL", SearchFilter{})
	if err != nil || len(got.Items) != 0 {
		t.Fatalf("tombstoned claim remained retrievable: result=%#v err=%v", got, err)
	}
	var oldTextCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_revisions WHERE claim_id=? AND content!=''`, first.ID).Scan(&oldTextCount); err != nil {
		t.Fatal(err)
	}
	if oldTextCount != 0 {
		t.Fatalf("deleted revision content remains in authority: count=%d", oldTextCount)
	}
	var jobs int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_jobs WHERE entity_id=? AND kind='delete_cleanup' AND status='pending'`, first.ID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 6 {
		t.Fatalf("delete cleanup jobs=%d, want 6 targets", jobs)
	}
}

func TestInferredClaimCannotBePromotedAndChineseTokenizerKeepsShortTerms(t *testing.T) {
	c := Claim{ID: "candidate", WorkspaceID: "default", Scope: Scope{Kind: ScopeWorkspace}, Subject: "项目", Key: "数据库", Content: "SQLite", Authority: AuthorityModelInferred, Status: ClaimActive, Revision: 1,
		Sources: []Source{{ID: "source", Version: 1, Scope: Scope{Kind: ScopeWorkspace}, Kind: "event", Reference: "event-1", Producer: "extractor"}}}
	if err := c.ValidateForWrite(); err == nil {
		t.Fatal("model inference was allowed to enter the active authority")
	}
	terms := tokenize("项目 A1 数据库")
	want := map[string]bool{"项": false, "项目": false, "目": false, "数据": false, "据库": false, "a1": false}
	for _, term := range terms {
		if _, ok := want[term]; ok {
			want[term] = true
		}
	}
	for term, seen := range want {
		if !seen {
			t.Errorf("tokenizer omitted %q from %#v", term, terms)
		}
	}
}

func TestContextManifestCapturesVerifiedRecipientVisibleVersions(t *testing.T) {
	ctx := context.Background()
	store, repo := newMemoryTestRepo(t)
	owner := contracts.Principal{WorkspaceID: "default", SubjectID: "user-1", Scopes: []string{"memory:read:personal"}}
	claim := writeMemory(t, ctx, store.DB(), repo, Scope{Kind: ScopePersonal, PrincipalID: owner.SubjectID}, "保留 SQLite 作为本地事务数据库", "context-source", 0)

	manifest, claims, err := repo.AssembleContext(ctx, owner, "", owner.SubjectID, "SQLite", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].ID != claim.ID || len(manifest.Entries) != 1 || manifest.Entries[0].Revision != claim.Revision {
		t.Fatalf("manifest did not capture the visible revision: manifest=%#v claims=%#v", manifest, claims)
	}
	var saved []byte
	if err := store.DB().QueryRowContext(ctx, `SELECT manifest FROM memory_context_manifests WHERE id=?`, manifest.ID).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if len(saved) == 0 {
		t.Fatal("context manifest was not durably recorded")
	}
	other := owner
	other.SubjectID = "user-2"
	manifest, claims, err = repo.AssembleContext(ctx, other, "", other.SubjectID, "SQLite", 100)
	if err != nil || len(claims) != 0 || len(manifest.Entries) != 0 {
		t.Fatalf("context leaked across personal principals: manifest=%#v claims=%#v err=%v", manifest, claims, err)
	}
}

func TestNormalizeTextPreservesOrderAndRepetition(t *testing.T) {
	if normalizeText("Alice trusts Bob") == normalizeText("Bob trusts Alice") {
		t.Fatal("reordered claim was treated as equal")
	}
	if normalizeText("Alice trusts Bob") == normalizeText("Alice trusts Bob Bob") {
		t.Fatal("repeated token was treated as equal")
	}
	if normalizeText("Alice trusts Bob") != normalizeText("ALICE trusts, Bob!") {
		t.Fatal("punctuation/case normalization changed")
	}
}

func TestTaskContextExcludesMemoriesFromOtherProjects(t *testing.T) {
	ctx := context.Background()
	store, repo := newMemoryTestRepo(t)
	_, err := store.DB().ExecContext(ctx, `INSERT INTO projects(id,workspace_id,name,status,version,created_at,updated_at,owner_subject_id) VALUES('project-a','default','A','active',1,'now','now','owner'),('project-b','default','B','active',1,'now','now','other')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.DB().ExecContext(ctx, `INSERT INTO devices(id,workspace_id,subject_id,name,status,token_hash,created_at) VALUES('device-a','default','owner','test','approved',X'01','now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.DB().ExecContext(ctx, `INSERT INTO tasks(id,workspace_id,project_id,title,goal,source,profile_id,status,version,created_at,updated_at,created_by_device_id,command_id,owner_subject_id) VALUES('task-a','default','project-a','Task','Goal','user','default','queued',1,'now','now','device-a','cmd','owner')`)
	if err != nil {
		t.Fatal(err)
	}
	writeMemory(t, ctx, store.DB(), repo, Scope{Kind: ScopeProject, ID: "project-a"}, "sharedword belongs to project A", "project-source-a", 0)
	writeMemory(t, ctx, store.DB(), repo, Scope{Kind: ScopeProject, ID: "project-b"}, "sharedword belongs to project B", "project-source-b", 0)
	owner := contracts.Principal{WorkspaceID: "default", SubjectID: "owner", Scopes: []string{"memory:read:workspace"}}
	projectSearch, err := repo.Search(ctx, owner, "sharedword", SearchFilter{ScopeKind: ScopeProject, ScopeID: "project-b"})
	if err != nil || len(projectSearch.Items) != 0 {
		t.Fatalf("same-workspace project crossed ownership boundary: result=%#v err=%v", projectSearch, err)
	}
	_, claims, err := repo.AssembleContext(ctx, owner, "task-a", owner.SubjectID, "sharedword", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].Scope.ID != "project-a" {
		t.Fatalf("task context crossed project boundary: %#v", claims)
	}
	other := contracts.Principal{WorkspaceID: "default", SubjectID: "other", Scopes: []string{"memory:read:workspace"}}
	_, _, err = repo.AssembleContext(ctx, other, "task-a", other.SubjectID, "sharedword", 1000)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("different subject accessed another subject's task: err=%v", err)
	}
}

func TestMemoryActionGrantsAreExplicitAndScopeBound(t *testing.T) {
	personal := Scope{Kind: ScopePersonal, PrincipalID: "owner"}
	projectA := Scope{Kind: ScopeProject, ID: "project-a"}
	projectB := Scope{Kind: ScopeProject, ID: "project-b"}
	legacy := contracts.Principal{WorkspaceID: "default", SubjectID: "owner", Scopes: []string{"client:command"}}
	if mayWriteScope(legacy, personal) || mayWriteScope(legacy, projectA) || mayDeleteScope(legacy, personal) {
		t.Fatal("generic command scope granted memory mutation")
	}
	narrow := contracts.Principal{WorkspaceID: "default", SubjectID: "owner", Scopes: []string{"memory:write:project:project-a", "memory:read:project:project-a"}}
	if !mayWriteScope(narrow, projectA) || mayWriteScope(narrow, projectB) || !mayReadScope(narrow, projectA) || mayReadScope(narrow, projectB) {
		t.Fatal("project grant escaped its explicit project ID")
	}
	owner := contracts.Principal{WorkspaceID: "default", SubjectID: "owner", Scopes: []string{"memory:read:personal", "memory:write:personal", "memory:delete:personal", "memory:read:workspace", "memory:write:workspace", "memory:delete:workspace"}}
	if !mayWriteScope(owner, personal) || !mayWriteScope(owner, projectA) || !mayDeleteScope(owner, projectA) || !mayReadScope(owner, projectB) {
		t.Fatal("configured owner scopes did not include personal and workspace contexts")
	}
}

func TestProposalApprovalRejectsDeletedEventSource(t *testing.T) {
	ctx := context.Background()
	store, repo := newMemoryTestRepo(t)
	payload := []byte(`{"result":"verified"}`)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO events(event_id,workspace_id,cursor,schema_version,event_type,occurred_at,subject_id,aggregate_type,aggregate_id,aggregate_version,payload) VALUES('source-event','default',1,1,'task.completed','now','owner','task','task-a',1,?)`, payload); err != nil {
		t.Fatal(err)
	}
	scope := Scope{Kind: ScopeWorkspace}
	source := Source{ID: "proposal-source", Version: 1, Scope: scope, Kind: "event", Reference: "event:source-event", ContentHash: hashBytes(payload), RecordedAt: time.Now().UTC(), Producer: "runner"}
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertSourceTx(ctx, tx, "default", source); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	proposal, err := repo.CreateProposal(ctx, contracts.RunnerPrincipal{RunnerID: "runner", WorkspaceIDs: []string{"default"}, Scopes: []string{"memory:propose:workspace"}}, runnerProposalRequest{WorkspaceID: "default", SourceID: source.ID, SourceVersion: 1, ExtractorVersion: "extractor-v1", Scope: scope, Subject: "project", Key: "setting", Content: "verified setting"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM events WHERE event_id='source-event' AND workspace_id='default'`); err != nil {
		t.Fatal(err)
	}
	approveTx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.ApproveProposalTx(ctx, approveTx, contracts.Principal{WorkspaceID: "default", SubjectID: "owner", Scopes: []string{"memory:read:workspace", "memory:write:workspace"}}, "approve-command", proposal.ID, proposal.Version, time.Now().UTC())
	_ = approveTx.Rollback()
	if !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("approval accepted a deleted event source: err=%v", err)
	}
}

func writeMemory(t *testing.T, ctx context.Context, db *sql.DB, repo *Repository, scope Scope, content, sourceID string, expected int64) Claim {
	t.Helper()
	claim, err := writeMemoryErr(ctx, db, repo, scope, content, sourceID, expected)
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

func writeMemoryErr(ctx context.Context, db *sql.DB, repo *Repository, scope Scope, content, sourceID string, expected int64) (Claim, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Claim{}, err
	}
	now := time.Now().UTC()
	actor := scope.PrincipalID
	if actor == "" {
		actor = "user-1"
	}
	source := Source{ID: sourceID, Version: 1, Scope: scope, Kind: "user_note", Reference: "command:" + sourceID, RecordedAt: now, Producer: "user"}
	claim, err := repo.PutTx(ctx, tx, WriteInput{WorkspaceID: "default", ActorID: actor, Expected: expected, Source: source, Claim: Claim{WorkspaceID: "default", Scope: scope, Subject: "project", Key: "database", Content: content, Authority: AuthorityUserConfirmed, Status: ClaimActive, Revision: 1, Sources: []Source{source}}})
	if err != nil {
		_ = tx.Rollback()
		return Claim{}, err
	}
	if err := tx.Commit(); err != nil {
		return Claim{}, err
	}
	return claim, nil
}

func runDelete(ctx context.Context, db *sql.DB, repo *Repository, in DeleteInput) (Claim, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Claim{}, err
	}
	claim, err := repo.DeleteTx(ctx, tx, in)
	if err != nil {
		_ = tx.Rollback()
		return Claim{}, err
	}
	if err := tx.Commit(); err != nil {
		return Claim{}, err
	}
	return claim, nil
}

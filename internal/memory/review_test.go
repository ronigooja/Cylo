package memory

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ronigooja/Zoe/contracts"
	"github.com/ronigooja/Zoe/internal/control"
	"testing"
	"time"
)

func TestReviewEventSourceSearchDoesNotDeadlock(t *testing.T) {
	store, repo := newMemoryTestRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	payload := []byte(`{"fact":"Chinese database decision"}`)
	_, err := store.DB().ExecContext(ctx, `INSERT INTO events(event_id,workspace_id,cursor,schema_version,event_type,occurred_at,aggregate_type,aggregate_id,aggregate_version,payload) VALUES('review-event','default',1,1,'review',?,'workspace','default',1,?)`, now.Format(time.RFC3339Nano), payload)
	if err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "review-source", Version: 1, Scope: Scope{Kind: ScopeWorkspace}, Kind: "event", Reference: "event:review-event", ContentHash: hashBytes(payload), RecordedAt: now, Producer: "user"}
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.PutTx(ctx, tx, WriteInput{WorkspaceID: "default", ActorID: "review-user", Source: source, Claim: Claim{WorkspaceID: "default", Scope: source.Scope, Subject: "database", Key: "decision", Content: "SQLite", Authority: AuthorityUserConfirmed, Status: ClaimActive, Revision: 1, Sources: []Source{source}}})
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	timed, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	result, err := repo.Search(timed, contracts.Principal{WorkspaceID: "default", SubjectID: "review-user", Scopes: []string{"memory:read:workspace"}}, "SQLite", SearchFilter{})
	if err != nil {
		t.Fatalf("valid event-backed retrieval failed: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("valid event-backed result=%#v", result)
	}
}

func TestReviewWordOrderChangesCannotSilentlyOverwrite(t *testing.T) {
	store, repo := newMemoryTestRepo(t)
	ctx := context.Background()
	scope := Scope{Kind: ScopePersonal, PrincipalID: "user-1"}
	first := writeMemory(t, ctx, store.DB(), repo, scope, "Alice trusts Bob", "review-order-1", 0)
	second := writeMemory(t, ctx, store.DB(), repo, scope, "Bob trusts Alice", "review-order-2", 0)
	if second.ID == first.ID || second.Status != ClaimConflicted {
		t.Fatalf("different ordered facts silently treated as equivalent: first=%s second=%#v", first.ID, second)
	}
}

func TestReviewOrdinaryClientCanReadWorkspaceMemory(t *testing.T) {
	store, repo := newMemoryTestRepo(t)
	scope := Scope{Kind: ScopeWorkspace}
	// The production command handler supplies SubjectID for workspace writes.
	tx, err := store.DB().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "review-client-source", Version: 1, Scope: scope, Kind: "user_note", Reference: "command:review", RecordedAt: time.Now().UTC(), Producer: "user"}
	_, err = repo.PutTx(context.Background(), tx, WriteInput{WorkspaceID: "default", ActorID: "user-1", Source: source, Claim: Claim{WorkspaceID: "default", Scope: scope, Subject: "database", Key: "decision", Content: "SQLite", Authority: AuthorityUserConfirmed, Status: ClaimActive, Revision: 1, Sources: []Source{source}}})
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	result, err := repo.Search(context.Background(), contracts.Principal{WorkspaceID: "default", SubjectID: "user-1", Scopes: []string{"client:read", "client:command", "device:manage", "memory:read:personal", "memory:write:personal", "memory:delete:personal", "memory:read:workspace", "memory:write:workspace", "memory:delete:workspace"}}, "SQLite", SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("normal device cannot retrieve workspace memory: %#v", result)
	}
}

func TestReviewExistingConflictCannotBeBypassedByThirdWrite(t *testing.T) {
	store, repo := newMemoryTestRepo(t)
	ctx := context.Background()
	scope := Scope{Kind: ScopePersonal, PrincipalID: "user-1"}
	writeMemory(t, ctx, store.DB(), repo, scope, "SQLite", "review-conflict-1", 0)
	writeMemory(t, ctx, store.DB(), repo, scope, "PostgreSQL", "review-conflict-2", 0)
	third, err := writeMemoryErr(ctx, store.DB(), repo, scope, "MySQL", "review-conflict-3", 0)
	if err == nil && third.Status != ClaimConflicted {
		t.Fatalf("unresolved conflict bypassed: third=%#v", third)
	}
	if err != nil && !errors.Is(err, ErrVersionConflict) {
		t.Fatal(err)
	}
}

func TestReviewProposalApprovalRevalidatesUnderlyingSource(t *testing.T) {
	store, repo := newMemoryTestRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	payload := []byte(`{"fact":"SQLite"}`)
	_, err := store.DB().ExecContext(ctx, `INSERT INTO events(event_id,workspace_id,cursor,schema_version,event_type,occurred_at,aggregate_type,aggregate_id,aggregate_version,payload) VALUES('review-old-event','default',1,1,'review',?,'workspace','default',1,?)`, now.Format(time.RFC3339Nano), payload)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{Kind: ScopeWorkspace}
	source := Source{ID: "review-expired-source", Version: 1, Scope: scope, Kind: "event", Reference: "event:review-old-event", ContentHash: hashBytes(payload), RecordedAt: now, Producer: "user"}
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = insertSourceTx(ctx, tx, "default", source); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	proposal, err := repo.CreateProposal(ctx, contracts.RunnerPrincipal{WorkspaceIDs: []string{"default"}}, runnerProposalRequest{WorkspaceID: "default", SourceID: source.ID, SourceVersion: 1, ExtractorVersion: "review-v1", Scope: scope, Subject: "database", Key: "decision", Content: "SQLite"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `DELETE FROM events WHERE event_id='review-old-event'`); err != nil {
		t.Fatal(err)
	}
	tx, err = store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	_, err = repo.ApproveProposalTx(ctx, tx, contracts.Principal{WorkspaceID: "default", SubjectID: "user-1", Scopes: []string{"memory:write:workspace", "memory:read:workspace"}}, "review-approve", proposal.ID, proposal.Version, now)
	if err == nil {
		t.Fatal("proposal approval accepted deleted underlying event as authoritative source")
	}
}

func TestReviewTaskContextExcludesUnrelatedProjectMemory(t *testing.T) {
	store, repo := newMemoryTestRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []string{
		`INSERT INTO actors(id,workspace_id,subject_id,name,status,created_at) VALUES('review-device','default','user-1','review','approved',?)`,
		`INSERT INTO projects(id,workspace_id,name,status,version,created_at,updated_at,owner_subject_id) VALUES('review-project-a','default','A','active',1,?,?,'user-1')`,
		`INSERT INTO projects(id,workspace_id,name,status,version,created_at,updated_at,owner_subject_id) VALUES('review-project-b','default','B','active',1,?,?,'user-1')`,
		`INSERT INTO tasks(id,workspace_id,project_id,title,goal,source,profile_id,status,version,created_at,updated_at,created_by_device_id,command_id,owner_subject_id) VALUES('review-task-a','default','review-project-a','task','goal','text','profile','queued',1,?,?,'review-device','command','user-1')`,
	}
	for i, statement := range statements {
		args := []any{now, now}
		if i == 0 {
			args = []any{now}
		}
		if _, err := store.DB().ExecContext(ctx, statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	// The owner helper intentionally requires ActorID; project scopes use the generic writer below.
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{Kind: ScopeProject, ID: "review-project-b"}
	source := Source{ID: "review-other-project-source", Version: 1, Scope: scope, Kind: "user_note", Reference: "command:other", RecordedAt: time.Now().UTC(), Producer: "user"}
	_, err = repo.PutTx(ctx, tx, WriteInput{WorkspaceID: "default", ActorID: "user-1", Source: source, Claim: Claim{WorkspaceID: "default", Scope: scope, Subject: "database", Key: "decision", Content: "private B SQLite", Authority: AuthorityUserConfirmed, Status: ClaimActive, Revision: 1, Sources: []Source{source}}})
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	_, claims, err := repo.AssembleContext(ctx, contracts.Principal{WorkspaceID: "default", SubjectID: "user-1", Scopes: []string{"memory:read:workspace"}}, "review-task-a", "user-1", "SQLite", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 0 {
		t.Fatalf("task A context included unrelated project B: %#v", claims)
	}
}

func TestReviewMemoryCommandRoundtripScopesAndTombstone(t *testing.T) {
	store, repo := newMemoryTestRepo(t)
	ctx := context.Background()
	svc := &Service{db: store.DB(), repo: repo}
	p := contracts.Principal{WorkspaceID: "default", SubjectID: "review-owner", Scopes: []string{"memory:write:workspace", "memory:read:workspace", "memory:delete:workspace"}}
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := svc.commandRemember(ctx, tx, control.CommandContext{Principal: p, CommandID: "review-write", AcceptedAt: time.Now().UTC()}, contracts.CommandRequest{Payload: json.RawMessage(`{"scope":{"kind":"workspace"},"subject":"数据库","key":"决定","content":"选择 SQLite 支持本地恢复","source_kind":"user_note"}`)})
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	found, err := repo.Search(ctx, p, "本地恢复", SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Items) != 1 || found.Items[0].ID != result.ResourceID || found.Items[0].Authority != AuthorityUserConfirmed || found.Items[0].Sources[0].Reference != "command:review-write" {
		t.Fatalf("roundtrip lost authority/source: %#v", found)
	}
	ungranted := p
	ungranted.Scopes = []string{"client:read", "client:command"}
	if _, err = repo.Search(ctx, ungranted, "SQLite", SearchFilter{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("client scope implicitly granted memory: %v", err)
	}
	tx, err = store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.commandRemember(ctx, tx, control.CommandContext{Principal: p, CommandID: "review-forge", AcceptedAt: time.Now().UTC()}, contracts.CommandRequest{Payload: json.RawMessage(`{"scope":{"kind":"workspace"},"subject":"db","key":"d","content":"fake","source_kind":"user_note","authority_kind":"execution_observed"}`)})
	tx.Rollback()
	if err == nil {
		t.Fatal("client controlled authority accepted")
	}
	tx, err = store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(deletePayload{MemoryID: result.ResourceID, ExpectedRevision: result.ResourceVersion, Reason: "user_requested"})
	_, _, err = svc.commandDelete(ctx, tx, control.CommandContext{Principal: p, CommandID: "review-delete", AcceptedAt: time.Now().UTC()}, contracts.CommandRequest{Payload: body})
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	_, claims, err := repo.AssembleContext(ctx, p, "", p.SubjectID, "SQLite", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 0 {
		t.Fatalf("deleted claim disclosed before cleanup: %#v", claims)
	}
}

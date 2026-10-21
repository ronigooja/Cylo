package integration

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/memory"
	"github.com/ronigooja/Cylo/internal/storage"
)

func memoryWorkerHarness(t *testing.T, scopes, workspaces []string) (*storage.Store, http.Handler, string) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "memory-worker.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	server, err := control.NewServer(store, control.Config{RunnerToken: token, RunnerID: "memory-worker", RunnerWorkspaceIDs: workspaces, RunnerScopes: scopes})
	if err != nil {
		t.Fatal(err)
	}
	if err := memory.Register(server, store); err != nil {
		t.Fatal(err)
	}
	if err := memory.RegisterDerived(server, store, memory.DerivedConfig{LeaseDuration: time.Second, MaxAttempts: 3, MaxBatch: 10}); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return store, server.Handler(), token
}

func memoryWorkerRequest(t *testing.T, handler http.Handler, token, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(data)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func seedMemoryWorkerClaim(t *testing.T, db *sql.DB, workspace, claimID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	sourceID := "source-" + claimID
	if workspace != "default" {
		if _, err := db.ExecContext(ctx, `INSERT INTO workspaces(id,name,created_at) VALUES(?,?,?)`, workspace, workspace, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_sources(workspace_id,id,version,scope_kind,scope_id,principal_id,kind,reference,content_hash,recorded_at,producer) VALUES(?,?,1,'workspace','','','user_note','user_note','',?,'user')`, workspace, sourceID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_claims(id,workspace_id,scope_kind,scope_id,principal_id,subject,claim_key,authority_kind,status,revision,created_at,updated_at) VALUES(?,?,'workspace','','','project','database','user_confirmed','active',1,?,?)`, claimID, workspace, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_revisions(claim_id,revision,content,authority_kind,status,recorded_at,actor_id) VALUES(?,1,'SQLite keeps writes local','user_confirmed','active',?,'user')`, claimID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_revision_sources(claim_id,revision,workspace_id,source_id,source_version) VALUES(?,1,?,?,1)`, claimID, workspace, sourceID); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryWorkerLeaseFenceAndIdempotentCompletion(t *testing.T) {
	store, handler, token := memoryWorkerHarness(t, []string{"memory:jobs:claim", "memory:jobs:complete"}, []string{"default"})
	seedMemoryWorkerClaim(t, store.DB(), "default", "claim-1")
	first := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/claim", map[string]any{"limit": 1})
	if first.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", first.Code, first.Body.String())
	}
	var response struct {
		Items []struct {
			ID      string `json:"id"`
			Kind    string `json:"kind"`
			Attempt int    `json:"attempt"`
			Claim   struct {
				Revision int64  `json:"revision"`
				Content  string `json:"content"`
			} `json:"claim"`
		} `json:"items"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 1 || response.Items[0].Kind != "index" || response.Items[0].Attempt != 1 || response.Items[0].Claim.Revision != 1 || response.Items[0].Claim.Content != "SQLite keeps writes local" {
		t.Fatalf("unexpected claimed job: %#v", response.Items)
	}
	job := response.Items[0]
	validated := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/"+job.ID+"/validate", map[string]any{"attempt": 1})
	if validated.Code != http.StatusOK {
		t.Fatalf("current lease validation status=%d body=%s", validated.Code, validated.Body.String())
	}
	var validation struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.Unmarshal(validated.Body.Bytes(), &validation); err != nil {
		t.Fatal(err)
	}
	if !validation.Allowed {
		t.Fatalf("current claimed job was not authorized for its derived effect: %s", validated.Body.String())
	}
	if _, err := store.DB().Exec(`UPDATE memory_jobs SET lease_until=? WHERE id=?`, time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano), job.ID); err != nil {
		t.Fatal(err)
	}
	second := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/claim", map[string]any{"limit": 1})
	if second.Code != http.StatusOK {
		t.Fatalf("reclaim status=%d body=%s", second.Code, second.Body.String())
	}
	var reclaimed struct {
		Items []struct {
			ID      string `json:"id"`
			Attempt int    `json:"attempt"`
		} `json:"items"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &reclaimed); err != nil {
		t.Fatal(err)
	}
	if len(reclaimed.Items) != 1 || reclaimed.Items[0].ID != job.ID || reclaimed.Items[0].Attempt != 2 {
		t.Fatalf("lease was not fenced on reclaim: %#v", reclaimed.Items)
	}
	staleValidation := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/"+job.ID+"/validate", map[string]any{"attempt": 1})
	if staleValidation.Code != http.StatusConflict {
		t.Fatalf("stale lease validation status=%d body=%s", staleValidation.Code, staleValidation.Body.String())
	}
	stale := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/"+job.ID+"/complete", map[string]any{"attempt": 1})
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale attempt status=%d body=%s", stale.Code, stale.Body.String())
	}
	complete := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/"+job.ID+"/complete", map[string]any{"attempt": 2})
	if complete.Code != http.StatusOK {
		t.Fatalf("complete status=%d body=%s", complete.Code, complete.Body.String())
	}
	duplicate := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/"+job.ID+"/complete", map[string]any{"attempt": 2})
	if duplicate.Code != http.StatusOK {
		t.Fatalf("idempotent completion status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
}

func TestMemoryWorkerRequiresExactScopeAndWorkspace(t *testing.T) {
	store, handler, token := memoryWorkerHarness(t, []string{"*"}, []string{"default", "*"})
	seedMemoryWorkerClaim(t, store.DB(), "default", "claim-default")
	seedMemoryWorkerClaim(t, store.DB(), "other", "claim-other")
	response := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/claim", map[string]any{"limit": 10})
	if response.Code != http.StatusForbidden {
		t.Fatalf("wildcard grants status=%d body=%s", response.Code, response.Body.String())
	}

	store2, handler2, token2 := memoryWorkerHarness(t, []string{"memory:jobs:claim", "memory:jobs:complete"}, []string{"default"})
	seedMemoryWorkerClaim(t, store2.DB(), "default", "claim-default")
	seedMemoryWorkerClaim(t, store2.DB(), "other", "claim-other")
	response = memoryWorkerRequest(t, handler2, token2, "/runner/v1/memory/jobs/claim", map[string]any{"limit": 10})
	if response.Code != http.StatusOK {
		t.Fatalf("scoped claim status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Items []struct {
			WorkspaceID string `json:"workspace_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for _, item := range result.Items {
		if item.WorkspaceID != "default" {
			t.Fatalf("job escaped workspace allowlist: %#v", item)
		}
	}
}

func TestMemoryWorkerProposalRemainsPendingForUserReview(t *testing.T) {
	store, handler, token := memoryWorkerHarness(t, []string{"memory:propose"}, []string{"default"})
	seedMemoryWorkerClaim(t, store.DB(), "default", "claim-proposal")
	response := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/proposals", map[string]any{
		"workspace_id":      "default",
		"source_id":         "source-claim-proposal",
		"source_version":    1,
		"extractor_version": "mem0-controlled-v1",
		"scope":             map[string]any{"kind": "workspace"},
		"subject":           "project",
		"key":               "derived_fact",
		"content":           "A candidate extracted from the source",
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("proposal status=%d body=%s", response.Code, response.Body.String())
	}
	var proposal struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &proposal); err != nil {
		t.Fatal(err)
	}
	if proposal.ID == "" || proposal.Status != "pending" {
		t.Fatalf("worker proposal was not left pending: %#v", proposal)
	}
	var count int
	if err := store.DB().QueryRow(`SELECT count(*) FROM memory_claims WHERE id='claim-proposal' AND revision=1 AND status='active'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("candidate proposal changed authoritative claims: count=%d", count)
	}
}

func TestMemoryWorkerCompletionRechecksTombstoneGeneration(t *testing.T) {
	store, handler, token := memoryWorkerHarness(t, []string{"memory:jobs:claim", "memory:jobs:complete"}, []string{"default"})
	seedMemoryWorkerClaim(t, store.DB(), "default", "claim-delete-race")
	claim := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/claim", map[string]any{"limit": 1})
	var response struct {
		Items []struct {
			ID      string `json:"id"`
			Attempt int    `json:"attempt"`
		} `json:"items"`
	}
	if err := json.Unmarshal(claim.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 1 {
		t.Fatalf("expected one index job, got %s", claim.Body.String())
	}
	job := response.Items[0]
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().Exec(`UPDATE memory_claims SET status='tombstoned',revision=2,deleted_at=?,updated_at=? WHERE id=?`, now, now, "claim-delete-race"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO memory_revisions(claim_id,revision,content,authority_kind,status,recorded_at,actor_id) VALUES(?,2,'','user_confirmed','tombstoned',?,'user')`, "claim-delete-race", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO memory_tombstones(claim_id,workspace_id,revision,actor_id,reason,deleted_at) VALUES(?, 'default', 2, 'user', 'user_requested', ?)`, "claim-delete-race", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE memory_jobs SET status='done' WHERE workspace_id='default' AND kind='delete_cleanup' AND entity_id='claim-delete-race' AND entity_version=2 AND target='qdrant'`); err != nil {
		t.Fatal(err)
	}
	completed := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/"+job.ID+"/complete", map[string]any{"attempt": job.Attempt})
	if completed.Code != http.StatusOK {
		t.Fatalf("completion status=%d body=%s", completed.Code, completed.Body.String())
	}
	var result struct {
		Status      string `json:"status"`
		Disposition string `json:"disposition"`
	}
	if err := json.Unmarshal(completed.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "done" || result.Disposition != "stale_generation" {
		t.Fatalf("stale index job was acknowledged without a generation result: %#v", result)
	}
	var cleanupStatus string
	if err := store.DB().QueryRow(`SELECT status FROM memory_jobs WHERE workspace_id='default' AND kind='delete_cleanup' AND entity_id='claim-delete-race' AND entity_version=2 AND target='qdrant'`).Scan(&cleanupStatus); err != nil {
		t.Fatal(err)
	}
	if cleanupStatus != "pending" {
		t.Fatalf("stale index effect did not reschedule tombstone cleanup: status=%q", cleanupStatus)
	}
}

func TestMemoryWorkerStaleIndexCompletionRepairsCurrentRevision(t *testing.T) {
	store, handler, token := memoryWorkerHarness(t, []string{"memory:jobs:claim", "memory:jobs:complete"}, []string{"default"})
	seedMemoryWorkerClaim(t, store.DB(), "default", "claim-revision-race")
	claim := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/claim", map[string]any{"limit": 1})
	var response struct {
		Items []struct {
			ID      string `json:"id"`
			Attempt int    `json:"attempt"`
		} `json:"items"`
	}
	if err := json.Unmarshal(claim.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 1 {
		t.Fatalf("expected one index job, got %s", claim.Body.String())
	}
	job := response.Items[0]
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().Exec(`UPDATE memory_claims SET revision=2,updated_at=? WHERE workspace_id='default' AND id='claim-revision-race'`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO memory_revisions(claim_id,revision,content,authority_kind,status,recorded_at,actor_id) VALUES('claim-revision-race',2,'New revision content','user_confirmed','active',?,'user')`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO memory_revision_sources(claim_id,revision,workspace_id,source_id,source_version) VALUES('claim-revision-race',2,'default','source-claim-revision-race',1)`); err != nil {
		t.Fatal(err)
	}
	currentJobID := "current-revision-job"
	if _, err := store.DB().Exec(`INSERT INTO memory_jobs(id,workspace_id,kind,entity_id,entity_version,target,payload,status,attempts,available_at,lease_owner,lease_until,created_at,updated_at) VALUES(?,'default','index','claim-revision-race',2,'qdrant','{}','leased',1,?,'memory-worker',?,?,?)`, currentJobID, now, time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano), now, now); err != nil {
		t.Fatal(err)
	}
	completed := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/"+job.ID+"/complete", map[string]any{"attempt": job.Attempt})
	if completed.Code != http.StatusOK {
		t.Fatalf("completion status=%d body=%s", completed.Code, completed.Body.String())
	}
	var repaired int
	if err := store.DB().QueryRow(`SELECT count(*) FROM memory_jobs WHERE workspace_id='default' AND kind='index' AND entity_id='claim-revision-race' AND entity_version=2 AND target='qdrant' AND status='pending'`).Scan(&repaired); err != nil {
		t.Fatal(err)
	}
	if repaired != 1 {
		t.Fatalf("stale index effect did not schedule current revision repair: pending=%d", repaired)
	}
	late := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/"+currentJobID+"/complete", map[string]any{"attempt": 1})
	if late.Code != http.StatusNotFound && late.Code != http.StatusConflict {
		t.Fatalf("repaired job accepted its prior lease completion: status=%d body=%s", late.Code, late.Body.String())
	}
}

func TestMemoryWorkerReconciliationAdvancesBeyondFirstPage(t *testing.T) {
	store, handler, token := memoryWorkerHarness(t, []string{"memory:jobs:claim", "memory:jobs:complete"}, []string{"default"})
	for i := 0; i < 501; i++ {
		seedMemoryWorkerClaim(t, store.DB(), "default", fmt.Sprintf("claim-page-%03d", i))
	}
	completed := 0
	for poll := 0; poll < 80 && completed < 501; poll++ {
		claimed := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/claim", map[string]any{"limit": 10})
		if claimed.Code != http.StatusOK {
			t.Fatalf("poll %d claim status=%d body=%s", poll, claimed.Code, claimed.Body.String())
		}
		var batch struct {
			Items []struct {
				ID      string `json:"id"`
				Attempt int    `json:"attempt"`
			} `json:"items"`
		}
		if err := json.Unmarshal(claimed.Body.Bytes(), &batch); err != nil {
			t.Fatal(err)
		}
		if len(batch.Items) == 0 {
			t.Fatalf("poll %d returned no jobs after completing %d of 501", poll, completed)
		}
		for _, job := range batch.Items {
			result := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/"+job.ID+"/complete", map[string]any{"attempt": job.Attempt})
			if result.Code != http.StatusOK {
				t.Fatalf("complete %s status=%d body=%s", job.ID, result.Code, result.Body.String())
			}
			completed++
		}
	}
	if completed != 501 {
		t.Fatalf("completed %d of 501 jobs", completed)
	}
}

func TestMemoryWorkerReconcilesMoreThan500ExtractionSources(t *testing.T) {
	store, handler, token := memoryWorkerHarness(t, []string{"memory:jobs:claim", "memory:jobs:complete"}, []string{"default"})
	seedMemoryWorkerClaim(t, store.DB(), "default", "claim-many-sources")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := 0; i < 501; i++ {
		sourceID := fmt.Sprintf("event-source-%03d", i)
		if _, err := store.DB().Exec(`INSERT INTO memory_sources(workspace_id,id,version,scope_kind,scope_id,principal_id,kind,reference,content_hash,recorded_at,producer) VALUES('default',?,1,'workspace','','','event',?,?,?,'runner')`, sourceID, "event:"+sourceID, "", now); err != nil {
			t.Fatal(err)
		}
		if _, err := store.DB().Exec(`INSERT INTO memory_revision_sources(claim_id,revision,workspace_id,source_id,source_version) VALUES('claim-many-sources',1,'default',?,1)`, sourceID); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		claimed := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/claim", map[string]any{"limit": 1})
		if claimed.Code != http.StatusOK {
			t.Fatalf("poll %d claim status=%d body=%s", i, claimed.Code, claimed.Body.String())
		}
	}
	var count int
	if err := store.DB().QueryRow(`SELECT count(*) FROM memory_jobs WHERE workspace_id='default' AND kind='extract' AND entity_id='claim-many-sources'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 501 {
		t.Fatalf("only %d of 501 extraction source jobs were reconciled", count)
	}
}

package integration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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
	return memoryWorkerHarnessWithModelBudget(t, scopes, workspaces, time.Second, 4, 192_000)
}

func memoryWorkerHarnessWithLease(t *testing.T, scopes, workspaces []string, leaseDuration time.Duration) (*storage.Store, http.Handler, string) {
	return memoryWorkerHarnessWithModelBudget(t, scopes, workspaces, leaseDuration, 4, 192_000)
}

func memoryWorkerHarnessWithModelBudget(t *testing.T, scopes, workspaces []string, leaseDuration time.Duration, callLimit, tokenBudget int) (*storage.Store, http.Handler, string) {
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
	if err := memory.RegisterDerived(server, store, memory.DerivedConfig{LeaseDuration: leaseDuration, MaxAttempts: 3, MaxBatch: 10, ModelCallLimit: callLimit, ModelTokenBudget: tokenBudget}); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterRunnerCredential(ctx, token, "memory-worker", workspaces, scopes); err != nil {
		t.Fatal(err)
	}
	return store, server.Handler(), token
}

func seedLeasedMemoryExtractJob(t *testing.T, db *sql.DB, claimID, jobID string) {
	t.Helper()
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	payload := []byte(`{"source":"synthetic event fixture"}`)
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	if _, err := db.Exec(`INSERT INTO events(event_id,workspace_id,cursor,schema_version,event_type,occurred_at,aggregate_type,aggregate_id,aggregate_version,payload) VALUES('event-`+claimID+`','default',1,1,'fixture',?,'claim',?,1,?)`, nowText, claimID, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE memory_sources SET kind='event',reference=?,content_hash=? WHERE workspace_id='default' AND id=?`, "event:event-"+claimID, hash, "source-"+claimID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO memory_jobs(id,workspace_id,kind,entity_id,entity_version,target,payload,status,attempts,available_at,lease_owner,lease_until,created_at,updated_at) VALUES(?,'default','extract',?,1,?,?,'leased',1,?,'memory-worker',?,?,?)`, jobID, claimID, "source-"+claimID, []byte(`{}`), nowText, now.Add(time.Minute).Format(time.RFC3339Nano), nowText, nowText); err != nil {
		t.Fatal(err)
	}
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

func TestDeletionOnlyWorkerRoutesAreAuthenticatedAndTombstoneOnlyBehindRestoreGate(t *testing.T) {
	store, handler, token := memoryWorkerHarness(t, []string{"memory:jobs:claim", "memory:jobs:complete"}, []string{"default"})
	if err := control.RegisterInstanceMaintenanceMigration(store); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().Exec(`UPDATE cylo_instance_state SET restore_deletion_reconciliation_required=1,derived_retrieval_enabled=0 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	seedMemoryWorkerClaim(t, store.DB(), "default", "tombstoned-claim")
	if _, err := store.DB().Exec(`UPDATE memory_claims SET status='tombstoned',deleted_at=?,revision=2 WHERE id='tombstoned-claim'`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO memory_tombstones(claim_id,workspace_id,revision,actor_id,reason,deleted_at) VALUES('tombstoned-claim','default',2,'owner','user_requested',?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO memory_jobs(id,workspace_id,kind,entity_id,entity_version,target,payload,status,attempts,available_at,created_at,updated_at) VALUES('delete-qdrant','default','delete_cleanup','tombstoned-claim',2,'qdrant','{}','pending',0,?,?,?)`, now, now, now); err != nil {
		t.Fatal(err)
	}
	if response := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/claim", map[string]any{"limit": 1}); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("ordinary worker route status=%d body=%s; restore gate must remain closed", response.Code, response.Body.String())
	}
	if response := memoryWorkerRequest(t, handler, "invalid-token", "/runner/v1/memory/deletion-jobs/claim", map[string]any{"limit": 10}); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated deletion-only route status=%d body=%s", response.Code, response.Body.String())
	}
	response := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/deletion-jobs/claim", map[string]any{"limit": 10})
	if response.Code != http.StatusOK {
		t.Fatalf("deletion-only claim status=%d body=%s", response.Code, response.Body.String())
	}
	var claimed struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &claimed); err != nil {
		t.Fatal(err)
	}
	if len(claimed.Items) != 1 || claimed.Items[0]["kind"] != "delete_cleanup" || claimed.Items[0]["target"] != "qdrant" || claimed.Items[0]["claim"] != nil || claimed.Items[0]["source"] != nil {
		t.Fatalf("deletion-only route returned more than a cleanup envelope: %#v", claimed.Items)
	}
	validate := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/deletion-jobs/delete-qdrant/validate", map[string]any{"attempt": 1})
	if validate.Code != http.StatusOK || !strings.Contains(validate.Body.String(), `"allowed":true`) {
		t.Fatalf("tombstone-backed cleanup validation status=%d body=%s", validate.Code, validate.Body.String())
	}
	complete := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/deletion-jobs/delete-qdrant/complete", map[string]any{"attempt": 1})
	if complete.Code != http.StatusOK {
		t.Fatalf("deletion-only completion status=%d body=%s", complete.Code, complete.Body.String())
	}
	var gate int
	if err := store.DB().QueryRow(`SELECT restore_deletion_reconciliation_required FROM cylo_instance_state WHERE singleton=1`).Scan(&gate); err != nil {
		t.Fatal(err)
	}
	if gate != 1 {
		t.Fatalf("worker completion unexpectedly opened restore gate: %d", gate)
	}
}

func TestMemoryWorkerModelCallLedgerReservesBeforeDispatchAndAccountsUnknownSafely(t *testing.T) {
	store, handler, token := memoryWorkerHarnessWithModelBudget(t, []string{"memory:jobs:complete"}, []string{"default"}, time.Minute, 1, 500)
	seedMemoryWorkerClaim(t, store.DB(), "default", "claim-model-ledger")
	seedLeasedMemoryExtractJob(t, store.DB(), "claim-model-ledger", "job-model-ledger")
	reservePath := "/runner/v1/memory/jobs/job-model-ledger/model-calls/reserve"
	reserveBody := map[string]any{
		"attempt": 1, "request_key": "144c5d13-84d4-4e08-a15b-78785f4c21c1",
		"provider": "openai_responses", "model": "gpt-6-luna",
		"estimated_input_tokens": 150, "max_output_tokens": 150,
	}
	first := memoryWorkerRequest(t, handler, token, reservePath, reserveBody)
	if first.Code != http.StatusOK {
		t.Fatalf("model call reserve status=%d body=%s", first.Code, first.Body.String())
	}
	var reservation struct {
		CallID          string `json:"call_id"`
		DispatchAllowed bool   `json:"dispatch_allowed"`
		ReservedTokens  int    `json:"reserved_tokens"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &reservation); err != nil {
		t.Fatal(err)
	}
	if reservation.CallID == "" || !reservation.DispatchAllowed || reservation.ReservedTokens != 300 {
		t.Fatalf("unexpected first model call reservation: %#v", reservation)
	}
	// A lost reserve response can be retried by key, but only the first
	// response grants permission to dispatch a physical provider request.
	replay := memoryWorkerRequest(t, handler, token, reservePath, reserveBody)
	if replay.Code != http.StatusOK {
		t.Fatalf("model call reserve replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	var replayed struct {
		CallID          string `json:"call_id"`
		DispatchAllowed bool   `json:"dispatch_allowed"`
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &replayed); err != nil {
		t.Fatal(err)
	}
	if replayed.CallID != reservation.CallID || replayed.DispatchAllowed {
		t.Fatalf("reserve replay authorized duplicate dispatch: %#v", replayed)
	}
	conflictingReplay := map[string]any{}
	for key, value := range reserveBody {
		conflictingReplay[key] = value
	}
	conflictingReplay["model"] = "different-model"
	conflict := memoryWorkerRequest(t, handler, token, reservePath, conflictingReplay)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflicting reserve replay status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	secondBody := map[string]any{
		"attempt": 1, "request_key": "72c7e4a9-426c-46dd-8d74-94222999f4db",
		"provider": "openai_responses", "model": "gpt-6-luna",
		"estimated_input_tokens": 100, "max_output_tokens": 100,
	}
	second := memoryWorkerRequest(t, handler, token, reservePath, secondBody)
	if second.Code != http.StatusConflict {
		t.Fatalf("second physical call escaped job call budget: status=%d body=%s", second.Code, second.Body.String())
	}
	var calls, reserved int
	if err := store.DB().QueryRow(`SELECT count(*),coalesce(sum(reserved_tokens),0) FROM memory_model_calls WHERE job_id='job-model-ledger'`).Scan(&calls, &reserved); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || reserved != 300 {
		t.Fatalf("reservation accounting changed after replay or denied call: calls=%d reserved=%d", calls, reserved)
	}
	if _, err := store.DB().Exec(`UPDATE memory_jobs SET attempts=2,lease_until=? WHERE id='job-model-ledger'`, time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	retryReservation := map[string]any{
		"attempt": 2, "request_key": "b311dc73-d21d-4745-99d1-4e165ebdf233",
		"provider": "openai_responses", "model": "gpt-6-luna",
		"estimated_input_tokens": 100, "max_output_tokens": 100,
	}
	retryCall := memoryWorkerRequest(t, handler, token, reservePath, retryReservation)
	if retryCall.Code != http.StatusConflict {
		t.Fatalf("job retry escaped the durable call budget: status=%d body=%s", retryCall.Code, retryCall.Body.String())
	}
	if _, err := store.DB().Exec(`UPDATE memory_sources SET deleted_at=? WHERE workspace_id='default' AND id=?`, time.Now().UTC().Format(time.RFC3339Nano), "source-claim-model-ledger"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE memory_jobs SET attempts=3,lease_until=? WHERE id='job-model-ledger'`, time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	staleSource := map[string]any{
		"attempt": 3, "request_key": "9f8f2c84-5684-4454-97d6-19dc7bd0fa2e",
		"provider": "openai_responses", "model": "gpt-6-luna",
		"estimated_input_tokens": 20, "max_output_tokens": 20,
	}
	staleSourceCall := memoryWorkerRequest(t, handler, token, reservePath, staleSource)
	if staleSourceCall.Code != http.StatusConflict {
		t.Fatalf("model call reserved from a deleted source: status=%d body=%s", staleSourceCall.Code, staleSourceCall.Body.String())
	}

	// Finalization is accounting-only and can land after lease expiry or a
	// source tombstone; it cannot grant another dispatch.
	if _, err := store.DB().Exec(`UPDATE memory_jobs SET lease_until=? WHERE id='job-model-ledger'`, time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE memory_sources SET deleted_at=? WHERE workspace_id='default' AND id=?`, time.Now().UTC().Format(time.RFC3339Nano), "source-claim-model-ledger"); err != nil {
		t.Fatal(err)
	}
	finalizePath := "/runner/v1/memory/jobs/job-model-ledger/model-calls/" + reservation.CallID + "/finalize"
	finalizeBody := map[string]any{"attempt": 1, "outcome": "succeeded", "usage_status": "unknown", "input_tokens": nil, "output_tokens": nil}
	finalized := memoryWorkerRequest(t, handler, token, finalizePath, finalizeBody)
	if finalized.Code != http.StatusOK {
		t.Fatalf("unknown usage finalization status=%d body=%s", finalized.Code, finalized.Body.String())
	}
	duplicateFinalize := memoryWorkerRequest(t, handler, token, finalizePath, finalizeBody)
	if duplicateFinalize.Code != http.StatusOK {
		t.Fatalf("idempotent finalization status=%d body=%s", duplicateFinalize.Code, duplicateFinalize.Body.String())
	}
	conflictingFinalizeBody := map[string]any{"attempt": 1, "outcome": "succeeded", "usage_status": "measured", "input_tokens": 0, "output_tokens": 0}
	conflictingFinalize := memoryWorkerRequest(t, handler, token, finalizePath, conflictingFinalizeBody)
	if conflictingFinalize.Code != http.StatusConflict {
		t.Fatalf("conflicting finalization status=%d body=%s", conflictingFinalize.Code, conflictingFinalize.Body.String())
	}
	var usageStatus string
	var inputTokens, outputTokens sql.NullInt64
	if err := store.DB().QueryRow(`SELECT usage_status,input_tokens,output_tokens FROM memory_model_calls WHERE id=?`, reservation.CallID).Scan(&usageStatus, &inputTokens, &outputTokens); err != nil {
		t.Fatal(err)
	}
	if usageStatus != "unknown" || inputTokens.Valid || outputTokens.Valid {
		t.Fatalf("missing provider usage was represented as measured zero: status=%q input=%v output=%v", usageStatus, inputTokens, outputTokens)
	}
}

func TestMemoryWorkerModelCallMeasuredUsageAdjustsLaterBudget(t *testing.T) {
	store, handler, token := memoryWorkerHarnessWithModelBudget(t, []string{"memory:jobs:complete"}, []string{"default"}, time.Minute, 4, 500)
	seedMemoryWorkerClaim(t, store.DB(), "default", "claim-model-measured")
	seedLeasedMemoryExtractJob(t, store.DB(), "claim-model-measured", "job-model-measured")
	reservePath := "/runner/v1/memory/jobs/job-model-measured/model-calls/reserve"
	reserved := memoryWorkerRequest(t, handler, token, reservePath, map[string]any{
		"attempt": 1, "request_key": "22c9a11a-9f1c-4f05-8452-ea79d00d9a41",
		"provider": "openai_responses", "model": "gpt-6-luna",
		"estimated_input_tokens": 100, "max_output_tokens": 100,
	})
	if reserved.Code != http.StatusOK {
		t.Fatalf("reserve status=%d body=%s", reserved.Code, reserved.Body.String())
	}
	var call struct {
		CallID string `json:"call_id"`
	}
	if err := json.Unmarshal(reserved.Body.Bytes(), &call); err != nil {
		t.Fatal(err)
	}
	finalized := memoryWorkerRequest(t, handler, token, "/runner/v1/memory/jobs/job-model-measured/model-calls/"+call.CallID+"/finalize", map[string]any{
		"attempt": 1, "outcome": "succeeded", "usage_status": "measured",
		"input_tokens": 400, "output_tokens": 200, "provider_request_id": "resp_synthetic",
	})
	if finalized.Code != http.StatusOK {
		t.Fatalf("finalize status=%d body=%s", finalized.Code, finalized.Body.String())
	}
	if _, err := store.DB().Exec(`UPDATE memory_jobs SET attempts=2,lease_until=? WHERE id='job-model-measured'`, time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	blocked := memoryWorkerRequest(t, handler, token, reservePath, map[string]any{
		"attempt": 2, "request_key": "783f3885-e860-429a-865d-d10bf6e7cce4",
		"provider": "openai_responses", "model": "gpt-6-luna",
		"estimated_input_tokens": 1, "max_output_tokens": 1,
	})
	if blocked.Code != http.StatusConflict {
		t.Fatalf("actual usage above reservation did not consume later budget: status=%d body=%s", blocked.Code, blocked.Body.String())
	}
	var storedInput, storedOutput int
	if err := store.DB().QueryRow(`SELECT input_tokens,output_tokens FROM memory_model_calls WHERE id=?`, call.CallID).Scan(&storedInput, &storedOutput); err != nil {
		t.Fatal(err)
	}
	if storedInput != 400 || storedOutput != 200 {
		t.Fatalf("measured usage was not retained: input=%d output=%d", storedInput, storedOutput)
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

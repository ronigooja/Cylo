package memory

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/storage"
)

type backupManifestHTTPFixture struct {
	store   *storage.Store
	server  *control.Server
	handler http.Handler
	root    string
	job     map[string]any
	jobs    []map[string]any
}

const backupManifestRunnerToken = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
const backupManifestAlternateToken = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"

func newBackupManifestHTTPFixture(t *testing.T, manifestPath string) *backupManifestHTTPFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := storage.Open(ctx, storage.Config{Path: filepath.Join(root, "memory.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
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
	server, err := control.NewServer(store, control.Config{
		RunnerToken: backupManifestRunnerToken, RunnerID: "fixture-runner",
		RunnerWorkspaceIDs: []string{"default"}, RunnerScopes: []string{derivedClaimScope, derivedCompleteScope},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = RegisterDerived(server, store, DerivedConfig{BackupDeleteManifestPath: manifestPath}); err != nil {
		t.Fatal(err)
	}
	if err = control.RegisterExportRoutes(server); err != nil {
		t.Fatal(err)
	}
	repo := NewRepositoryWithStore(store)
	claim := writeMemory(t, ctx, store.DB(), repo, Scope{Kind: ScopePersonal, PrincipalID: "fixture-owner"}, "deleted claim", "backup-manifest-source", 0)
	if _, err = runDelete(ctx, store.DB(), repo, DeleteInput{
		WorkspaceID: "default", ActorID: "fixture-owner", ClaimID: claim.ID,
		Expected: claim.Revision, ReasonCode: "user_requested",
	}); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	response := performBackupManifestRequest(t, handler, "POST", "/runner/v1/memory/deletion-jobs/claim", map[string]any{"limit": 100}, backupManifestRunnerToken)
	if response.Code != http.StatusOK {
		t.Fatalf("claim cleanup jobs status=%d body=%s", response.Code, response.Body.String())
	}
	var claimed struct {
		Items []map[string]any `json:"items"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &claimed); err != nil {
		t.Fatal(err)
	}
	var job map[string]any
	for _, item := range claimed.Items {
		if item["target"] == "backup_delete_manifest" {
			job = item
			break
		}
	}
	if job == nil {
		t.Fatal("backup_delete_manifest cleanup job was not claimed")
	}
	return &backupManifestHTTPFixture{store: store, server: server, handler: handler, root: root, job: job, jobs: claimed.Items}
}

func TestExportTempCleanupHTTPThenDeletionCompletionAdvancesCheckpoint(t *testing.T) {
	f := newBackupManifestHTTPFixture(t, privateBackupManifestPath(t))
	var job map[string]any
	for _, candidate := range f.jobs {
		if candidate["target"] == "export_temp" {
			job = candidate
			break
		}
	}
	if job == nil {
		t.Fatal("export_temp cleanup job was not claimed")
	}
	id := "drill-export"
	now := "2026-10-22T00:00:00Z"
	if _, err := f.store.DB().ExecContext(context.Background(), `INSERT INTO cylo_export_temp_registry(export_id,workspace_id,owner_id,export_scope,created_deletion_sequence,status,invalidation_sequence,created_at,updated_at) VALUES(?, 'default', 'fixture-owner', 'personal', 0, 'active', 0, ?, ?)`, id, now, now); err != nil {
		t.Fatal(err)
	}
	root := f.store.Path() + ".exports"
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{id + ".data.json", id + ".archive.zip"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("private export fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.store.DB().ExecContext(context.Background(), `UPDATE cylo_instance_state SET restore_deletion_reconciliation_required=1,derived_retrieval_enabled=0 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	ordinary := performBackupManifestRequest(t, f.handler, "POST", "/runner/v1/memory/jobs/claim", map[string]any{"limit": 1}, backupManifestRunnerToken)
	if ordinary.Code != http.StatusServiceUnavailable {
		t.Fatalf("ordinary memory route escaped restore gate: status=%d body=%s", ordinary.Code, ordinary.Body.String())
	}
	payload := map[string]any{
		"job_id": job["id"], "workspace_id": job["workspace_id"], "entity_id": job["entity_id"],
		"entity_version": int64(job["entity_version"].(float64)), "attempt": int(job["attempt"].(float64)),
	}
	cleanup := performBackupManifestRequest(t, f.handler, "POST", "/runner/v1/memory/export-temp/cleanup", payload, backupManifestRunnerToken)
	if cleanup.Code != http.StatusOK || !bytes.Contains(cleanup.Body.Bytes(), []byte(`"status":"completed"`)) {
		t.Fatalf("export temp cleanup status=%d body=%s", cleanup.Code, cleanup.Body.String())
	}
	for _, name := range []string{id + ".data.json", id + ".archive.zip"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("registered export artifact remains or stat failed: %v", err)
		}
	}
	complete := performBackupManifestRequest(t, f.handler, "POST", "/runner/v1/memory/deletion-jobs/"+job["id"].(string)+"/complete", map[string]any{"attempt": int(job["attempt"].(float64))}, backupManifestRunnerToken)
	if complete.Code != http.StatusOK || !bytes.Contains(complete.Body.Bytes(), []byte(`"status":"done"`)) {
		t.Fatalf("generic deletion completion status=%d body=%s", complete.Code, complete.Body.String())
	}
	var status string
	if err := f.store.DB().QueryRowContext(context.Background(), `SELECT status FROM memory_jobs WHERE id=?`, job["id"]).Scan(&status); err != nil || status != "done" {
		t.Fatalf("export cleanup job status=%q err=%v", status, err)
	}
	var checkpoint int64
	if err := f.store.DB().QueryRowContext(context.Background(), `SELECT completed_sequence FROM cylo_deletion_reconciliation_checkpoints WHERE target='export_temp'`).Scan(&checkpoint); err != nil || checkpoint != 1 {
		t.Fatalf("export temp checkpoint=%d err=%v", checkpoint, err)
	}
}

func privateBackupManifestPath(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, "backup-deletions.jsonl")
}

func (f *backupManifestHTTPFixture) request(t *testing.T, workspace string, attempt int, token string) *httptest.ResponseRecorder {
	t.Helper()
	return performBackupManifestRequest(t, f.handler, "POST", "/runner/v1/memory/backup-delete-manifest/cleanup", map[string]any{
		"job_id": f.job["id"], "workspace_id": workspace, "entity_id": f.job["entity_id"],
		"entity_version": int64(f.job["entity_version"].(float64)), "attempt": attempt,
	}, token)
}

func performBackupManifestRequest(t *testing.T, handler http.Handler, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestBackupDeleteManifestCleanupHTTPIsDurableAndIdempotent(t *testing.T) {
	manifest := privateBackupManifestPath(t)
	f := newBackupManifestHTTPFixture(t, manifest)
	attempt := int(f.job["attempt"].(float64))
	response := f.request(t, "default", attempt, backupManifestRunnerToken)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"status":"completed"`)) {
		t.Fatalf("cleanup status=%d body=%s", response.Code, response.Body.String())
	}
	contents, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var row struct {
		Sequence   int64  `json:"sequence"`
		SourceHash string `json:"source_hash"`
	}
	if err = json.Unmarshal(bytes.TrimSpace(contents), &row); err != nil || row.Sequence != 1 || row.SourceHash == "" {
		t.Fatalf("durable manifest row=%#v err=%v", row, err)
	}
	assertBackupManifestJobDone(t, f, attempt)
	response = f.request(t, "default", attempt, backupManifestRunnerToken)
	if response.Code != http.StatusOK {
		t.Fatalf("idempotent cleanup retry status=%d body=%s", response.Code, response.Body.String())
	}
	contents, err = os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(contents, []byte("\n")); got != 1 {
		t.Fatalf("idempotent retry wrote %d manifest records", got)
	}
	assertBackupManifestJobDone(t, f, attempt)
}

func TestBackupDeleteManifestCleanupHTTPRejectsScopeWorkspaceLeaseKindAndTarget(t *testing.T) {
	cases := []struct {
		name      string
		attempt   func(int) int
		mutate    string
		workspace string
		want      int
	}{
		{name: "scope", want: http.StatusForbidden},
		{name: "workspace", want: http.StatusForbidden},
		{name: "lease attempt", attempt: func(value int) int { return value + 1 }, want: http.StatusConflict},
		{name: "job kind", mutate: "kind", want: http.StatusForbidden},
		{name: "job target", mutate: "target", want: http.StatusForbidden},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			manifest := privateBackupManifestPath(t)
			f := newBackupManifestHTTPFixture(t, manifest)
			attempt := int(f.job["attempt"].(float64))
			if test.attempt != nil {
				attempt = test.attempt(attempt)
			}
			if test.mutate == "kind" {
				if _, err := f.store.DB().ExecContext(context.Background(), `UPDATE memory_jobs SET kind='index' WHERE id=?`, f.job["id"]); err != nil {
					t.Fatal(err)
				}
			}
			if test.mutate == "target" {
				if _, err := f.store.DB().ExecContext(context.Background(), `UPDATE memory_jobs SET target='unknown_target' WHERE id=?`, f.job["id"]); err != nil {
					t.Fatal(err)
				}
			}
			workspace := test.workspace
			if workspace == "" {
				workspace = "default"
			}
			token := backupManifestRunnerToken
			if test.name == "scope" || test.name == "workspace" {
				scopes, workspaces := []string{derivedCompleteScope}, []string{"default"}
				if test.name == "scope" {
					scopes = []string{derivedClaimScope}
				} else {
					workspaces = []string{"other"}
				}
				if err := f.server.RegisterRunnerCredential(context.Background(), backupManifestAlternateToken, "alternate-runner", workspaces, scopes); err != nil {
					t.Fatal(err)
				}
				token = backupManifestAlternateToken
			}
			response := f.request(t, workspace, attempt, token)
			if response.Code != test.want {
				t.Fatalf("cleanup status=%d body=%s want=%d", response.Code, response.Body.String(), test.want)
			}
			if _, err := os.Stat(manifest); !os.IsNotExist(err) {
				t.Fatalf("invalid cleanup wrote a manifest (stat error %v)", err)
			}
		})
	}
}

func TestBackupDeleteManifestCleanupHTTPRejectsInvalidTombstoneAndPersistenceFailures(t *testing.T) {
	t.Run("invalid tombstone", func(t *testing.T) {
		manifest := privateBackupManifestPath(t)
		f := newBackupManifestHTTPFixture(t, manifest)
		if _, err := f.store.DB().ExecContext(context.Background(), `DELETE FROM memory_tombstones WHERE claim_id=?`, f.job["entity_id"]); err != nil {
			t.Fatal(err)
		}
		response := f.request(t, "default", int(f.job["attempt"].(float64)), backupManifestRunnerToken)
		if response.Code != http.StatusConflict {
			t.Fatalf("invalid tombstone status=%d body=%s", response.Code, response.Body.String())
		}
		if _, err := os.Stat(manifest); !os.IsNotExist(err) {
			t.Fatalf("invalid tombstone wrote a manifest (stat error %v)", err)
		}
	})
	for _, failure := range []string{"missing configuration", "unsafe parent", "unsafe manifest file"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "backup-deletions.jsonl")
			switch failure {
			case "missing configuration":
				path = ""
			case "unsafe parent":
				parent := filepath.Join(root, "public")
				if err := os.Mkdir(parent, 0o755); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(parent, "backup-deletions.jsonl")
			case "unsafe manifest file":
				if err := os.WriteFile(path, nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			f := newBackupManifestHTTPFixture(t, path)
			response := f.request(t, "default", int(f.job["attempt"].(float64)), backupManifestRunnerToken)
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("persistence failure status=%d body=%s", response.Code, response.Body.String())
			}
			assertBackupManifestJobNotDone(t, f)
			assertBackupManifestJobNotDone(t, f)
			if _, err := f.store.DB().ExecContext(context.Background(), `UPDATE cylo_instance_state SET restore_deletion_reconciliation_required=1,derived_retrieval_enabled=0 WHERE singleton=1`); err != nil {
				t.Fatal(err)
			}
			gated := performBackupManifestRequest(t, f.handler, http.MethodPost, "/runner/v1/memory/jobs/claim", map[string]any{"limit": 1}, backupManifestRunnerToken)
			if gated.Code != http.StatusServiceUnavailable {
				t.Fatalf("restore gate opened after persistence failure: status=%d body=%s", gated.Code, gated.Body.String())
			}
		})
	}
}

func assertBackupManifestJobDone(t *testing.T, f *backupManifestHTTPFixture, attempt int) {
	t.Helper()
	var status string
	var checkpoint int64
	if err := f.store.DB().QueryRowContext(context.Background(), `SELECT status FROM memory_jobs WHERE id=? AND attempts=?`, f.job["id"], attempt).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "done" {
		t.Fatalf("backup manifest cleanup job status=%q", status)
	}
	if err := f.store.DB().QueryRowContext(context.Background(), `SELECT completed_sequence FROM cylo_deletion_reconciliation_checkpoints WHERE target='backup_delete_manifest'`).Scan(&checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint != 1 {
		t.Fatalf("backup deletion checkpoint=%d, want 1", checkpoint)
	}
}

func assertBackupManifestJobNotDone(t *testing.T, f *backupManifestHTTPFixture) {
	t.Helper()
	var status string
	if err := f.store.DB().QueryRowContext(context.Background(), `SELECT status FROM memory_jobs WHERE id=?`, f.job["id"]).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == "done" {
		t.Fatal("backup manifest cleanup job completed after manifest persistence failure")
	}
	var checkpoint int64
	err := f.store.DB().QueryRowContext(context.Background(), `SELECT completed_sequence FROM cylo_deletion_reconciliation_checkpoints WHERE target='backup_delete_manifest'`).Scan(&checkpoint)
	if err == nil && checkpoint != 0 {
		t.Fatalf("backup deletion checkpoint advanced to %d after manifest persistence failure", checkpoint)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
}

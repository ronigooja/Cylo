package ops_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/internal/capabilities"
	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/memory"
	"github.com/ronigooja/Cylo/internal/storage"
	"github.com/ronigooja/Cylo/internal/voice"
)

// TestMaintenanceRESTEncryptedRestoreDrill is an explicit disposable local
// acceptance drill. Set CYLO_OPS_PYTHON to the isolated Python interpreter
// with tools/data_lifecycle/requirements.txt installed.
func TestMaintenanceRESTEncryptedRestoreDrill(t *testing.T) {
	python := os.Getenv("CYLO_OPS_PYTHON")
	if python == "" {
		t.Skip("set CYLO_OPS_PYTHON to run the encrypted restore drill")
	}
	repo := repositoryRoot(t)
	ctx := context.Background()
	tmp := t.TempDir()
	db := filepath.Join(tmp, "source", "data", "cylo.sqlite")
	if err := os.MkdirAll(filepath.Dir(db), 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := openDrillStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	deviceToken := randomBearer(t)
	if _, _, err = control.InitializeInstanceAuth(ctx, store, deviceToken); err != nil {
		t.Fatal(err)
	}
	runnerToken := randomBearer(t)
	if err = control.RegisterRunnerCredentialIfMissing(ctx, store, runnerToken, "ops-drill-runner", []string{"default"}, []string{"runner:control:read"}); err != nil {
		t.Fatal(err)
	}
	server, err := control.NewServer(store, control.Config{InstanceScopes: []string{"instance:admin"}, RunnerToken: runnerToken, RunnerID: "ops-drill-runner", RunnerWorkspaceIDs: []string{"default"}, RunnerScopes: []string{"runner:control:read"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = control.RegisterInstanceRoutes(server); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	initial := getInstance(t, httpServer.URL, runnerToken)
	if initial.InstanceID == "" || initial.Epoch != 1 || !initial.DispatchEnabled {
		t.Fatalf("unexpected initial runner instance response: %#v", initial)
	}
	response, err := postJSON(httpServer.URL+"/v1/admin/instance/fence", deviceToken, map[string]any{"reason": "ops restore drill", "expected_epoch": initial.Epoch})
	if err != nil || response.Code != http.StatusOK {
		t.Fatalf("REST fence response code=%d body=%s err=%v", response.Code, response.Body, err)
	}
	fenced := getInstance(t, httpServer.URL, runnerToken)
	if fenced.DispatchEnabled || fenced.Epoch != 2 {
		t.Fatalf("REST fence did not advance and close dispatch: %#v", fenced)
	}
	gateTx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	derivedEnabled, err := control.DerivedRetrievalEnabledTx(ctx, gateTx)
	_ = gateTx.Rollback()
	if err != nil || derivedEnabled {
		t.Fatalf("REST fence left derived retrieval enabled: enabled=%t err=%v", derivedEnabled, err)
	}
	// The derived-data gate must close with the source fence. Keep a real
	// deletion ledger row in the snapshot and verify it remains present later.
	if _, err = store.DB().ExecContext(ctx, `INSERT INTO memory_claims(id,workspace_id,scope_kind,scope_id,principal_id,subject,claim_key,authority_kind,status,revision,created_at,updated_at,deleted_at) VALUES('deleted-claim','default','workspace','','','ops drill','deleted','user_confirmed','tombstoned',1,?,?,?)`, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `INSERT INTO memory_tombstones(claim_id,workspace_id,revision,actor_id,reason,deleted_at) VALUES('deleted-claim','default',1,'ops-owner','drill deletion',?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(tmp, "backup.key")
	writePrivate(t, keyFile, key)
	backupDir := filepath.Join(tmp, "backups")
	if runPythonExpectFailure(t, python, repo, "tools/cylo_backup.py", "--database", db, "--destination", backupDir, "--key-file", keyFile) == nil {
		t.Fatal("backup was allowed while the source SQLite writer lock was held")
	}
	httpServer.Close()
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	dataRoot := filepath.Join(tmp, "managed")
	if err = os.MkdirAll(filepath.Join(dataRoot, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dataRoot, "config", "service.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inventory := filepath.Join(tmp, "inventory.json")
	inventoryJSON, _ := json.Marshal(map[string]any{"schema_version": 1, "roots": map[string]string{"managed": dataRoot}, "service_config_paths": []any{map[string]string{"root_id": "managed", "path": "config/service.json"}}, "artifact_paths": []any{}, "persistent_volume_paths": []any{}, "secret_refs": []any{}})
	if err = os.WriteFile(inventory, inventoryJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	volumeInventory := filepath.Join(tmp, "volume-inventory.json")
	volumePath := filepath.Join(dataRoot, "volume.db")
	if err = os.WriteFile(volumePath, []byte("persistent data"), 0o600); err != nil {
		t.Fatal(err)
	}
	volumeJSON, _ := json.Marshal(map[string]any{"schema_version": 1, "roots": map[string]string{"managed": dataRoot}, "service_config_paths": []any{}, "artifact_paths": []any{}, "persistent_volume_paths": []any{map[string]string{"root_id": "managed", "path": "volume.db"}}, "secret_refs": []any{}})
	if err = os.WriteFile(volumeInventory, volumeJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	if runPythonExpectFailure(t, python, repo, "tools/cylo_backup.py", "--database", db, "--destination", filepath.Join(tmp, "volume-backups"), "--key-file", keyFile, "--inventory", volumeInventory) == nil {
		t.Fatal("backup was allowed to claim online persistent-volume consistency without a quiesce adapter")
	}
	runPython(t, python, repo, "tools/cylo_backup.py", "--database", db, "--destination", backupDir, "--key-file", keyFile, "--inventory", inventory)
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	var artifact string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".cylo-backup") {
			if artifact != "" {
				t.Fatal("backup command created more than one encrypted artifact")
			}
			artifact = filepath.Join(backupDir, entry.Name())
		}
	}
	if artifact == "" {
		t.Fatal("backup command did not create an encrypted artifact")
	}
	maintenance := filepath.Join(tmp, "cylo-maintenance")
	build := exec.Command("go", "build", "-o", maintenance, "./cmd/cylo-maintenance")
	build.Dir = repo
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build maintenance CLI: %v\n%s", err, output)
	}
	latestLedger := filepath.Join(tmp, "latest.deletion-ledger")
	latestBytes, err := os.ReadFile(db + ".deletion-ledger")
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err = os.WriteFile(latestLedger, latestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	latestHead := filepath.Join(tmp, "latest-deletion-head.json")
	deletionHead := exec.Command(maintenance, "deletion-head", "--database", db, "--output", latestHead)
	if output, err := deletionHead.CombinedOutput(); err != nil {
		t.Fatalf("write current deletion head: %v\n%s", err, output)
	}
	corrupt := filepath.Join(tmp, "corrupt.cylo-backup")
	artifactBytes, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	artifactBytes[len(artifactBytes)-1] ^= 0xff
	if err = os.WriteFile(corrupt, artifactBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	badRestore := filepath.Join(tmp, "bad-restore")
	if runPythonExpectFailure(t, python, repo, "tools/cylo_restore.py", "--artifact", corrupt, "--key-file", keyFile, "--latest-deletion-ledger", latestLedger, "--latest-deletion-head-file", latestHead, "--restore-directory", badRestore) == nil {
		t.Fatal("tampered encrypted archive was accepted")
	}
	if _, err = os.Lstat(badRestore); !os.IsNotExist(err) {
		t.Fatalf("tampered archive created a staged restore path: %v", err)
	}
	restore := filepath.Join(tmp, "restore")
	runPython(t, python, repo, "tools/cylo_restore.py", "--artifact", artifact, "--key-file", keyFile, "--latest-deletion-ledger", latestLedger, "--latest-deletion-head-file", latestHead, "--restore-directory", restore)
	pendingDB := filepath.Join(restore, "data", "cylo.sqlite.restore-pending")
	proofKey := make([]byte, 32)
	if _, err = rand.Read(proofKey); err != nil {
		t.Fatal(err)
	}
	proofKeyFile := filepath.Join(tmp, "proof.key")
	writePrivate(t, proofKeyFile, proofKey)
	fakeSystemd := filepath.Join(tmp, "fake-systemd")
	if err = os.MkdirAll(fakeSystemd, 0o700); err != nil {
		t.Fatal(err)
	}
	systemctl := filepath.Join(fakeSystemd, "systemctl")
	fakeSystemctl := []byte("#!/bin/sh\nif [ \"$1\" = \"mask\" ]; then exit 0; fi\nif [ \"$1\" = \"show\" ]; then case \"$3\" in --property=ActiveState) echo inactive;; --property=UnitFileState) echo masked;; *) exit 2;; esac; exit 0; fi\nexit 2\n")
	if err = os.WriteFile(systemctl, fakeSystemctl, 0o700); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	if err = os.Setenv("PATH", fakeSystemd+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Setenv("PATH", oldPath) }()
	proofFile := filepath.Join(tmp, "isolation-proof.json")
	runPython(t, python, repo, "tools/cylo_migrate.py", "--maintenance", maintenance, "--database", db, "--proof-key-file", proofKeyFile, "--proof-output", proofFile, "--unit", "cylo-ops-drill.service", "--stop-mask-and-fence")
	activeDB := filepath.Join(restore, "data", "cylo.sqlite")
	prepare := exec.Command(maintenance, "prepare-restore", "--database", pendingDB, "--activate-as", activeDB, "--new-instance-id", "ops-drill-target", "--expected-epoch", "2")
	if output, err := prepare.CombinedOutput(); err != nil {
		t.Fatalf("prepare restore: %v\n%s", err, output)
	}
	reconcile := exec.Command(maintenance, "reconcile-deletions", "--database", activeDB, "--restore-state", filepath.Join(restore, "restore-state.json"))
	if output, err := reconcile.CombinedOutput(); err != nil {
		t.Fatalf("reconcile trusted deletion head: %v\n%s", err, output)
	}
	assertRestoredAuthority(t, activeDB, false, true, false)
	enable := exec.Command(maintenance, "enable", "--database", activeDB, "--expected-epoch", "3", "--proof-file", proofFile, "--proof-key-file", proofKeyFile)
	if output, err := enable.CombinedOutput(); err != nil {
		t.Fatalf("enable after verified zero-head reconciliation: %v\n%s", err, output)
	}
	assertRestoredAuthority(t, activeDB, true, true, false)
}

// TestEncryptedBackupPersistsNonemptyDeletionManifest exercises the production
// backup CLI against disposable authority and sidecar files. The deletion
// journal includes external targets whose live providers are absent, so the
// verified restore remains closed for reconciliation.
func TestEncryptedBackupPersistsNonemptyDeletionManifest(t *testing.T) {
	python := os.Getenv("CYLO_OPS_PYTHON")
	if python == "" {
		t.Skip("set CYLO_OPS_PYTHON to run the encrypted backup manifest drill")
	}
	repoRoot := repositoryRoot(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "source", "data", "cylo.sqlite")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store, err := openDrillStore(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	repository := memory.NewRepositoryWithStore(store)
	now := time.Now().UTC()
	scope := memory.Scope{Kind: memory.ScopeWorkspace}
	source := memory.Source{ID: "ops-delete-source", Version: 1, Scope: scope, Kind: "user_note", Reference: "ops-drill", RecordedAt: now, Producer: "user"}
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := repository.PutTx(ctx, tx, memory.WriteInput{
		WorkspaceID: "default", ActorID: "ops-owner",
		Source: source,
		Claim:  memory.Claim{WorkspaceID: "default", Scope: scope, Subject: "ops drill", Key: "deleted claim", Content: "temporary drill record", Authority: memory.AuthorityUserConfirmed, Status: memory.ClaimActive, Revision: 1, Sources: []memory.Source{source}},
	})
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.DeleteTx(ctx, tx, memory.DeleteInput{WorkspaceID: "default", ActorID: "ops-owner", ClaimID: claim.ID, Expected: 1, ReasonCode: "user_requested"}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	journal, err := storage.NewDeletionJournal(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	journalRows, err := journal.ReadAll()
	if err != nil || len(journalRows) != 1 {
		t.Fatalf("expected one durable deletion row, rows=%#v err=%v", journalRows, err)
	}
	manifestDir := filepath.Join(tmp, "backup-maintenance")
	if err = os.Mkdir(manifestDir, 0o700); err != nil {
		t.Fatal(err)
	}
	backupDeleteManifest := filepath.Join(manifestDir, "deletions.jsonl")
	if err = control.AppendBackupDeleteManifest(backupDeleteManifest, journalRows[0], journalRows); err != nil {
		t.Fatal(err)
	}
	if _, err = control.FenceInstance(ctx, store, "nonempty deletion backup drill", 1); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(tmp, "backup.key")
	writePrivate(t, keyFile, key)
	backupDir := filepath.Join(tmp, "backups")
	runPython(t, python, repoRoot, "tools/cylo_backup.py", "--database", dbPath, "--destination", backupDir, "--key-file", keyFile, "--backup-delete-manifest", backupDeleteManifest)
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	var artifact string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".cylo-backup") {
			if artifact != "" {
				t.Fatal("backup command created more than one encrypted artifact")
			}
			artifact = filepath.Join(backupDir, entry.Name())
		}
	}
	if artifact == "" {
		t.Fatalf("encrypted backup artifact missing from %s", backupDir)
	}
	verifiedDir := filepath.Join(tmp, "verified")
	if err = os.Mkdir(verifiedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	inspect := `import json,sys; from pathlib import Path; from data_lifecycle.common import read_key,unpack_verified; from data_lifecycle.retention import _read_manifest; artifact=Path(sys.argv[1]); key=read_key(Path(sys.argv[2])); payload,manifest=unpack_verified(artifact,key,Path(sys.argv[3])); print(json.dumps({"deletion":manifest["backup_delete_manifest"],"retention":_read_manifest(artifact.parent,key)},sort_keys=True))`
	result := runPythonCode(python, repoRoot, inspect, artifact, keyFile, verifiedDir)
	if result.err != nil {
		t.Fatalf("decrypt and verify backup: %v\n%s", result.err, result.text)
	}
	var verified struct {
		Deletion struct {
			Baseline string   `json:"baseline"`
			Sequence int64    `json:"sequence"`
			Targets  []string `json:"targets"`
			Version  int      `json:"version"`
		} `json:"deletion"`
		Retention struct {
			Entries []struct {
				Artifact         string   `json:"artifact"`
				DeletionBaseline string   `json:"deletion_baseline"`
				DeletionSequence int64    `json:"deletion_sequence"`
				DeletionTargets  []string `json:"deletion_targets"`
			} `json:"entries"`
		} `json:"retention"`
	}
	if err = json.Unmarshal([]byte(result.text), &verified); err != nil {
		t.Fatalf("decode decrypted backup evidence: %v; output=%s", err, result.text)
	}
	if verified.Deletion.Sequence != 1 || verified.Deletion.Baseline == "" || verified.Deletion.Version != 1 || len(verified.Deletion.Targets) == 0 {
		t.Fatalf("encrypted manifest deletion head is incomplete: %#v", verified.Deletion)
	}
	hasBackupTarget := false
	if len(verified.Retention.Entries) == 1 {
		for _, target := range verified.Retention.Entries[0].DeletionTargets {
			if target == "backup_delete_manifest" {
				hasBackupTarget = true
			}
		}
	}
	if len(verified.Retention.Entries) != 1 || verified.Retention.Entries[0].Artifact != filepath.Base(artifact) || verified.Retention.Entries[0].DeletionSequence != 1 || verified.Retention.Entries[0].DeletionBaseline != verified.Deletion.Baseline || !hasBackupTarget {
		t.Fatalf("authenticated retention ownership does not bind the nonempty deletion snapshot: %#v", verified.Retention.Entries)
	}
	latestLedger := filepath.Join(tmp, "latest.deletion-ledger")
	latestBytes, err := os.ReadFile(dbPath + ".deletion-ledger")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(latestLedger, latestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	maintenance := filepath.Join(tmp, "cylo-maintenance")
	build := exec.Command("go", "build", "-o", maintenance, "./cmd/cylo-maintenance")
	build.Dir = repoRoot
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build maintenance CLI: %v\n%s", buildErr, output)
	}
	headFile := filepath.Join(tmp, "latest-head.json")
	headCommand := exec.Command(maintenance, "deletion-head", "--database", dbPath, "--output", headFile)
	if output, headErr := headCommand.CombinedOutput(); headErr != nil {
		t.Fatalf("read latest deletion head: %v\n%s", headErr, output)
	}
	restore := filepath.Join(tmp, "restore")
	runPython(t, python, repoRoot, "tools/cylo_restore.py", "--artifact", artifact, "--key-file", keyFile, "--latest-deletion-ledger", latestLedger, "--latest-deletion-head-file", headFile, "--restore-directory", restore)
	var restoreState struct {
		RestoreState struct {
			DeletionRecordsVerified map[string]any `json:"deletion_records_verified"`
			TrustedHead             struct {
				SnapshotSequence int64 `json:"snapshot_sequence"`
				JournalSequence  int64 `json:"journal_sequence"`
			} `json:"trusted_deletion_head"`
		} `json:"restore_state"`
	}
	stateBytes, err := os.ReadFile(filepath.Join(restore, "restore-state.json"))
	if err != nil {
		t.Fatalf("read restored deletion authority state: %v", err)
	}
	if err = json.Unmarshal(stateBytes, &restoreState); err != nil {
		t.Fatalf("decode restored deletion authority state: %v", err)
	}
	if len(restoreState.RestoreState.DeletionRecordsVerified) == 0 || restoreState.RestoreState.TrustedHead.SnapshotSequence != 1 || restoreState.RestoreState.TrustedHead.JournalSequence != 1 {
		t.Fatalf("restored external deletion head is not verified: %#v", restoreState)
	}
	pendingDB := filepath.Join(restore, "data", "cylo.sqlite.restore-pending")
	activeDB := filepath.Join(restore, "data", "cylo.sqlite")
	prepare := exec.Command(maintenance, "prepare-restore", "--database", pendingDB, "--activate-as", activeDB, "--new-instance-id", "nonempty-delete-target", "--expected-epoch", "2")
	if output, prepareErr := prepare.CombinedOutput(); prepareErr != nil {
		t.Fatalf("prepare nonempty deletion restore: %v\n%s", prepareErr, output)
	}
	reconcile := exec.Command(maintenance, "reconcile-deletions", "--database", activeDB, "--restore-state", filepath.Join(restore, "restore-state.json"))
	output, reconcileErr := reconcile.CombinedOutput()
	if reconcileErr != nil {
		t.Fatalf("reconcile nonempty deletion head: %v\n%s", reconcileErr, output)
	}
	var reconciliation struct {
		Replayed       int      `json:"replayed"`
		GateCleared    bool     `json:"gate_cleared"`
		BlockedTargets []string `json:"blocked_targets"`
	}
	if err = json.Unmarshal(output, &reconciliation); err != nil {
		t.Fatalf("decode reconciliation result: %v; output=%s", err, output)
	}
	missingExternalAdapter := false
	for _, target := range reconciliation.BlockedTargets {
		if target == "mem0:adapter_missing" || target == "qdrant:adapter_missing" {
			missingExternalAdapter = true
		}
	}
	if reconciliation.GateCleared || !missingExternalAdapter {
		t.Fatalf("external deletion adapters were falsely accepted: %#v", reconciliation)
	}
	assertRestoredAuthority(t, activeDB, false, false, true)
}

type runnerInstance struct {
	InstanceID      string `json:"instance_id"`
	Epoch           int64  `json:"epoch"`
	DispatchEnabled bool   `json:"dispatch_enabled"`
}

func getInstance(t *testing.T, base, token string) runnerInstance {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+"/runner/v1/instance", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET runner instance status=%d body=%s", resp.StatusCode, body)
	}
	var state runnerInstance
	if err = json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

type responseBody struct {
	Code int
	Body []byte
}

func postJSON(url, token string, body any) (responseBody, error) {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return responseBody{}, err
	}
	defer resp.Body.Close()
	value, _ := io.ReadAll(resp.Body)
	return responseBody{Code: resp.StatusCode, Body: value}, nil
}

func openDrillStore(ctx context.Context, path string) (*storage.Store, error) {
	store, err := storage.Open(ctx, storage.Config{Path: path})
	if err != nil {
		return nil, err
	}
	register := []func(*storage.Store) error{storage.RegisterCoreMigrations, memory.RegisterMigrations, voice.RegisterMigrations, capabilities.RegisterMigrations, control.RegisterInstanceMaintenanceMigration}
	for _, fn := range register {
		if err = fn(store); err != nil {
			_ = store.Close()
			return nil, err
		}
	}
	if err = store.Migrate(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func assertRestoredAuthority(t *testing.T, path string, expectedDispatch, expectedDerived, expectedReconciliation bool) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var dispatch, derived, reconciliationRequired, tombstones int
	if err = db.QueryRow(`SELECT dispatch_enabled,derived_retrieval_enabled,restore_deletion_reconciliation_required FROM cylo_instance_state WHERE singleton=1`).Scan(&dispatch, &derived, &reconciliationRequired); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM memory_tombstones`).Scan(&tombstones); err != nil {
		t.Fatal(err)
	}
	if (dispatch == 1) != expectedDispatch || (derived == 1) != expectedDerived || (reconciliationRequired == 1) != expectedReconciliation || tombstones != 1 {
		t.Fatalf("restored dispatch=%d derived=%d reconciliation_required=%d tombstones=%d expected_dispatch=%t expected_reconciliation=%t", dispatch, derived, reconciliationRequired, tombstones, expectedDispatch, expectedReconciliation)
	}
}

func signedProof(t *testing.T, key []byte, oldID string, epoch int64, unknown int) []byte {
	t.Helper()
	payload := map[string]any{"schema_version": 1, "old_instance_id": oldID, "old_epoch": epoch, "source_host": "local-ops-drill", "units": []map[string]string{{"name": "cylo-ops-drill.service", "active_state": "inactive", "unit_file_state": "masked"}}, "active_runs": 0, "unknown_effects": unknown, "active_deployment_jobs": 0, "live_deployments": 0, "open_voice_sessions": 0, "verified_at": time.Now().UTC()}
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	doc, _ := json.Marshal(map[string]string{"payload": base64.RawURLEncoding.EncodeToString(raw), "hmac": hex.EncodeToString(mac.Sum(nil))})
	return doc
}

func runPython(t *testing.T, python, repo, script string, args ...string) {
	t.Helper()
	if output := runPythonRaw(python, repo, script, args...); output.err != nil {
		t.Fatalf("%s failed: %v\n%s", script, output.err, output.text)
	}
}

func runPythonExpectFailure(t *testing.T, python, repo, script string, args ...string) error {
	t.Helper()
	output := runPythonRaw(python, repo, script, args...)
	if output.err == nil {
		return nil
	}
	return output.err
}

func runPythonRaw(python, repo, script string, args ...string) struct {
	err  error
	text string
} {
	commandArgs := append([]string{filepath.Join(repo, script)}, args...)
	command := exec.Command(python, commandArgs...)
	command.Dir = repo
	command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(repo, "tools"), "PYTHONDONTWRITEBYTECODE=1")
	output, err := command.CombinedOutput()
	return struct {
		err  error
		text string
	}{err, string(output)}
}

func runPythonCode(python, repo, code string, args ...string) struct {
	err  error
	text string
} {
	commandArgs := append([]string{"-c", code}, args...)
	command := exec.Command(python, commandArgs...)
	command.Dir = repo
	command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(repo, "tools"), "PYTHONDONTWRITEBYTECODE=1")
	output, err := command.CombinedOutput()
	return struct {
		err  error
		text string
	}{err, string(output)}
}

func randomBearer(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func hashBearer(t *testing.T, value string) []byte {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return digest[:]
}

func writePrivate(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func copyDB(t *testing.T, source, destination string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(destination, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate ops drill source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

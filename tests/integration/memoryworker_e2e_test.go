package integration

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/memory"
)

const memoryE2EModelCodec = "paraphrase_multilingual_minilm_l12_384_faf4aa42"

type memoryWorkerE2EProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	ended  bool
}

func (p *memoryWorkerE2EProcess) readEvent(t *testing.T) map[string]any {
	t.Helper()
	line, err := p.stdout.ReadBytes('\n')
	if err != nil {
		t.Fatalf("Python memory worker exited before reporting its E2E event: %v", err)
	}
	var event map[string]any
	if err := json.Unmarshal(line, &event); err != nil {
		t.Fatalf("Python memory worker returned invalid E2E protocol JSON: %v", err)
	}
	if event["event"] == "error" {
		t.Fatalf("Python memory worker E2E action failed with %v", event["error_type"])
	}
	return event
}

func (p *memoryWorkerE2EProcess) send(t *testing.T, request map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(p.stdin, "%s\n", data); err != nil {
		t.Fatalf("send Python worker E2E action: %v", err)
	}
	return p.readEvent(t)
}

func (p *memoryWorkerE2EProcess) stop(t *testing.T) {
	t.Helper()
	if p.ended {
		return
	}
	_ = p.send(t, map[string]any{"action": "stop"})
	_ = p.stdin.Close()
	if err := p.cmd.Wait(); err != nil {
		t.Fatalf("Python memory worker exited unsuccessfully after E2E: %v", err)
	}
	p.ended = true
}

func TestMemoryWorkerGoPythonMem0QdrantEndToEnd(t *testing.T) {
	if os.Getenv("CYLO_MEMORY_E2E") != "1" {
		t.Skip("set CYLO_MEMORY_E2E=1 to run the live Go/Python/Mem0/Qdrant acceptance flow")
	}
	proxyURL := strings.TrimSpace(os.Getenv("CYLO_MEMORY_E2E_LLM_BASE_URL"))
	proxyModel := strings.TrimSpace(os.Getenv("CYLO_MEMORY_E2E_LLM_MODEL"))
	proxyKey := os.Getenv("CYLO_MEMORY_E2E_LLM_API_KEY")
	if os.Getenv("CYLO_MEMORY_E2E_MOCK_RESPONSES") == "1" {
		mock := httptest.NewServer(http.HandlerFunc(memoryE2EResponsesStub))
		t.Cleanup(mock.Close)
		proxyURL, proxyModel, proxyKey = mock.URL+"/v1", "e2e-responses-stub", "e2e-test-key"
	} else if proxyURL == "" || proxyModel == "" || proxyKey == "" {
		t.Skip("live E2E requires CYLO_MEMORY_E2E_LLM_BASE_URL, CYLO_MEMORY_E2E_LLM_MODEL, and a deployment-provided CYLO_MEMORY_E2E_LLM_API_KEY")
	}

	qdrantURL := startEphemeralMemoryE2EQdrant(t)
	store, handler, runnerToken := memoryWorkerHarness(t,
		[]string{"memory:jobs:claim", "memory:jobs:complete", "memory:propose"},
		[]string{"default", "e2e-other"},
	)
	seedMemoryWorkerE2EClaim(t, store.DB(), "default", "claim-e2e-target", "event-e2e-target", "device juniper-731 is stored in cabinet amber-47")
	seedMemoryWorkerE2EClaim(t, store.DB(), "e2e-other", "claim-e2e-other", "event-e2e-other", "device orchid-284 is stored in cabinet violet-19")
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	python := strings.TrimSpace(os.Getenv("CYLO_MEMORY_E2E_PYTHON"))
	if python == "" {
		python = filepath.Join(repositoryRoot, "services", "memory", ".venv", "bin", "python")
	}
	historyDB := filepath.Join(t.TempDir(), "mem0-history.sqlite")
	workerEnv := memoryE2EEnv(os.Environ(), map[string]string{
		"CYLO_CONTROL_URL":              server.URL,
		"CYLO_RUNNER_TOKEN":             runnerToken,
		"CYLO_QDRANT_URL":               qdrantURL,
		"CYLO_QDRANT_API_KEY":           "",
		"CYLO_QDRANT_COLLECTION":        "cylo_memory_v1_" + memoryE2EModelCodec + "_e2e",
		"CYLO_MEM0_COLLECTION":          "cylo_mem0_e2e",
		"CYLO_MEM0_HISTORY_DB":          historyDB,
		"CYLO_MEM0_LLM_PROVIDER":        "openai",
		"CYLO_MEM0_LLM_MODEL":           proxyModel,
		"CYLO_MEM0_LLM_BASE_URL":        proxyURL,
		"CYLO_MEM0_LLM_API_KEY":         proxyKey,
		"CYLO_MEMORY_EXTRACTOR_VERSION": "mem0-controlled-e2e-v1",
		"CYLO_MEMORY_BATCH_SIZE":        "100",
		"CYLO_MEMORY_MODEL_CACHE":       "/tmp/cylo-fastembed-cache",
		"FASTEMBED_CACHE_PATH":          "/tmp/cylo-fastembed-cache",
		"MEM0_TELEMETRY":                "false",
		"PYTHONUNBUFFERED":              "1",
	})
	process := startMemoryWorkerE2EProcess(t, python, repositoryRoot, workerEnv)

	initial := process.send(t, map[string]any{"action": "process", "limit": 100})
	if initial["event"] != "processed" || initial["claimed"] != float64(4) {
		t.Fatalf("initial worker pass did not claim both workspace index/extraction jobs: %#v", initial)
	}
	assertMemoryWorkerE2EProposal(t, store.DB(), "default", "source-event-e2e-target")
	assertMemoryWorkerE2EProposal(t, store.DB(), "e2e-other", "source-event-e2e-other")
	assertMemoryWorkerE2EAuthority(t, store.DB(), "default", "claim-e2e-target", "active", 1)
	assertMemoryWorkerE2EAuthority(t, store.DB(), "e2e-other", "claim-e2e-other", "active", 1)
	assertMemoryWorkerE2EState(t, process, "default", "claim-e2e-target", true)
	assertMemoryWorkerE2EState(t, process, "e2e-other", "claim-e2e-other", true)
	process.stop(t)

	// Reopen the persistent Mem0 history database through a fresh worker
	// process. Ownership metadata must survive this restart for scoped cleanup.
	process = startMemoryWorkerE2EProcess(t, python, repositoryRoot, workerEnv)
	assertMemoryWorkerE2EState(t, process, "default", "claim-e2e-target", true)
	assertMemoryWorkerE2EState(t, process, "e2e-other", "claim-e2e-other", true)

	// Requeue the already indexed target claim so the effect adapter can pause
	// after lease validation. The delete commits while that stale write is held.
	if _, err := store.DB().Exec(`UPDATE memory_jobs SET status='pending',attempts=0,available_at=?,lease_owner='',lease_until=NULL WHERE workspace_id='default' AND kind='index' AND entity_id='claim-e2e-target' AND entity_version=1 AND target='qdrant'`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(process.stdin, `{"action":"race_start"}`+"\n"); err != nil {
		t.Fatalf("start stale index effect race: %v", err)
	}
	ready := process.readEvent(t)
	if ready["event"] != "race_effect_validated" {
		t.Fatalf("unexpected stale effect race state: %#v", ready)
	}

	deleteTx, err := store.DB().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := memory.NewRepository(store.DB()).DeleteTx(context.Background(), deleteTx, memory.DeleteInput{
		WorkspaceID: "default", ActorID: "e2e-user", ClaimID: "claim-e2e-target", Expected: 1, ReasonCode: "user_requested",
	})
	if err != nil {
		_ = deleteTx.Rollback()
		t.Fatalf("tombstone target claim: %v", err)
	}
	if err := deleteTx.Commit(); err != nil {
		t.Fatal(err)
	}
	if deleted.Status != memory.ClaimTombstoned || deleted.Revision != 2 {
		t.Fatalf("unexpected tombstone result: status=%s revision=%d", deleted.Status, deleted.Revision)
	}
	principal := contracts.Principal{WorkspaceID: "default", SubjectID: "e2e-user", Scopes: []string{"memory:read:workspace"}}
	visible, err := memory.NewRepository(store.DB()).Search(context.Background(), principal, "juniper", memory.SearchFilter{ScopeKind: memory.ScopeWorkspace})
	if err != nil {
		t.Fatal(err)
	}
	if len(visible.Items) != 0 {
		t.Fatalf("tombstoned memory remained visible through Go authority search: %#v", visible.Items)
	}

	if _, err := io.WriteString(process.stdin, `{"action":"race_release"}`+"\n"); err != nil {
		t.Fatalf("release stale Qdrant effect: %v", err)
	}
	race := process.readEvent(t)
	completion, _ := race["completion"].(map[string]any)
	if race["event"] != "race_complete" || completion["disposition"] != "stale_generation" {
		t.Fatalf("Go authority did not repair the post-tombstone stale index effect: %#v", race)
	}
	var repairedCleanup int
	if err := store.DB().QueryRow(`SELECT count(*) FROM memory_jobs WHERE workspace_id='default' AND kind='delete_cleanup' AND entity_id='claim-e2e-target' AND entity_version=2 AND target='qdrant' AND status='pending'`).Scan(&repairedCleanup); err != nil {
		t.Fatal(err)
	}
	if repairedCleanup != 1 {
		t.Fatalf("stale index completion did not requeue Qdrant tombstone cleanup: %d", repairedCleanup)
	}

	cleanup := process.send(t, map[string]any{"action": "process", "limit": 100})
	if cleanup["event"] != "processed" {
		t.Fatalf("cleanup worker pass failed: %#v", cleanup)
	}
	assertMemoryWorkerE2EAuthority(t, store.DB(), "default", "claim-e2e-target", "tombstoned", 2)
	assertMemoryWorkerE2EState(t, process, "default", "claim-e2e-target", false)
	assertMemoryWorkerE2EState(t, process, "e2e-other", "claim-e2e-other", true)
	for _, target := range []string{"mem0", "qdrant"} {
		var status string
		if err := store.DB().QueryRow(`SELECT status FROM memory_jobs WHERE workspace_id='default' AND kind='delete_cleanup' AND entity_id='claim-e2e-target' AND entity_version=2 AND target=?`, target).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "done" {
			t.Fatalf("%s cleanup job status=%s, want done", target, status)
		}
	}
	process.stop(t)
}

func memoryE2EResponsesStub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
		http.NotFound(w, r)
		return
	}
	request, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	text := "Synthetic device fixture is stored in a test cabinet."
	if strings.Contains(strings.ToLower(string(request)), "juniper-731") {
		text = "Device juniper-731 is stored in cabinet amber-47."
	} else if strings.Contains(strings.ToLower(string(request)), "orchid-284") {
		text = "Device orchid-284 is stored in cabinet violet-19."
	}
	outputText, _ := json.Marshal(map[string]any{"memory": []map[string]string{{"text": text}}})
	response := map[string]any{
		"id": "resp-cylo-e2e", "object": "response", "created_at": time.Now().Unix(),
		"status": "completed", "model": "e2e-responses-stub", "output_text": string(outputText),
		"output": []map[string]any{{
			"id": "msg-cylo-e2e", "type": "message", "status": "completed", "role": "assistant",
			"content": []map[string]any{{"type": "output_text", "text": string(outputText), "annotations": []any{}}},
		}},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func startMemoryWorkerE2EProcess(t *testing.T, python, repositoryRoot string, env []string) *memoryWorkerE2EProcess {
	t.Helper()
	cmd := exec.Command(python, filepath.Join(repositoryRoot, "services", "memory", "tests", "e2e_worker.py"))
	cmd.Dir = repositoryRoot
	cmd.Env = env
	cmd.Stderr = io.Discard // Never persist provider error bodies that could echo deployment details.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start Python memory worker E2E subprocess: %v", err)
	}
	process := &memoryWorkerE2EProcess{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}
	t.Cleanup(func() {
		if !process.ended && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	if event := process.readEvent(t); event["event"] != "ready" {
		t.Fatalf("unexpected Python worker startup event: %v", event["event"])
	}
	return process
}

func startEphemeralMemoryE2EQdrant(t *testing.T) string {
	t.Helper()
	output, err := exec.Command("docker", "run", "--rm", "-d", "-p", "127.0.0.1::6333", "qdrant/qdrant:v1.19.1").Output()
	if err != nil {
		t.Fatalf("start pinned local Qdrant E2E container: %v", err)
	}
	containerID := strings.TrimSpace(string(output))
	t.Cleanup(func() { _ = exec.Command("docker", "stop", containerID).Run() })
	portOutput, err := exec.Command("docker", "port", containerID, "6333/tcp").Output()
	if err != nil {
		t.Fatalf("read ephemeral Qdrant port: %v", err)
	}
	address := strings.TrimSpace(string(portOutput))
	colon := strings.LastIndex(address, ":")
	if colon < 0 {
		t.Fatalf("Docker returned an invalid Qdrant port mapping")
	}
	port := address[colon+1:]
	url := "http://127.0.0.1:" + port
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(url + "/collections")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return url
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("ephemeral local Qdrant did not become ready at %s", url)
	return ""
}

func seedMemoryWorkerE2EClaim(t *testing.T, db *sql.DB, workspace, claimID, eventID, sourceText string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if workspace != "default" {
		if _, err := db.ExecContext(ctx, `INSERT INTO workspaces(id,name,created_at) VALUES(?,?,?)`, workspace, workspace, now); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO event_counters(workspace_id,current_cursor) VALUES(?,0)`, workspace); err != nil {
			t.Fatal(err)
		}
	}
	payload, err := json.Marshal(map[string]string{"source": sourceText})
	if err != nil {
		t.Fatal(err)
	}
	var cursor int64
	if err := db.QueryRowContext(ctx, `SELECT current_cursor+1 FROM event_counters WHERE workspace_id=?`, workspace).Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE event_counters SET current_cursor=? WHERE workspace_id=?`, cursor, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO events(event_id,workspace_id,cursor,schema_version,event_type,occurred_at,subject_id,aggregate_type,aggregate_id,aggregate_version,payload) VALUES(?,?,?,1,'memory.e2e_source',?,NULL,'workspace',?,1,?)`, eventID, workspace, cursor, now, workspace, payload); err != nil {
		t.Fatal(err)
	}
	sourceID := "source-" + eventID
	digest := sha256.Sum256(payload)
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_sources(workspace_id,id,version,scope_kind,scope_id,principal_id,kind,reference,content_hash,recorded_at,producer) VALUES(?,?,1,'workspace','','','event',?,?,?,'user')`, workspace, sourceID, "event:"+eventID, hex.EncodeToString(digest[:]), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_claims(id,workspace_id,scope_kind,scope_id,principal_id,subject,claim_key,authority_kind,status,revision,created_at,updated_at) VALUES(?,?,'workspace','','','device','storage_location','user_confirmed','active',1,?,?)`, claimID, workspace, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_revisions(claim_id,revision,content,authority_kind,status,recorded_at,actor_id) VALUES(?,1,?,'user_confirmed','active',?,'e2e-user')`, claimID, "Synthetic authority fixture for the memory worker acceptance test.", now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_revision_sources(claim_id,revision,workspace_id,source_id,source_version) VALUES(?,1,?,?,1)`, claimID, workspace, sourceID); err != nil {
		t.Fatal(err)
	}
}

func memoryE2EEnv(current []string, overrides map[string]string) []string {
	values := make(map[string]string, len(current)+len(overrides))
	allowed := map[string]bool{
		"PATH": true, "HOME": true, "TMPDIR": true, "LANG": true, "LC_ALL": true,
		"SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
		"HTTPS_PROXY": true, "HTTP_PROXY": true, "NO_PROXY": true, "ALL_PROXY": true,
		"https_proxy": true, "http_proxy": true, "no_proxy": true, "all_proxy": true,
	}
	for _, entry := range current {
		name, value, ok := strings.Cut(entry, "=")
		if ok && allowed[name] {
			values[name] = value
		}
	}
	for name, value := range overrides {
		if value == "" {
			delete(values, name)
		} else {
			values[name] = value
		}
	}
	result := make([]string, 0, len(values))
	for name, value := range values {
		result = append(result, name+"="+value)
	}
	return result
}

func assertMemoryWorkerE2EProposal(t *testing.T, db *sql.DB, workspace, sourceID string) {
	t.Helper()
	var status, content, storedSourceID string
	var sourceVersion int64
	err := db.QueryRow(`SELECT p.status,p.content,p.source_id,p.source_version FROM memory_proposals p WHERE p.workspace_id=? AND p.source_id=?`, workspace, sourceID).Scan(&status, &content, &storedSourceID, &sourceVersion)
	if err != nil {
		t.Fatalf("read proposal provenance for %s: %v", workspace, err)
	}
	if status != "pending" || strings.TrimSpace(content) == "" || storedSourceID != sourceID || sourceVersion != 1 {
		t.Fatalf("invalid E2E proposal/provenance: workspace=%s status=%q content_present=%t source=%q source_version=%d", workspace, status, content != "", storedSourceID, sourceVersion)
	}
}

func assertMemoryWorkerE2EAuthority(t *testing.T, db *sql.DB, workspace, claimID, status string, revision int64) {
	t.Helper()
	var gotStatus string
	var gotRevision int64
	if err := db.QueryRow(`SELECT status,revision FROM memory_claims WHERE workspace_id=? AND id=?`, workspace, claimID).Scan(&gotStatus, &gotRevision); err != nil {
		t.Fatalf("read authoritative claim: %v", err)
	}
	if gotStatus != status || gotRevision != revision {
		t.Fatalf("authority claim workspace=%s id=%s status=%s revision=%d, want status=%s revision=%d", workspace, claimID, gotStatus, gotRevision, status, revision)
	}
}

func assertMemoryWorkerE2EState(t *testing.T, process *memoryWorkerE2EProcess, workspace, claimID string, exists bool) {
	t.Helper()
	state := process.send(t, map[string]any{"action": "state", "workspace_id": workspace, "claim_id": claimID})
	if state["event"] != "state" {
		t.Fatalf("unexpected worker state response: %#v", state)
	}
	for _, key := range []string{"mem0_vectors", "history", "messages", "qdrant_vectors"} {
		count, ok := state[key].(float64)
		if !ok || (exists && count < 1) || (!exists && count != 0) {
			t.Fatalf("workspace=%s claim=%s %s=%v, exists=%t", workspace, claimID, key, state[key], exists)
		}
	}
}

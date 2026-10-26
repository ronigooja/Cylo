package control

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/agents/ao"
	"github.com/ronigooja/Cylo/internal/runner"
	"github.com/ronigooja/Cylo/internal/storage"
)

type dispatchRecoveryAO struct{ creates int }

func (a *dispatchRecoveryAO) CreateWorker(context.Context, ao.WorkerSpec) (ao.Session, error) {
	a.creates++
	return ao.Session{ID: "unexpected-session"}, nil
}
func (*dispatchRecoveryAO) GetSession(context.Context, string) (ao.Session, error) {
	return ao.Session{}, errors.New("unexpected AO session lookup")
}
func (*dispatchRecoveryAO) GetConversation(context.Context, string) (ao.Conversation, error) {
	return ao.Conversation{}, errors.New("unexpected AO conversation lookup")
}
func (*dispatchRecoveryAO) StopWorker(context.Context, string) (ao.Session, ao.KillResponse, error) {
	return ao.Session{}, ao.KillResponse{}, errors.New("unexpected AO stop")
}

func TestUncertainTaskDispatchRemainsNonDispatchableWithoutMutation(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO actors(id,workspace_id,subject_id,name,status,created_at) VALUES('device','default','owner','test','approved',?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(store, Config{})
	if err != nil {
		t.Fatal(err)
	}
	guardCalled := false
	if err := server.RegisterTaskDispatchGuard("service", func(context.Context, *sql.Tx, string, string, time.Time) (string, error) {
		guardCalled = true
		return "grant_revoked", nil
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO tasks(id,workspace_id,title,goal,source,profile_id,status,version,created_at,updated_at,created_by_device_id,command_id) VALUES('task','default','test','test','service','profile','active',1,?,?, 'device','command')`, at, at); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO runs(id,workspace_id,task_id,attempt_id,status,desired_control,fence,runner_id,authorized_device_id,command_id,goal,profile_id,runtime_route_id,model_route_id,role,started_at) VALUES('run','default','task','attempt','uncertain','run',1,'runner','device','command','test','profile','runtime','model','worker',?)`, at); err != nil {
		t.Fatal(err)
	}
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := server.TaskDispatchAuthorizationTx(ctx, tx, "run", time.Now().UTC())
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if result.Allowed || result.ReasonCode != "reconciliation_unavailable" {
		t.Fatalf("uncertain run authorization = %+v, want reconciliation-only denial", result)
	}
	if guardCalled {
		t.Fatal("uncertain run reached the new-dispatch grant guard")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var status, desired string
	if err := store.DB().QueryRowContext(ctx, `SELECT status,desired_control FROM runs WHERE id='run'`).Scan(&status, &desired); err != nil {
		t.Fatal(err)
	}
	if status != "uncertain" || desired != "run" {
		t.Fatalf("uncertain run mutated to status=%q desired_control=%q", status, desired)
	}
}

func TestProductionControlClientRechecksLiveTaskAuthorizationOverHTTP(t *testing.T) {
	ctx := context.Background()
	runnerToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	store, err := storage.Open(ctx, storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO actors(id,workspace_id,subject_id,name,status,created_at) VALUES('device','default','owner','test','approved',?)`, at); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO tasks(id,workspace_id,title,goal,source,profile_id,status,version,created_at,updated_at,created_by_device_id,command_id) VALUES('task','default','test','test','user','profile','active',1,?,?, 'device','command')`, at, at); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO runs(id,workspace_id,task_id,attempt_id,status,desired_control,fence,runner_id,authorized_device_id,command_id,goal,profile_id,runtime_route_id,model_route_id,role,started_at) VALUES('run','default','task','attempt','active','run',1,'runner','device','command','test','profile','runtime','model','worker',?)`, at); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(store, Config{RunnerToken: runnerToken, RunnerID: "runner", RunnerWorkspaceIDs: []string{"default"}, RunnerScopes: []string{"runner:claim"}})
	if err != nil {
		t.Fatal(err)
	}
	guardCalled := false
	if err := server.RegisterTaskDispatchGuard("service", func(context.Context, *sql.Tx, string, string, time.Time) (string, error) {
		guardCalled = true
		return "grant_revoked", nil
	}); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	client, err := runner.NewControlClient(httpServer.URL, runnerToken, httpServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	allowed, reason, err := client.CheckDispatchAuthorization(ctx, "run")
	if err != nil || !allowed || reason != "" {
		t.Fatalf("user dispatch check = allowed:%v reason:%q err:%v", allowed, reason, err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE tasks SET source='service' WHERE id='task'`); err != nil {
		t.Fatal(err)
	}
	allowed, reason, err = client.CheckDispatchAuthorization(ctx, "run")
	if err != nil || allowed || reason != "grant_revoked" {
		t.Fatalf("revoked dispatch check = allowed:%v reason:%q err:%v", allowed, reason, err)
	}
	var desired string
	if err := store.DB().QueryRowContext(ctx, `SELECT desired_control FROM runs WHERE id='run'`).Scan(&desired); err != nil || desired != "stop" {
		t.Fatalf("revoked active run desired_control=%q err=%v", desired, err)
	}
	guardCalled = false
	if _, err := store.DB().ExecContext(ctx, `UPDATE runs SET status='uncertain',desired_control='run' WHERE id='run'`); err != nil {
		t.Fatal(err)
	}
	allowed, reason, err = client.CheckDispatchAuthorization(ctx, "run")
	if err != nil || allowed || reason != "reconciliation_unavailable" {
		t.Fatalf("uncertain recovery check = allowed:%v reason:%q err:%v", allowed, reason, err)
	}
	if guardCalled {
		t.Fatal("uncertain recovery reached a dispatch grant or create authorization")
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT desired_control FROM runs WHERE id='run'`).Scan(&desired); err != nil || desired != "run" {
		t.Fatalf("uncertain dispatch check changed desired_control=%q err=%v", desired, err)
	}
}

func TestProductionHTTPRecoveryDoesNotReplayPersistedAOCreateAttempt(t *testing.T) {
	ctx := context.Background()
	runnerToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x43}, 32))
	store, err := storage.Open(ctx, storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO actors(id,workspace_id,subject_id,name,status,created_at) VALUES('device','default','owner','test','approved',?)`, at); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO tasks(id,workspace_id,title,goal,source,profile_id,status,version,created_at,updated_at,created_by_device_id,command_id) VALUES('task','default','test','test','user','profile','active',1,?,?, 'device','command')`, at, at); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO runs(id,workspace_id,task_id,attempt_id,status,desired_control,fence,runner_id,authorized_device_id,command_id,goal,profile_id,runtime_route_id,model_route_id,role,started_at) VALUES('run','default','task','attempt','active','run',1,'runner','device','command','test','profile','runtime','model','worker',?)`, at); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(store, Config{RunnerToken: runnerToken, RunnerID: "runner", RunnerWorkspaceIDs: []string{"default"}, RunnerScopes: []string{"runner:claim", "runner:observe", "runner:control:read"}})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	control, err := runner.NewControlClient(httpServer.URL, runnerToken, httpServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	claim := contracts.JobClaim{RunID: "run", TaskID: "task", AttemptID: "attempt", WorkspaceID: "default", Fence: 1, Goal: "test", ProfileID: "profile", Harness: "codex", RuntimeRouteID: "runtime", ModelRouteID: "model", IsolationPolicy: "ao-codex-workspace-write-v1", Role: "worker", AOProjectID: "project"}
	journal, err := runner.OpenJournal(ctx, t.TempDir()+"/runner.db")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	spec := ao.WorkerSpec{ClientRequestID: claim.RunID, ProjectID: claim.AOProjectID, Harness: claim.Harness, DisplayName: "original", Prompt: "original request body"}
	if _, err := journal.SaveClaim(ctx, claim, spec, "create_pending"); err != nil {
		t.Fatal(err)
	}
	agent := &dispatchRecoveryAO{}
	engine := runner.Engine{Control: control, AO: agent, Journal: journal}
	if err := engine.Process(ctx, claim); !errors.Is(err, runner.ErrOutcomePending) {
		t.Fatalf("recovery error=%v, want fenced uncertain result", err)
	}
	if agent.creates != 0 {
		t.Fatalf("recovered create_pending journal made %d AO create POSTs", agent.creates)
	}
	var status string
	if err := store.DB().QueryRowContext(ctx, `SELECT status FROM runs WHERE id='run'`).Scan(&status); err != nil || status != "uncertain" {
		t.Fatalf("recovered run status=%q err=%v, want uncertain", status, err)
	}
	allowed, reason, err := control.CheckDispatchAuthorization(ctx, claim.RunID)
	if err != nil || allowed || reason != "reconciliation_unavailable" {
		t.Fatalf("uncertain run dispatch check = allowed:%v reason:%q err:%v", allowed, reason, err)
	}
}

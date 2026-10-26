package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
)

func TestReconciliationObservationValidatesAbsenceAndReleasesRun(t *testing.T) {
	for _, scenario := range []string{"complete", "incomplete-ao", "container", "wrong-epoch", "unconfirmed", "session-bound", "unsafe-effect", "ordinary-proof", "missing-incarnation", "old-incarnation", "future-incarnation"} {
		t.Run(scenario, func(t *testing.T) {
			server, closeStore := budgetTestServer(t, contracts.RunBudgetSnapshot{})
			defer closeStore()
			if err := RegisterInstanceMaintenanceMigration(server.store); err != nil {
				t.Fatal(err)
			}
			if err := server.store.Migrate(context.Background()); err != nil {
				t.Fatal(err)
			}
			state, err := instanceStatus(t, server.store)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = server.store.DB().Exec(`UPDATE runs SET status='uncertain' WHERE id='budget-run'`); err != nil {
				t.Fatal(err)
			}
			proof := &contracts.NoEffectProof{JournalPhase: "effect_unknown", AOCreateStarted: true, Outcome: "failed", Reconciliation: &contracts.NoEffectReconciliation{AOServerStartedAt: time.Now().UTC().Add(-time.Second), JournalUpdatedAt: time.Now().UTC().Add(-2 * time.Second), RunID: "budget-run", Actor: "operator", Confirmed: true, VerifiedAt: time.Now().UTC(), AOQuery: "GET /api/v1/sessions/inventory", AOInventoryComplete: true, AOClientRequestIDComplete: true, DockerQuery: "docker ps -aq --no-trunc; docker inspect all IDs", DockerInventoryComplete: true, InstanceID: state.InstanceID, InstanceEpoch: state.Epoch}}
			observe := func(obs contracts.RunnerObservation) *httptest.ResponseRecorder {
				body, _ := json.Marshal(obs)
				req := httptest.NewRequest("POST", "/runner/v1/runs/budget-run/observations", bytes.NewReader(body))
				req.SetPathValue("run_id", "budget-run")
				req.Header.Set("Idempotency-Key", obs.ObservationID)
				req = req.WithContext(context.WithValue(req.Context(), runnerPrincipalKey{}, contracts.RunnerPrincipal{RunnerID: "budget-runner", WorkspaceIDs: []string{"default"}, Scopes: []string{"runner:observe"}}))
				w := httptest.NewRecorder()
				server.handleObservation(w, req)
				return w
			}
			unknown := contracts.RunnerObservation{ObservationID: "unknown", RunID: "budget-run", AttemptID: "budget-attempt", Fence: 91, Kind: "effect_unknown", ExternalEffects: []contracts.ExternalEffect{{ID: "create", Kind: "ao_worker_create", Status: "unknown"}}}
			if w := observe(unknown); w.Code != 200 {
				t.Fatalf("unknown: %d %s", w.Code, w.Body.String())
			}
			switch scenario {
			case "missing-incarnation":
				proof.Reconciliation.AOServerStartedAt = time.Time{}
			case "old-incarnation":
				proof.Reconciliation.AOServerStartedAt = proof.Reconciliation.JournalUpdatedAt.Add(-time.Second)
			case "future-incarnation":
				proof.Reconciliation.AOServerStartedAt = proof.Reconciliation.VerifiedAt.Add(time.Second)
			case "incomplete-ao":
				proof.Reconciliation.AOInventoryComplete = false
			case "container":
				proof.Reconciliation.DockerRuntimeCount = 1
			case "wrong-epoch":
				proof.Reconciliation.InstanceEpoch++
			case "unconfirmed":
				proof.Reconciliation.Confirmed = false
			case "ordinary-proof":
				proof.Reconciliation = nil
				proof.JournalPhase = "claimed"
				proof.AOCreateStarted = false
			case "session-bound":
				if _, err = server.store.DB().Exec(`UPDATE runs SET ao_session_id='session' WHERE id='budget-run'`); err != nil {
					t.Fatal(err)
				}
			case "unsafe-effect":
				if _, err = server.store.DB().Exec(`UPDATE external_effects SET kind='send' WHERE run_id='budget-run'`); err != nil {
					t.Fatal(err)
				}
			}
			obs := contracts.RunnerObservation{ObservationID: "reconciled", RunID: "budget-run", AttemptID: "budget-attempt", Fence: 91, Kind: "no_effect_terminal", NoEffectProof: proof, Error: "No runtime exists"}
			w := observe(obs)
			var runStatus, effectStatus string
			if err = server.store.DB().QueryRow(`SELECT status FROM runs WHERE id='budget-run'`).Scan(&runStatus); err != nil {
				t.Fatal(err)
			}
			if err = server.store.DB().QueryRow(`SELECT status FROM external_effects WHERE id='create'`).Scan(&effectStatus); err != nil {
				t.Fatal(err)
			}
			if scenario != "complete" {
				if w.Code != 409 || runStatus != "uncertain" || effectStatus != "unknown" {
					t.Fatalf("unsafe proof accepted: %d %s run=%s effect=%s", w.Code, w.Body.String(), runStatus, effectStatus)
				}
				return
			}
			if w.Code != 200 || runStatus != "failed" || effectStatus != "no_effect" {
				t.Fatalf("valid reconciliation: %d %s run=%s effect=%s", w.Code, w.Body.String(), runStatus, effectStatus)
			}
			if replay := observe(obs); replay.Code != 200 {
				t.Fatalf("lost ack replay: %d %s", replay.Code, replay.Body.String())
			}
			var receipts int
			if err = server.store.DB().QueryRow(`SELECT count(*) FROM runner_observations WHERE kind='no_effect_terminal'`).Scan(&receipts); err != nil || receipts != 1 {
				t.Fatalf("receipt count=%d err=%v", receipts, err)
			}
		})
	}
}

func TestWorkerBootRejectionReleasesCapacityOnlyForStable409Proof(t *testing.T) {
	for _, status := range []int{409, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server, closeStore := budgetTestServer(t, contracts.RunBudgetSnapshot{})
			defer closeStore()
			if _, err := server.store.DB().Exec(`UPDATE runs SET status='dispatching',ao_project_id='project' WHERE id='budget-run'`); err != nil {
				t.Fatal(err)
			}
			observe := func(obs contracts.RunnerObservation) *httptest.ResponseRecorder {
				body, err := json.Marshal(obs)
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest("POST", "/runner/v1/runs/budget-run/observations", bytes.NewReader(body))
				req.SetPathValue("run_id", "budget-run")
				req.Header.Set("Idempotency-Key", obs.ObservationID)
				req = req.WithContext(context.WithValue(req.Context(), runnerPrincipalKey{}, contracts.RunnerPrincipal{RunnerID: "budget-runner", WorkspaceIDs: []string{"default"}, Scopes: []string{"runner:observe"}}))
				w := httptest.NewRecorder()
				server.handleObservation(w, req)
				return w
			}
			started := contracts.RunnerObservation{ObservationID: "started", RunID: "budget-run", AttemptID: "budget-attempt", Fence: 91, Kind: "dispatch_started", AOProjectID: "project"}
			if w := observe(started); w.Code != 200 {
				t.Fatalf("dispatch_started: %d %s", w.Code, w.Body.String())
			}
			rejected := contracts.RunnerObservation{ObservationID: "rejected", RunID: "budget-run", AttemptID: "budget-attempt", Fence: 91, Kind: "create_rejected", AOProjectID: "project", CreateRejection: &contracts.CreateRejectionProof{HTTPStatus: status, ErrorCode: "worker_boot_failed"}}
			w := observe(rejected)
			var runStatus, taskStatus string
			if err := server.store.DB().QueryRow(`SELECT runs.status,tasks.status FROM runs JOIN tasks ON tasks.id=runs.task_id WHERE runs.id='budget-run'`).Scan(&runStatus, &taskStatus); err != nil {
				t.Fatal(err)
			}
			if status == 503 {
				if w.Code != 409 || runStatus != "active" || taskStatus != "active" {
					t.Fatalf("untrusted 503 released run: %d %s run=%s task=%s", w.Code, w.Body.String(), runStatus, taskStatus)
				}
				unknown := contracts.RunnerObservation{ObservationID: "unknown", RunID: "budget-run", AttemptID: "budget-attempt", Fence: 91, Kind: "effect_unknown", ExternalEffects: []contracts.ExternalEffect{{ID: "create", Kind: "ao_worker_create", Status: "unknown"}}}
				if w := observe(unknown); w.Code != 200 {
					t.Fatalf("unknown: %d %s", w.Code, w.Body.String())
				}
			} else {
				if w.Code != 200 || runStatus != "failed" || taskStatus != "failed" {
					t.Fatalf("stable rejection: %d %s run=%s task=%s", w.Code, w.Body.String(), runStatus, taskStatus)
				}
				if replay := observe(rejected); replay.Code != 200 {
					t.Fatalf("rejection receipt replay: %d %s", replay.Code, replay.Body.String())
				}
			}
			var occupied, receipts int
			if err := server.store.DB().QueryRow(`SELECT count(*) FROM runs WHERE status IN ('dispatching','active','uncertain')`).Scan(&occupied); err != nil {
				t.Fatal(err)
			}
			if err := server.store.DB().QueryRow(`SELECT count(*) FROM runner_observations WHERE kind='create_rejected'`).Scan(&receipts); err != nil {
				t.Fatal(err)
			}
			if status == 409 && (occupied != 0 || receipts != 1) {
				t.Fatalf("rejection leaked capacity or duplicated receipt: slots=%d receipts=%d", occupied, receipts)
			}
			if status == 503 {
				if err := server.store.DB().QueryRow(`SELECT status FROM runs WHERE id='budget-run'`).Scan(&runStatus); err != nil {
					t.Fatal(err)
				}
				if occupied != 1 || receipts != 0 || runStatus != "uncertain" {
					t.Fatalf("ambiguous 503 lost fencing: slots=%d receipts=%d status=%s", occupied, receipts, runStatus)
				}
			}
		})
	}
}

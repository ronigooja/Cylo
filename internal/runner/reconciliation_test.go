package runner

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/agents/ao"
)

type reconciliationControl struct {
	*fakeControl
	claim contracts.JobClaim
}

func (c reconciliationControl) Current(context.Context) ([]contracts.JobClaim, error) {
	return []contracts.JobClaim{c.claim}, nil
}

type reconciliationInstance struct{ epoch int64 }

func (i reconciliationInstance) Instance(context.Context) (InstanceState, error) {
	return InstanceState{InstanceID: "instance", Epoch: i.epoch}, nil
}

type reconciliationInventory struct {
	inventory ao.SessionInventory
	calls     int
}

func (i *reconciliationInventory) ListSessions(context.Context) (ao.SessionInventory, error) {
	i.calls++
	return i.inventory, nil
}

type reconciliationDocker struct {
	runtime bool
	fail    bool
	calls   int
}

func (d *reconciliationDocker) Run(_ context.Context, args ...string) ([]byte, error) {
	d.calls++
	if d.fail {
		return nil, errors.New("unavailable")
	}
	if args[0] == "ps" {
		if d.runtime {
			return []byte(strings.Repeat("a", 64)), nil
		}
		return nil, nil
	}
	if args[0] != "inspect" {
		return nil, errors.New("mutation attempted")
	}
	return json.Marshal(map[string]any{"Id": strings.Repeat("a", 64), "Config": map[string]any{"Labels": map[string]string{aoContainerRuntimeLabel: aoContainerCodexRuntime}}})
}
func prepareReconciliation(t *testing.T) (*Engine, *fakeControl, *reconciliationInventory, *reconciliationDocker) {
	t.Helper()
	ctx := context.Background()
	claim := validClaim()
	e, c := makeEngine(t, claim, contracts.RunControl{}, &fakeAO{}, nil, nil)
	e.Control = reconciliationControl{c, claim}
	e.Instance = reconciliationInstance{1}
	if _, err := e.Journal.SaveClaim(ctx, claim, ao.WorkerSpec{ClientRequestID: claim.RunID, ProjectID: claim.AOProjectID, Harness: "codex"}, "effect_unknown"); err != nil {
		t.Fatal(err)
	}
	if err := e.Journal.SetInstance(ctx, claim.RunID, claim.AttemptID, claim.Fence, "instance", 1); err != nil {
		t.Fatal(err)
	}
	return e, c, &reconciliationInventory{inventory: ao.SessionInventory{Complete: true, ClientRequestIDComplete: true, ServerStartedAt: time.Now().UTC()}}, &reconciliationDocker{}
}
func TestExplicitReconciliationReplaysExactDurableReceipt(t *testing.T) {
	e, c, inventory, docker := prepareReconciliation(t)
	ctx := context.Background()
	c.loseFirstObservationAck = true
	if err := e.ReconcileNoEffect(ctx, []string{"run-1"}, "operator", true, inventory, docker); err == nil {
		t.Fatal("expected lost acknowledgement")
	}
	if len(c.observations) != 1 {
		t.Fatal(c.observations)
	}
	first := c.observations[0]
	if first.NoEffectProof.Reconciliation.Actor != "operator" || first.NoEffectProof.Outcome != "failed" {
		t.Fatal(first)
	}
	if err := e.ReconcileNoEffect(ctx, []string{"run-1"}, "second-operator", true, inventory, docker); err != nil {
		t.Fatal(err)
	}
	if len(c.observations) != 2 || !reflect.DeepEqual(first, c.observations[1]) || inventory.calls != 1 {
		t.Fatal("retry changed proof or repeated inventories")
	}
	if err := e.ReconcileNoEffect(ctx, []string{"run-1"}, "operator", true, inventory, docker); err != nil {
		t.Fatal(err)
	}
	if len(c.observations) != 2 {
		t.Fatal("acknowledged receipt was duplicated")
	}
	if err := e.Process(ctx, validClaim()); err != nil {
		t.Fatal(err)
	}
	if e.AO.(*fakeAO).createN != 0 {
		t.Fatal("created a worker during receipt recovery")
	}
}
func TestExplicitReconciliationFailsClosed(t *testing.T) {
	for _, name := range []string{"confirmation", "actor", "incomplete", "legacy-session", "matched-session", "container", "docker-error", "epoch", "bound", "wrong-phase", "missing-incarnation", "old-incarnation", "future-incarnation", "nonutc-incarnation"} {
		t.Run(name, func(t *testing.T) {
			e, c, inventory, docker := prepareReconciliation(t)
			ctx := context.Background()
			actor := "operator"
			confirm := true
			switch name {
			case "missing-incarnation":
				inventory.inventory.ServerStartedAt = time.Time{}
			case "old-incarnation":
				inventory.inventory.ServerStartedAt = time.Now().UTC().Add(-time.Hour)
			case "future-incarnation":
				inventory.inventory.ServerStartedAt = time.Now().UTC().Add(time.Hour)
			case "nonutc-incarnation":
				inventory.inventory.ServerStartedAt = time.Now().In(time.FixedZone("offset", 3600))
			case "confirmation":
				confirm = false
			case "actor":
				actor = ""
			case "incomplete":
				inventory.inventory.Complete = false
			case "legacy-session":
				inventory.inventory.Sessions = []ao.Session{{ID: "legacy"}}
			case "matched-session":
				inventory.inventory.Sessions = []ao.Session{{ID: "session", ClientRequestID: "run-1"}}
			case "container":
				docker.runtime = true
			case "docker-error":
				docker.fail = true
			case "epoch":
				e.Instance = reconciliationInstance{2}
			case "bound":
				if err := e.Journal.SetSession(ctx, "run-1", "attempt-1", 7, "session", "effect_unknown"); err != nil {
					t.Fatal(err)
				}
			case "wrong-phase":
				if err := e.Journal.SetPhase(ctx, "run-1", "attempt-1", 7, "create_pending"); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.ReconcileNoEffect(ctx, []string{"run-1"}, actor, confirm, inventory, docker); err == nil {
				t.Fatal("unsafe reconciliation accepted")
			}
			if name == "container" || name == "epoch" || name == "old-incarnation" {
				var body, actor string
				if err := e.Journal.db.QueryRow(`SELECT result_json,actor FROM journal_reconciliations WHERE run_id='run-1'`).Scan(&body, &actor); err != nil {
					t.Fatal(err)
				}
				var audit map[string]any
				if err := json.Unmarshal([]byte(body), &audit); err != nil {
					t.Fatal(err)
				}
				if audit["outcome"] != "refused" || actor != "operator" || audit["attempt_id"] != "attempt-1" || audit["fence"] != float64(7) || audit["instance_id"] != "instance" || audit["instance_epoch"] != float64(1) || audit["journal_updated_at"] == nil {
					t.Fatalf("missing durable refused identity evidence: %s", body)
				}
				if name == "container" && !strings.Contains(body, "container exists") {
					t.Fatalf("missing refused container result: %s", body)
				}
			}
			if len(c.observations) != 0 || e.AO.(*fakeAO).createN != 0 {
				t.Fatal("unsafe reconciliation caused effects")
			}
		})
	}
}

package runner

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
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
	return json.Marshal(map[string]any{"Id": strings.Repeat("a", 64), "Name": "/ao-codex", "Created": time.Now().UTC().Add(-time.Hour), "Config": map[string]any{"Image": "ao-codex", "Labels": map[string]string{aoContainerRuntimeLabel: aoContainerCodexRuntime}}, "State": map[string]any{"Status": "created", "OOMKilled": false, "ExitCode": 0, "Error": "", "Running": false, "Paused": false, "Restarting": false, "Dead": false, "Pid": 0, "StartedAt": time.Time{}, "FinishedAt": time.Time{}}})
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

type completeInventoryDocker struct {
	containers map[string]map[string]any
	round      int
	change     func(int, map[string]map[string]any)
	mutations  int
}

func (d *completeInventoryDocker) Run(_ context.Context, args ...string) ([]byte, error) {
	switch args[0] {
	case "ps":
		d.round++
		if d.change != nil {
			d.change(d.round, d.containers)
		}
		ids := []string{}
		for id := range d.containers {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return []byte(strings.Join(ids, "\n")), nil
	case "inspect":
		body, ok := d.containers[args[len(args)-1]]
		if !ok {
			return nil, ErrDockerObjectNotFound
		}
		return json.Marshal(body)
	default:
		d.mutations++
		return nil, errors.New("unexpected Docker mutation")
	}
}
func neverStartedContainerFixture(id string) map[string]any {
	return map[string]any{"Id": id, "Name": "/preserved-ao", "Created": time.Now().UTC().Add(-time.Hour), "RestartCount": 0, "Config": map[string]any{"Image": "cylo/ao-codex", "Labels": map[string]string{aoContainerManagedLabel: "true", aoContainerRuntimeLabel: aoContainerCodexRuntime, aoContainerSessionLabel: "legacy-session"}}, "HostConfig": map[string]any{"RestartPolicy": map[string]any{"Name": "no"}}, "State": map[string]any{"Status": "created", "OOMKilled": false, "ExitCode": 0, "Error": "", "Running": false, "Paused": false, "Restarting": false, "Dead": false, "Pid": 0, "StartedAt": time.Time{}, "FinishedAt": time.Time{}}}
}
func preexistingCapabilityFixture(id string) map[string]any {
	labels := map[string]string{"io.cylo.managed": "true", "io.cylo.workspace": strings.Repeat("1", 24), "io.cylo.deployment": strings.Repeat("2", 24), "io.cylo.attempt": strings.Repeat("3", 24), "io.cylo.release": strings.Repeat("4", 64), "io.cylo.fence": "1"}
	return map[string]any{"Id": id, "Name": "/cylo-cap-" + labels["io.cylo.deployment"], "Created": time.Now().UTC().Add(-24 * time.Hour), "RestartCount": 0, "Config": map[string]any{"Image": "python@sha256:" + strings.Repeat("5", 64), "Labels": labels}, "HostConfig": map[string]any{"RestartPolicy": map[string]any{"Name": "unless-stopped"}}, "State": map[string]any{"Status": "running", "Running": true, "Paused": false, "Restarting": false, "Dead": false, "Pid": 42, "StartedAt": time.Now().UTC().Add(-time.Hour), "FinishedAt": time.Time{}}}
}
func recordOriginalDispatch(t *testing.T, e *Engine) {
	t.Helper()
	claim := validClaim()
	obs := contracts.RunnerObservation{ObservationID: "original-dispatch", RunID: claim.RunID, AttemptID: claim.AttemptID, Fence: claim.Fence, Kind: "dispatch_started", OccurredAt: time.Now().UTC().Add(-30 * time.Minute)}
	if err := e.Journal.QueueObservation(context.Background(), claim.RunID, obs); err != nil {
		t.Fatal(err)
	}
	if err := e.Journal.AcknowledgeObservation(context.Background(), obs.ObservationID); err != nil {
		t.Fatal(err)
	}
}
func TestReconciliationPreservesSelectedNeverStartedRecordAndPreexistingCapability(t *testing.T) {
	e, c, inventory, _ := prepareReconciliation(t)
	recordOriginalDispatch(t, e)
	retainedID := strings.Repeat("a", 64)
	capabilityID := strings.Repeat("b", 64)
	docker := &completeInventoryDocker{containers: map[string]map[string]any{retainedID: neverStartedContainerFixture(retainedID), capabilityID: preexistingCapabilityFixture(capabilityID)}}
	before, _ := json.Marshal(docker.containers)
	opts := ReconciliationOptions{RetainNeverStartedContainerIDs: []string{retainedID}, UnrelatedPreexistingCapabilityContainerIDs: []string{capabilityID}}
	if err := e.ReconcileNoEffect(context.Background(), []string{"run-1"}, "operator", true, inventory, docker, opts); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(docker.containers)
	if string(before) != string(after) || docker.mutations != 0 || docker.round != 2 || e.AO.(*fakeAO).createN != 0 {
		t.Fatal("reconciliation changed Docker artifacts or failed to check twice")
	}
	if len(c.observations) != 1 {
		t.Fatal(c.observations)
	}
	proof := c.observations[0].NoEffectProof.Reconciliation
	if proof.DockerRuntimeCount != 0 || proof.DockerInventoryContainerCount != 2 || len(proof.RetainedNeverStartedContainers) != 1 || len(proof.PreexistingCapabilityContainers) != 1 || proof.RetainedNeverStartedContainers[0].ID != retainedID || proof.PreexistingCapabilityContainers[0].ID != capabilityID {
		t.Fatalf("missing preserved-record evidence: %+v", proof)
	}
	var auditBody string
	if err := e.Journal.db.QueryRow(`SELECT result_json FROM journal_reconciliations WHERE run_id='run-1'`).Scan(&auditBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(auditBody, "complete_no_launched_ao_runtime") || !strings.Contains(auditBody, retainedID) || !strings.Contains(auditBody, capabilityID) {
		t.Fatal(auditBody)
	}
}
func TestRetainedCreatedContainerRejectsIncompleteOrChangedState(t *testing.T) {
	for _, scenario := range []string{"no-selection", "exited", "running", "paused", "restarting", "dead", "pid", "started", "finished", "restarted", "restart-policy", "missing-state", "null-flag", "missing-restart-count", "missing-label", "unknown-runtime", "missing-identity", "snapshot-change", "disappeared", "duplicate-selection", "overlap"} {
		t.Run(scenario, func(t *testing.T) {
			e, c, inventory, _ := prepareReconciliation(t)
			id := strings.Repeat("a", 64)
			fixture := neverStartedContainerFixture(id)
			docker := &completeInventoryDocker{containers: map[string]map[string]any{id: fixture}}
			opts := ReconciliationOptions{RetainNeverStartedContainerIDs: []string{id}}
			state := fixture["State"].(map[string]any)
			switch scenario {
			case "no-selection":
				opts.RetainNeverStartedContainerIDs = nil
			case "exited":
				state["Status"] = "exited"
			case "running":
				state["Running"] = true
			case "paused":
				state["Paused"] = true
			case "restarting":
				state["Restarting"] = true
			case "dead":
				state["Dead"] = true
			case "pid":
				state["Pid"] = 1
			case "started":
				state["StartedAt"] = time.Now().UTC().Add(-time.Minute)
			case "finished":
				state["FinishedAt"] = time.Now().UTC().Add(-time.Minute)
			case "restarted":
				fixture["RestartCount"] = 1
			case "restart-policy":
				fixture["HostConfig"].(map[string]any)["RestartPolicy"] = map[string]any{"Name": "always"}
			case "missing-state":
				delete(state, "Dead")
			case "null-flag":
				state["Running"] = nil
			case "missing-restart-count":
				delete(fixture, "RestartCount")
			case "missing-label":
				delete(fixture["Config"].(map[string]any)["Labels"].(map[string]string), aoContainerSessionLabel)
			case "unknown-runtime":
				fixture["Config"].(map[string]any)["Labels"].(map[string]string)[aoContainerRuntimeLabel] = "unknown"
			case "missing-identity":
				delete(fixture, "Name")
			case "snapshot-change":
				docker.change = func(round int, _ map[string]map[string]any) {
					if round == 2 {
						fixture["Name"] = "/changed-preserved-ao"
					}
				}
			case "disappeared":
				docker.change = func(round int, containers map[string]map[string]any) {
					if round == 2 {
						delete(containers, id)
					}
				}
			case "duplicate-selection":
				opts.RetainNeverStartedContainerIDs = append(opts.RetainNeverStartedContainerIDs, id)
			case "overlap":
				opts.UnrelatedPreexistingCapabilityContainerIDs = []string{id}
			}
			if err := e.ReconcileNoEffect(context.Background(), []string{"run-1"}, "operator", true, inventory, docker, opts); err == nil {
				t.Fatal("unsafe retained artifact was accepted")
			}
			if len(c.observations) != 0 || docker.mutations != 0 || e.AO.(*fakeAO).createN != 0 {
				t.Fatal("refused reconciliation caused effects")
			}
		})
	}
}
func TestPreexistingCapabilityExclusionRequiresExactHistoricalEvidence(t *testing.T) {
	for _, scenario := range []string{"not-selected", "new-created", "missing-label", "ao-tag", "blank-ao-tag", "ao-image", "wrong-name", "missing-running", "snapshot-change", "no-original-dispatch"} {
		t.Run(scenario, func(t *testing.T) {
			e, c, inventory, _ := prepareReconciliation(t)
			if scenario != "no-original-dispatch" {
				recordOriginalDispatch(t, e)
			}
			id := strings.Repeat("b", 64)
			fixture := preexistingCapabilityFixture(id)
			labels := fixture["Config"].(map[string]any)["Labels"].(map[string]string)
			docker := &completeInventoryDocker{containers: map[string]map[string]any{id: fixture}}
			opts := ReconciliationOptions{UnrelatedPreexistingCapabilityContainerIDs: []string{id}}
			switch scenario {
			case "not-selected":
				opts.UnrelatedPreexistingCapabilityContainerIDs = nil
			case "new-created":
				fixture["Created"] = time.Now().UTC()
			case "missing-label":
				delete(labels, "io.cylo.release")
			case "ao-tag":
				labels[aoContainerRuntimeLabel] = aoContainerCodexRuntime
			case "blank-ao-tag":
				labels[aoContainerSessionLabel] = ""
			case "ao-image":
				fixture["Config"].(map[string]any)["Image"] = "ao-codex"
			case "wrong-name":
				fixture["Name"] = "/arbitrary"
			case "missing-running":
				delete(fixture["State"].(map[string]any), "Running")
			case "snapshot-change":
				docker.change = func(round int, _ map[string]map[string]any) {
					if round == 2 {
						fixture["RestartCount"] = 1
					}
				}
			}
			if err := e.ReconcileNoEffect(context.Background(), []string{"run-1"}, "operator", true, inventory, docker, opts); err == nil {
				t.Fatal("unproven historical capability was excluded")
			}
			if len(c.observations) != 0 || docker.mutations != 0 {
				t.Fatal("refused exclusion mutated state")
			}
		})
	}
}

func TestManagedCapabilityExclusionUsesJournalAndHostAuthority(t *testing.T) {
	e, c, inventory, _ := prepareReconciliation(t)
	ctx := context.Background()
	authority, err := OpenDeploymentAuthority(ctx, filepath.Join(t.TempDir(), "authority.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	claim := contracts.DeploymentClaim{WorkspaceID: "workspace", DeploymentID: "deployment", AttemptID: "attempt", Fence: 1, ReleaseDigest: "sha256:" + strings.Repeat("4", 64)}
	if err := e.Journal.SaveDeploymentClaim(ctx, claim); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := deploymentClaimFingerprint(claim)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = authority.BeginDeploymentEffect(ctx, claim.DeploymentID, claim.Fence, fingerprint); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("b", 64)
	fixture := preexistingCapabilityFixture(id)
	fixture["Name"] = "/" + deploymentContainerName(claim.DeploymentID)
	fixture["Config"].(map[string]any)["Labels"] = deploymentLabels(claim)
	docker := &completeInventoryDocker{containers: map[string]map[string]any{id: fixture}}
	if err = e.ReconcileNoEffect(ctx, []string{"run-1"}, "operator", true, inventory, docker, ReconciliationOptions{DeploymentAuthority: authority}); err != nil {
		t.Fatal(err)
	}
	proof := c.observations[0].NoEffectProof.Reconciliation
	if len(proof.UnrelatedCapabilityContainerIDs) != 1 || proof.UnrelatedCapabilityContainerIDs[0] != id || docker.round != 2 || docker.mutations != 0 {
		t.Fatalf("capability evidence=%+v", proof)
	}
}
func TestUnmanagedNonAOInventoryRequiresCompleteIdentityAndState(t *testing.T) {
	for _, scenario := range []string{"complete", "missing-name", "missing-image", "null-labels", "missing-state", "blank-ao-tag"} {
		t.Run(scenario, func(t *testing.T) {
			e, c, inventory, _ := prepareReconciliation(t)
			id := strings.Repeat("e", 64)
			fixture := preexistingCapabilityFixture(id)
			fixture["Name"] = "/control-service"
			config := fixture["Config"].(map[string]any)
			config["Image"] = "cylo/control:local"
			config["Labels"] = map[string]string{}
			switch scenario {
			case "missing-name":
				delete(fixture, "Name")
			case "missing-image":
				delete(config, "Image")
			case "null-labels":
				config["Labels"] = nil
			case "missing-state":
				delete(fixture, "State")
			case "blank-ao-tag":
				config["Labels"].(map[string]string)[aoContainerRuntimeLabel] = ""
			}
			docker := &completeInventoryDocker{containers: map[string]map[string]any{id: fixture}}
			err := e.ReconcileNoEffect(context.Background(), []string{"run-1"}, "operator", true, inventory, docker)
			if scenario != "complete" {
				if err == nil || len(c.observations) != 0 {
					t.Fatal("unclassifiable Docker container was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			proof := c.observations[0].NoEffectProof.Reconciliation
			if proof.DockerInventoryContainerCount != 1 || len(proof.UnmanagedContainerIDs) != 1 || proof.UnmanagedContainerIDs[0] != id || docker.mutations != 0 || docker.round != 2 {
				t.Fatalf("unmanaged complete inventory=%+v", proof)
			}
		})
	}
}

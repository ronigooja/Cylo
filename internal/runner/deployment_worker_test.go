package runner

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ronigooja/Zoe/contracts"
)

type deploymentWorkerControl struct {
	claim        contracts.DeploymentClaim
	observations []contracts.DeploymentObservation
}

func (c *deploymentWorkerControl) ClaimDeployment(context.Context) (*contracts.DeploymentClaim, error) {
	return nil, nil
}
func (c *deploymentWorkerControl) CurrentDeploymentClaim(context.Context) (*contracts.DeploymentClaim, error) {
	claim := c.claim
	if len(c.observations) > 0 && c.observations[len(c.observations)-1].State != "uncertain" {
		return nil, nil
	}
	return &claim, nil
}
func (c *deploymentWorkerControl) UnresolvedDeploymentClaims(ctx context.Context) ([]contracts.DeploymentClaim, error) {
	if len(c.observations) > 0 && c.observations[len(c.observations)-1].State != "uncertain" {
		return nil, nil
	}
	claim := c.claim
	return []contracts.DeploymentClaim{claim}, nil
}
func (c *deploymentWorkerControl) ObserveDeployment(_ context.Context, observation contracts.DeploymentObservation) error {
	c.observations = append(c.observations, observation)
	return nil
}

func TestDeploymentWorkerReconcilesAcknowledgedUncertainWithoutRedispatch(t *testing.T) {
	ctx := context.Background()
	claim := validDeploymentClaim()
	journal, err := OpenJournal(ctx, filepath.Join(t.TempDir(), "runner.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	authority, err := OpenDeploymentAuthority(ctx, filepath.Join(t.TempDir(), "authority.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	docker := &fakeDocker{claim: claim, imageDigest: claim.Manifest.ArtifactDigest, containerID: "container-id", ambiguousRun: true}
	control := &deploymentWorkerControl{claim: claim}
	worker := &DeploymentWorker{Control: control, Journal: journal, Executor: &DeploymentExecutor{Docker: docker, Fences: authority}}
	if err := worker.Process(ctx, claim); err != nil {
		t.Fatalf("initial ambiguous deployment: %v", err)
	}
	if len(control.observations) != 1 || control.observations[0].State != "uncertain" {
		t.Fatalf("initial observations=%+v", control.observations)
	}
	if err := worker.Process(ctx, claim); err != nil {
		t.Fatalf("read-only reconciliation: %v", err)
	}
	if len(control.observations) != 2 || control.observations[1].State != "running" {
		t.Fatalf("reconciled observations=%+v", control.observations)
	}
	if control.observations[0].ObservationID == control.observations[1].ObservationID {
		t.Fatalf("reconciliation reused the uncertain observation ID %q", control.observations[0].ObservationID)
	}
	var runs, pulls int
	for _, command := range docker.commands {
		switch command[0] {
		case "run":
			runs++
		case "pull":
			pulls++
		}
	}
	if runs != 1 || pulls != 1 {
		t.Fatalf("reconciliation repeated Docker writes: runs=%d pulls=%d commands=%v", runs, pulls, docker.commands)
	}
}

func TestDeploymentWorkerRunBacksOffAndClaimsIndependentPendingWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1400*time.Millisecond)
	defer cancel()
	claim := validDeploymentClaim()
	journal, err := OpenJournal(context.Background(), filepath.Join(t.TempDir(), "runner.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	control := &pendingAfterCurrentControl{claim: claim}
	worker := &DeploymentWorker{Control: control, Journal: journal, Executor: &DeploymentExecutor{}}
	err = worker.Run(ctx, false, time.Millisecond)
	if err != nil {
		t.Fatalf("worker Run: %v", err)
	}
	if control.currentCalls == 0 || control.claimCalls == 0 {
		t.Fatalf("current=%d claim=%d", control.currentCalls, control.claimCalls)
	}
}

type pendingAfterCurrentControl struct {
	claim        contracts.DeploymentClaim
	currentCalls int
	claimCalls   int
}

type deploymentControlClient interface {
	ClaimDeployment(context.Context) (*contracts.DeploymentClaim, error)
	CurrentDeploymentClaim(context.Context) (*contracts.DeploymentClaim, error)
	UnresolvedDeploymentClaims(context.Context) ([]contracts.DeploymentClaim, error)
	ObserveDeployment(context.Context, contracts.DeploymentObservation) error
}

var _ deploymentControlClient = (*deploymentWorkerControl)(nil)
var _ deploymentControlClient = (*pendingAfterCurrentControl)(nil)

func (c *pendingAfterCurrentControl) CurrentDeploymentClaim(context.Context) (*contracts.DeploymentClaim, error) {
	c.currentCalls++
	claim := c.claim
	claim.ClaimState = "uncertain"
	return &claim, nil
}
func (c *pendingAfterCurrentControl) UnresolvedDeploymentClaims(context.Context) ([]contracts.DeploymentClaim, error) {
	c.currentCalls++
	return nil, nil
}
func (c *pendingAfterCurrentControl) ClaimDeployment(context.Context) (*contracts.DeploymentClaim, error) {
	c.claimCalls++
	return nil, errors.New("no other deployment in this fixture")
}
func (*pendingAfterCurrentControl) ObserveDeployment(context.Context, contracts.DeploymentObservation) error {
	return nil
}

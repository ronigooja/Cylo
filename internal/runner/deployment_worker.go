package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ronigooja/Cylo/contracts"
)

var ErrNoDeployment = errors.New("no capability deployment claim available")

type DeploymentControl interface {
	ClaimDeployment(context.Context) (*contracts.DeploymentClaim, error)
	CurrentDeploymentClaim(context.Context) (*contracts.DeploymentClaim, error)
	UnresolvedDeploymentClaims(context.Context) ([]contracts.DeploymentClaim, error)
	ObserveDeployment(context.Context, contracts.DeploymentObservation) error
}

type DeploymentWorker struct {
	Control  DeploymentControl
	Journal  *Journal
	Executor *DeploymentExecutor
}

func (w *DeploymentWorker) Process(ctx context.Context, claim contracts.DeploymentClaim) error {
	if w.Control == nil || w.Journal == nil || w.Executor == nil {
		return errors.New("deployment worker requires control, journal, and executor")
	}
	if err := w.Journal.SaveDeploymentClaim(ctx, claim); err != nil {
		return err
	}
	entry, ok, err := w.Journal.DeploymentEntry(ctx, claim.DeploymentID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("durable deployment claim disappeared")
	}
	if entry.Observation != nil {
		if !entry.Acknowledged {
			return w.submit(ctx, *entry.Observation)
		}
		if entry.Observation.State != "uncertain" {
			return nil
		}
		// The same fenced claim is replayed through the executor's read-only
		// reconciliation path. Never dispatch its Docker writes a second time.
		reconciled := w.Executor.Execute(ctx, claim)
		if reconciled.State == "uncertain" {
			return ErrOutcomePending
		}
		reconciled.ObservationID = fmt.Sprintf("deploy-%s-%d-reconcile-%d", shortHash(claim.AttemptID), claim.Fence, time.Now().UTC().UnixNano())
		if err := w.Journal.SaveDeploymentObservation(ctx, reconciled); err != nil {
			return err
		}
		return w.submit(ctx, reconciled)
	}
	observation := w.Executor.Execute(ctx, claim)
	if err := w.Journal.SaveDeploymentObservation(ctx, observation); err != nil {
		return err
	}
	return w.submit(ctx, observation)
}

func (w *DeploymentWorker) submit(ctx context.Context, observation contracts.DeploymentObservation) error {
	if err := w.Control.ObserveDeployment(ctx, observation); err != nil {
		return err
	}
	return w.Journal.AcknowledgeDeploymentObservation(ctx, observation.DeploymentID, observation.ObservationID)
}

// Run performs restart reconciliation before polling. Unknown effects are
// retried through the executor's inspect-only path with bounded backoff, while
// independent pending deployments remain claimable.
func (w *DeploymentWorker) Run(ctx context.Context, once bool, interval time.Duration) error {
	if w.Control == nil || w.Journal == nil || w.Executor == nil {
		return errors.New("deployment worker requires control, journal, and executor")
	}
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if interval > 30*time.Second {
		interval = 30 * time.Second
	}
	if once {
		current, err := w.Control.UnresolvedDeploymentClaims(ctx)
		if err != nil {
			return err
		}
		var firstErr error
		if len(current) > 0 {
			for _, active := range current {
				if activeErr := w.Process(ctx, active); activeErr != nil && firstErr == nil {
					firstErr = activeErr
				}
			}
		}
		claim, err := w.Control.ClaimDeployment(ctx)
		if err != nil {
			if firstErr != nil {
				return firstErr
			}
			return err
		}
		if claim == nil {
			if firstErr != nil {
				return firstErr
			}
			return ErrNoDeployment
		}
		if err := w.Process(ctx, *claim); err != nil && firstErr != nil {
			return firstErr
		} else if err != nil {
			return err
		}
		if claim.CleanupOnly {
			return nil
		}
		return firstErr
	}
	backoff := interval
	if backoff < time.Second {
		backoff = time.Second
	}
	maxBackoff := 30 * time.Second
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		unresolved := false
		current, err := w.Control.UnresolvedDeploymentClaims(ctx)
		if err == nil {
			for _, active := range current {
				if activeErr := w.Process(ctx, active); activeErr != nil {
					unresolved = true
				}
			}
		} else {
			unresolved = true
		}
		claim, claimErr := w.Control.ClaimDeployment(ctx)
		if claimErr == nil && claim != nil {
			err = w.Process(ctx, *claim)
			if errors.Is(err, ErrOutcomePending) {
				unresolved = true
			} else if err != nil {
				unresolved = true
			}
		} else if claimErr != nil {
			unresolved = true
		}
		delay := interval
		if unresolved {
			delay = backoff
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		} else {
			backoff = interval
			if backoff < time.Second {
				backoff = time.Second
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
	}
}

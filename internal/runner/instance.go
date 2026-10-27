package runner

import (
	"context"
	"errors"
	"fmt"

	"github.com/ronigooja/Zoe/contracts"
)

var ErrInstanceFence = errors.New("runner active instance fence is unavailable or changed")

type InstanceAuthority interface {
	Instance(context.Context) (InstanceState, error)
}

func (e *Engine) checkInstance(ctx context.Context, claim contracts.JobClaim, requireDispatch bool) error {
	if e.Instance == nil {
		return nil
	}
	state, err := e.Instance.Instance(ctx)
	if err != nil {
		return fmt.Errorf("%w: read active instance state: %v", ErrInstanceFence, err)
	}
	if state.InstanceID == "" || state.Epoch < 1 || (requireDispatch && !state.DispatchEnabled) {
		return ErrInstanceFence
	}
	entry, ok, err := e.Journal.Entry(ctx, claim.RunID)
	if err != nil {
		return fmt.Errorf("read runner instance fence: %w", err)
	}
	if !ok {
		return errors.New("runner journal claim is missing before instance fence")
	}
	if entry.InstanceID == "" {
		if entry.Session != "" || (entry.Phase != "claimed" && entry.Phase != "failed") {
			return fmt.Errorf("%w: pre-existing AO effect has no persisted instance epoch", ErrInstanceFence)
		}
		if err := e.Journal.SetInstance(ctx, claim.RunID, claim.AttemptID, claim.Fence, state.InstanceID, state.Epoch); err != nil {
			return fmt.Errorf("persist active instance fence: %w", err)
		}
		return nil
	}
	if entry.InstanceID != state.InstanceID || entry.Epoch != state.Epoch {
		return ErrInstanceFence
	}
	return nil
}

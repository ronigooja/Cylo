package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/ronigooja/Cylo/contracts"
)

// CapabilityTriggerAdmissionStatus is the control plane's explicit report of
// task-trigger admission and any queued program triggers without an executor.
type CapabilityTriggerAdmissionStatus struct {
	PendingProgramTriggers int    `json:"pending_program_triggers"`
	ProgramExecution       string `json:"program_execution"`
	ProgramReasonCode      string `json:"program_reason_code,omitempty"`
}

type TriggerAdmissionControl interface {
	AdmitCapabilityTriggers(context.Context) (CapabilityTriggerAdmissionStatus, error)
}

// TriggerAdmissionWorker advances event/schedule cursors and admits
// worker/super service tasks. It only calls the local control API; model work
// begins later through the normal runner task-claim path.
type TriggerAdmissionWorker struct {
	Control              TriggerAdmissionControl
	OnProgramUnsupported func(int)
}

func (c *ControlClient) AdmitCapabilityTriggers(ctx context.Context) (CapabilityTriggerAdmissionStatus, error) {
	response, err := c.request(ctx, http.MethodPost, "/runner/v1/capability-triggers/admissions", nil, "")
	if err != nil {
		return CapabilityTriggerAdmissionStatus{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return CapabilityTriggerAdmissionStatus{}, safeHTTPError("admit capability triggers", response.StatusCode)
	}
	var status CapabilityTriggerAdmissionStatus
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&status); err != nil {
		return CapabilityTriggerAdmissionStatus{}, fmt.Errorf("decode capability trigger admission status: %w", err)
	}
	return status, nil
}

func (c *ControlClient) DispatchAuthorization(ctx context.Context, runID string) (contracts.DispatchAuthorization, error) {
	if runID == "" {
		return contracts.DispatchAuthorization{}, errors.New("run id is required for dispatch authorization")
	}
	endpoint := "/runner/v1/runs/" + url.PathEscape(runID) + "/dispatch-authorization"
	response, err := c.request(ctx, http.MethodPost, endpoint, nil, "")
	if err != nil {
		return contracts.DispatchAuthorization{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return contracts.DispatchAuthorization{}, safeHTTPError("check task dispatch authorization", response.StatusCode)
	}
	var result contracts.DispatchAuthorization
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return contracts.DispatchAuthorization{}, fmt.Errorf("decode task dispatch authorization: %w", err)
	}
	if !result.Allowed && result.ReasonCode == "" {
		return contracts.DispatchAuthorization{}, errors.New("control denied dispatch without a reason code")
	}
	return result, nil
}

func (w *TriggerAdmissionWorker) Run(ctx context.Context, once bool, interval time.Duration) error {
	if w == nil || w.Control == nil {
		return errors.New("trigger admission worker requires control client")
	}
	if interval <= 0 {
		interval = 15 * time.Second
	}
	if interval < time.Second {
		interval = time.Second
	}
	if interval > time.Minute {
		interval = time.Minute
	}
	if once {
		status, err := w.Control.AdmitCapabilityTriggers(ctx)
		if err != nil {
			return err
		}
		w.reportUnsupported(status.PendingProgramTriggers)
		return nil
	}
	backoff := time.Second
	lastUnsupported := -1
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		status, err := w.Control.AdmitCapabilityTriggers(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			wait := backoff
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			if err := waitContext(ctx, wait); err != nil {
				return nil
			}
			continue
		}
		backoff = time.Second
		if status.PendingProgramTriggers != lastUnsupported {
			w.reportUnsupported(status.PendingProgramTriggers)
			lastUnsupported = status.PendingProgramTriggers
		}
		if err := waitContext(ctx, interval); err != nil {
			return nil
		}
	}
}

func (w *TriggerAdmissionWorker) reportUnsupported(count int) {
	if count > 0 && w.OnProgramUnsupported != nil {
		w.OnProgramUnsupported(count)
	}
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

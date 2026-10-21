package voice

import (
	"context"
	"time"
)

// AccountMediaCapabilities describes capabilities only when a trusted adapter
// backed by Space account/media control is configured. The current downstream
// Space API does not expose these capabilities.
type AccountMediaCapabilities struct {
	AccountLeaseIdentity       bool `json:"account_lease_identity"`
	AccountScopedTermination   bool `json:"account_scoped_termination"`
	TrustedTerminationEvidence bool `json:"trusted_termination_evidence"`
}

type AccountTerminationObservation struct {
	AccountID  string
	LeaseID    string
	SessionID  string
	Terminated bool
	EvidenceID string
	ObservedAt time.Time
}

// AccountMediaLifecycle is the extension contract required before Cylo can
// safely release or rotate a specific hidden Space account lease. It is
// intentionally not implemented by the current Space downstream adapter.
type AccountMediaLifecycle interface {
	Capabilities() AccountMediaCapabilities
	ObserveTermination(context.Context, string) (AccountTerminationObservation, error)
}

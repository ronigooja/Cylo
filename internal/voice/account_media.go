package voice

import (
	"context"
	"time"
)

// AccountMediaCapabilities describes adapter capabilities. A configured
// adapter still does not prove gateway support until the exact lease identity
// and trusted release evidence are present and validated for a session.
type AccountMediaCapabilities struct {
	AccountLeaseIdentity       bool `json:"account_lease_identity"`
	AccountScopedTermination   bool `json:"account_scoped_termination"`
	TrustedTerminationEvidence bool `json:"trusted_termination_evidence"`
}

type AccountTerminationObservation struct {
	AccountID         string
	LeaseID           string
	LeaseGeneration   int64
	SessionID         string
	SessionGeneration int64
	Terminated        bool
	EvidenceID        string
	ObservedAt        time.Time
}

// AccountLeaseReference is an opaque, non-credential identity supplied by a
// trusted Space adapter. Cylo persists only a hash of AccountID; the adapter
// uses LeaseID and Generation to scope and deduplicate upstream release.
type AccountLeaseReference struct {
	LeaseID           string
	Generation        int64
	SessionID         string
	SessionGeneration int64
}

// AccountMediaLifecycle is the contract required before Cylo can safely
// release or rotate a specific hidden Space account lease.
type AccountMediaLifecycle interface {
	Capabilities() AccountMediaCapabilities
	ReleaseAccountLease(context.Context, AccountLeaseReference, string) (AccountTerminationObservation, error)
}

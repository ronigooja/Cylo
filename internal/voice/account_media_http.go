package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SpaceAccountMediaAdapter implements the optional account-level contract on
// the already authorized Space gateway. It carries no account credentials.
// The gateway must return a pseudonymous account ID and opaque lease ID on
// session creation, and an exact terminal observation for idempotent release.
type SpaceAccountMediaAdapter struct {
	base   *url.URL
	key    string
	client *http.Client
}

func NewSpaceAccountMediaAdapter(gatewayURL, gatewayAPIKey string, client *http.Client) (*SpaceAccountMediaAdapter, error) {
	base, err := url.Parse(strings.TrimSpace(gatewayURL))
	if err != nil || base == nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || strings.TrimSpace(gatewayAPIKey) == "" {
		return nil, errors.New("account media adapter requires the configured HTTPS Space gateway and its deployment-provided key")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &SpaceAccountMediaAdapter{base: base, key: gatewayAPIKey, client: client}, nil
}

func (a *SpaceAccountMediaAdapter) Capabilities() AccountMediaCapabilities {
	return AccountMediaCapabilities{AccountLeaseIdentity: true, AccountScopedTermination: true, TrustedTerminationEvidence: true}
}

// ReleaseAccountLease is idempotent when the gateway honors the stable
// Idempotency-Key. Zoe accepts a release only when the returned evidence
// exactly matches the requested session and lease generation.
func (a *SpaceAccountMediaAdapter) ReleaseAccountLease(ctx context.Context, lease AccountLeaseReference, idempotencyKey string) (AccountTerminationObservation, error) {
	if strings.TrimSpace(lease.LeaseID) == "" || lease.Generation < 1 || strings.TrimSpace(lease.SessionID) == "" || strings.TrimSpace(idempotencyKey) == "" {
		return AccountTerminationObservation{}, errors.New("account lease release identity is incomplete")
	}
	payload, err := json.Marshal(struct {
		VoiceSessionID         string `json:"voice_session_id"`
		VoiceSessionGeneration int64  `json:"voice_session_generation"`
		AccountLeaseGeneration int64  `json:"account_lease_generation"`
	}{VoiceSessionID: lease.SessionID, VoiceSessionGeneration: lease.SessionGeneration, AccountLeaseGeneration: lease.Generation})
	if err != nil {
		return AccountTerminationObservation{}, err
	}
	escapedPath := strings.TrimRight(a.base.EscapedPath(), "/") + "/v1/voice/account-leases/" + url.PathEscape(lease.LeaseID) + "/release"
	decodedPath, err := url.PathUnescape(escapedPath)
	if err != nil {
		return AccountTerminationObservation{}, err
	}
	u := *a.base
	u.Path = decodedPath
	u.RawPath = escapedPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
	if err != nil {
		return AccountTerminationObservation{}, err
	}
	req.Header.Set("Authorization", "Bearer "+a.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	response, err := a.client.Do(req)
	if err != nil {
		return AccountTerminationObservation{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusServiceUnavailable {
			return AccountTerminationObservation{}, errors.New("Space account lease release returned HTTP 503: upstream WebRTC termination evidence is unavailable")
		}
		return AccountTerminationObservation{}, fmt.Errorf("Space account lease release returned HTTP %d", response.StatusCode)
	}
	var wire struct {
		AccountID         string `json:"account_id"`
		LeaseID           string `json:"account_lease_id"`
		Generation        int64  `json:"account_lease_generation"`
		SessionID         string `json:"voice_session_id"`
		SessionGeneration int64  `json:"voice_session_generation"`
		Terminated        bool   `json:"terminated"`
		EvidenceID        string `json:"evidence_id"`
		ObservedAt        string `json:"observed_at"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return AccountTerminationObservation{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return AccountTerminationObservation{}, errors.New("Space account lease release returned trailing or oversized data")
	}
	observedAt, err := time.Parse(time.RFC3339Nano, wire.ObservedAt)
	if err != nil || strings.TrimSpace(wire.AccountID) == "" || wire.LeaseID != lease.LeaseID || wire.Generation != lease.Generation || wire.SessionID != lease.SessionID || wire.SessionGeneration != lease.SessionGeneration || !wire.Terminated || strings.TrimSpace(wire.EvidenceID) == "" || observedAt.IsZero() || observedAt.After(time.Now().UTC()) {
		return AccountTerminationObservation{}, errors.New("Space account lease release did not confirm the exact terminal lease identity")
	}
	return AccountTerminationObservation{AccountID: wire.AccountID, LeaseID: wire.LeaseID, LeaseGeneration: wire.Generation, SessionID: wire.SessionID, SessionGeneration: wire.SessionGeneration, Terminated: true, EvidenceID: wire.EvidenceID, ObservedAt: observedAt}, nil
}

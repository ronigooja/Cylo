package voice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSpaceAccountMediaAdapterRequiresExactIdempotentReleaseEvidence(t *testing.T) {
	lease := AccountLeaseReference{LeaseID: "opaque-lease", Generation: 7, SessionID: "space-session", SessionGeneration: 3}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/gateway/v1/voice/account-leases/opaque-lease/release" {
			t.Errorf("release request=%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Idempotency-Key") != "release-key" {
			t.Errorf("idempotency key=%q", r.Header.Get("Idempotency-Key"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["voice_session_id"] != lease.SessionID || payload["voice_session_generation"] != float64(lease.SessionGeneration) || payload["account_lease_generation"] != float64(lease.Generation) {
			t.Errorf("release payload=%v", payload)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account_id": "opaque-account", "account_lease_id": lease.LeaseID,
			"account_lease_generation": lease.Generation, "voice_session_id": lease.SessionID,
			"voice_session_generation": lease.SessionGeneration, "terminated": true,
			"evidence_id": "evidence-1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano),
		})
	}))
	defer server.Close()
	adapter, err := NewSpaceAccountMediaAdapter(server.URL+"/gateway", "deployment-key", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	observation, err := adapter.ReleaseAccountLease(context.Background(), lease, "release-key")
	if err != nil {
		t.Fatal(err)
	}
	if observation.LeaseID != lease.LeaseID || observation.LeaseGeneration != lease.Generation || observation.SessionID != lease.SessionID || observation.SessionGeneration != lease.SessionGeneration || !observation.Terminated {
		t.Fatalf("observation=%+v", observation)
	}
}

func TestSpaceAccountMediaAdapterRejectsWrongLeaseGeneration(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account_id": "opaque-account", "account_lease_id": "different-lease",
			"account_lease_generation": 8, "voice_session_id": "space-session",
			"voice_session_generation": 3, "terminated": true,
			"evidence_id": "evidence-1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano),
		})
	}))
	defer server.Close()
	adapter, err := NewSpaceAccountMediaAdapter(server.URL, "deployment-key", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.ReleaseAccountLease(context.Background(), AccountLeaseReference{LeaseID: "opaque-lease", Generation: 7, SessionID: "space-session", SessionGeneration: 3}, "release-key")
	if err == nil {
		t.Fatal("mismatched lease generation was accepted")
	}
}

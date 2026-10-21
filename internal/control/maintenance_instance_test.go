package control

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/internal/storage"
)

func TestPersistentInstanceFenceAndVerifiedRestore(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = RegisterInstanceMaintenanceMigration(store); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := instanceStatus(t, store)
	if err != nil || !state.DispatchEnabled || state.Epoch != 1 {
		t.Fatalf("initial state=%#v err=%v", state, err)
	}
	fenced, err := FenceInstance(ctx, store, "migration", state.Epoch)
	if err != nil || fenced.DispatchEnabled || fenced.Epoch != 2 {
		t.Fatalf("fenced state=%#v err=%v", fenced, err)
	}
	if _, err = FenceInstance(ctx, store, "stale", 1); err == nil {
		t.Fatal("stale fence epoch was accepted")
	}
	prepared, err := PrepareRestoredInstance(ctx, store, "target-instance", fenced.Epoch)
	if err != nil || prepared.DispatchEnabled || prepared.DerivedRetrievalEnabled || prepared.InstanceID != "target-instance" || prepared.Epoch != 3 {
		t.Fatalf("prepared state=%#v err=%v", prepared, err)
	}

	key := []byte("0123456789abcdef0123456789abcdef")
	proofDoc := signedProof(t, key, fenced.InstanceID, fenced.Epoch)
	proof, err := VerifyIsolationProof(proofDoc, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = RecordIsolationProof(ctx, store, proof); err != nil {
		t.Fatal(err)
	}
	enabled, err := EnableInstance(ctx, store, prepared.Epoch, proof)
	if err != nil || !enabled.DispatchEnabled || enabled.InstanceID != prepared.InstanceID {
		t.Fatalf("enabled state=%#v err=%v", enabled, err)
	}

	if _, err = PrepareRestoredInstance(ctx, store, "second-target", enabled.Epoch); err == nil {
		t.Fatal("active instance was accepted as a restored target")
	}
}

func TestIsolationProofRejectsUnverifiedAndActiveSource(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	good := signedProof(t, key, "source", 4)
	var envelope isolationProofEnvelope
	if err := json.Unmarshal(good, &envelope); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyIsolationProof([]byte(`{"payload":"`+envelope.Payload+`","hmac":"deadbeef"}`), key); err == nil {
		t.Fatal("forged proof HMAC was accepted")
	}

	payload := map[string]any{
		"schema_version": 1, "old_instance_id": "source", "old_epoch": 4,
		"source_host": "source.example", "units": []map[string]string{{"name": "cylo.service", "active_state": "active", "unit_file_state": "masked"}},
		"active_runs": 0, "unknown_effects": 0, "verified_at": time.Now().UTC(),
	}
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	active := isolationProofEnvelope{Payload: base64.RawURLEncoding.EncodeToString(raw), HMAC: hex.EncodeToString(mac.Sum(nil))}
	doc, _ := json.Marshal(active)
	if _, err := VerifyIsolationProof(doc, key); err == nil {
		t.Fatal("active systemd unit proof was accepted")
	}
}

func instanceStatus(t *testing.T, store *storage.Store) (InstanceState, error) {
	t.Helper()
	tx, err := store.DB().BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return InstanceState{}, err
	}
	defer tx.Rollback()
	return GetInstanceState(context.Background(), tx)
}

func signedProof(t *testing.T, key []byte, oldID string, oldEpoch int64) []byte {
	t.Helper()
	payload := map[string]any{
		"schema_version": 1, "old_instance_id": oldID, "old_epoch": oldEpoch,
		"source_host": "source.example", "units": []map[string]string{{"name": "cylo.service", "active_state": "inactive", "unit_file_state": "masked"}},
		"active_runs": 0, "unknown_effects": 0, "verified_at": time.Now().UTC(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	doc, err := json.Marshal(isolationProofEnvelope{Payload: base64.RawURLEncoding.EncodeToString(raw), HMAC: hex.EncodeToString(mac.Sum(nil))})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

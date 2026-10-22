package acceptance

import (
	"net/http"
	"sync/atomic"
	"testing"
)

func TestIndependentHangupIgnoredClaimCannotReachGateway(t *testing.T) {
	var deletes atomic.Int32
	store, handler, token := newHangupHarness(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deletes.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	if _, err := store.DB().Exec(`CREATE TRIGGER ignore_voice_claim BEFORE UPDATE OF state ON voice_sessions WHEN NEW.state='ending' BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	response := hangupRequest(handler, token)
	if response.Code != http.StatusConflict || deletes.Load() != 0 {
		t.Fatalf("ignored claim status=%d deletes=%d", response.Code, deletes.Load())
	}
	var state string
	var blocked int
	if err := store.DB().QueryRow(`SELECT state FROM voice_sessions WHERE id='hangup-session'`).Scan(&state); err != nil || state != "active" {
		t.Fatalf("state=%q err=%v", state, err)
	}
	if err := store.DB().QueryRow(`SELECT blocked FROM voice_pool_state WHERE id=1`).Scan(&blocked); err != nil || blocked != 1 {
		t.Fatalf("pool blocked=%d err=%v", blocked, err)
	}
}

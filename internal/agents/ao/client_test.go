package ao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCreateWorkerKeepsServerFailureUnknownWithoutCreateReplay(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/v1/sessions" || r.Method != http.MethodPost {
			t.Fatalf("unexpected AO route: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer deployment-token" {
			t.Fatalf("AO bearer was not sent")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["kind"] != "worker" || body["clientRequestId"] != "run-1" {
			t.Fatalf("unsafe/non-idempotent create body: %#v", body)
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, Token: "deployment-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateWorker(context.Background(), WorkerSpec{
		ClientRequestID: "run-1", ProjectID: "safe-fixture", Harness: "codex",
		Model: "gpt-6.1-sol", DisplayName: "Zoe run run-1", Prompt: "Inspect the local fixture.",
	})
	if !errors.Is(err, ErrOutcomeUnknown) || requests != 1 {
		t.Fatalf("error=%v requests=%d; unknown create must not be replayed", err, requests)
	}
}

func TestCreateWorkerDoesNotTreatUnrecognizedClientErrorAsDefiniteRejection(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "details must not escape", http.StatusBadRequest)
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateWorker(context.Background(), WorkerSpec{
		ClientRequestID: "run-1", ProjectID: "safe-fixture", Harness: "codex", DisplayName: "test", Prompt: "test",
	})
	if err == nil || requests != 1 {
		t.Fatalf("error=%v requests=%d", err, requests)
	}
	if strings.Contains(err.Error(), "details must not escape") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("AO error exposed response details or credentials: %v", err)
	}
	if !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("unrecognized AO rejection must remain uncertain: %v", err)
	}
}

func TestCreateWorkerReturnsSanitizedAllowlistedRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":"INVALID_REQUEST","message":"private details"}`)
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, Token: "fixture-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateWorker(context.Background(), WorkerSpec{ClientRequestID: "run-1", ProjectID: "p", Harness: "codex", DisplayName: "test", Prompt: "test"})
	var rejected CreateRejectedError
	if !errors.As(err, &rejected) || rejected.Status != http.StatusBadRequest || rejected.Code != "INVALID_REQUEST" {
		t.Fatalf("error=%v, want sanitized allowlisted rejection", err)
	}
	if strings.Contains(err.Error(), "private details") || strings.Contains(err.Error(), "fixture-token") {
		t.Fatalf("unsafe rejection detail: %v", err)
	}
}

func TestCreateWorkerRecognizesNestedProjectNotFoundRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"PROJECT_NOT_FOUND","message":"private"}}`)
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, Token: "fixture-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateWorker(context.Background(), WorkerSpec{ClientRequestID: "run-1", ProjectID: "p", Harness: "codex", DisplayName: "test", Prompt: "test"})
	var rejected CreateRejectedError
	if !errors.As(err, &rejected) || rejected.Code != "PROJECT_NOT_FOUND" || rejected.Status != http.StatusNotFound {
		t.Fatalf("error=%v", err)
	}
}

func TestCreateWorkerDoesNotTrustAllowlistedCodeOnServerFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"code":"INVALID_REQUEST"}`)
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, Token: "fixture-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateWorker(context.Background(), WorkerSpec{ClientRequestID: "run-1", ProjectID: "p", Harness: "codex", DisplayName: "test", Prompt: "test"})
	var rejected CreateRejectedError
	if errors.As(err, &rejected) || !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("error=%v; proxy 5xx must stay unknown", err)
	}
}

func TestCreateWorkerTimeoutRemainsUnknownWithoutReplay(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil || request["clientRequestId"] != "run-timeout" {
			t.Errorf("request identity changed: %#v (%v)", request, err)
		}
		w.Header().Set("Content-Type", "application/json")
		<-r.Context().Done()
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, Token: "fixture-token", Client: &http.Client{Timeout: 25 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateWorker(context.Background(), WorkerSpec{
		ClientRequestID: "run-timeout", ProjectID: "safe-fixture", Harness: "opencode",
		DisplayName: "bounded fixture", Prompt: "local fixture only",
	})
	if !errors.Is(err, ErrOutcomeUnknown) || requests.Load() != 1 {
		t.Fatalf("error=%v requests=%d, want one unknown create request", err, requests.Load())
	}
	if strings.Contains(err.Error(), "do not expose this") || strings.Contains(err.Error(), "fixture-token") {
		t.Fatalf("unsafe error detail escaped: %v", err)
	}
}

func TestCreateWorkerDoesNotReplayIncompleteKeyAsCreate(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":"conflict","code":"CLIENT_REQUEST_INCOMPLETE"}`)
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, Token: "fixture-token", Client: &http.Client{Timeout: 25 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateWorker(context.Background(), WorkerSpec{
		ClientRequestID: "run-committed", ProjectID: "safe-fixture", Harness: "opencode",
		DisplayName: "bounded fixture", Prompt: "local fixture only",
	})
	if !errors.Is(err, ErrOutcomeUnknown) || requests != 1 {
		t.Fatalf("error=%v requests=%d; incomplete create is not a read-only lookup", err, requests)
	}
}

func TestAOErrorEnvelopeCodeIsIgnoredWhenMalformedOrOversized(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{"code":"CLIENT_REQUEST_INCOMPLETE"`},
		{name: "oversized", body: `{"code":"CLIENT_REQUEST_INCOMPLETE","padding":"` + strings.Repeat("x", 64<<10) + `"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, test.body)
			}))
			defer srv.Close()
			client, err := New(Config{BaseURL: srv.URL, Token: "secret"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.CreateWorker(context.Background(), WorkerSpec{
				ClientRequestID: "run-safe-error", ProjectID: "safe-fixture", Harness: "opencode",
				DisplayName: "bounded fixture", Prompt: "local fixture only",
			})
			if err == nil || !errors.Is(err, ErrOutcomeUnknown) || strings.Contains(err.Error(), "padding") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe/incorrect error classification: %v", err)
			}
		})
	}
}

func TestCreateWorkerKeepsMalformedSuccessfulResponseUnknownWithoutReplay(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"session":`)
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, Token: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateWorker(context.Background(), WorkerSpec{
		ClientRequestID: "run-ambiguous", ProjectID: "safe-fixture", Harness: "codex", DisplayName: "test", Prompt: "inspect fixture",
	})
	if !errors.Is(err, ErrOutcomeUnknown) || requests != 1 {
		t.Fatalf("error=%v requests=%d; malformed create response must stay uncertain", err, requests)
	}
}

func TestCreateWorkerTreatsSuccessfulResponseWithoutIDAsUnknown(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = fmt.Fprint(w, `{"session":{}}`)
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, Token: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateWorker(context.Background(), WorkerSpec{
		ClientRequestID: "run-ambiguous", ProjectID: "safe-fixture", Harness: "codex", DisplayName: "test", Prompt: "inspect fixture",
	})
	if !errors.Is(err, ErrOutcomeUnknown) || requests != 1 {
		t.Fatalf("error=%v requests=%d", err, requests)
	}
}

func TestStopWorkerVerifiesSessionReadAfterKill(t *testing.T) {
	requests := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/sessions/ao-1/kill":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["requireTerminationProof"] != true || body["deleteArtifacts"] != false {
				t.Fatalf("missing proof-required kill body: %#v (%v)", body, err)
			}
			_, _ = fmt.Fprint(w, `{"ok":true,"sessionId":"ao-1","freed":true,"cleanupPending":false,"terminationProof":{"source":"ao_chat_stop","method":"native_process_exit_waited","sessionId":"ao-1","verifiedAt":"2026-10-21T12:00:00Z","cleanupComplete":true}}`)
		case "/api/v1/sessions/ao-1":
			_, _ = fmt.Fprint(w, `{"session":{"id":"ao-1","status":"terminated","statusReadiness":"ready"}}`)
		default:
			t.Fatalf("unexpected route: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	session, kill, err := client.StopWorker(context.Background(), "ao-1")
	if err != nil {
		t.Fatal(err)
	}
	if !kill.Freed || kill.CleanupPending || session.Status != "terminated" || kill.TerminationProof == nil || kill.TerminationProof.Method != "native_process_exit_waited" {
		t.Fatalf("kill=%+v session=%+v", kill, session)
	}
	if len(requests) != 2 || requests[0] != "POST /api/v1/sessions/ao-1/kill" || requests[1] != "GET /api/v1/sessions/ao-1" {
		t.Fatalf("request order: %v", requests)
	}
}

func TestGetConversationKeepsLimitInQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/sessions/ao-1/conversation" || r.URL.Query().Get("limit") != "200" {
			t.Fatalf("unexpected conversation request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"sessionId":"ao-1","controller":"idle","latestSequence":0,"turns":[],"messages":[],"activities":[]}`)
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, Token: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetConversation(context.Background(), "ao-1"); err != nil {
		t.Fatal(err)
	}
}

func TestNewRejectsCredentialBearingBaseURL(t *testing.T) {
	if _, err := New(Config{BaseURL: "https://user:pass@example.test", Token: "secret"}); err == nil {
		t.Fatal("expected credential-bearing base URL rejection")
	}
}

func TestWorkerBootFailureRequiresStableCodeAndConflictStatus(t *testing.T) {
	for _, tc := range []struct {
		status   int
		code     string
		definite bool
	}{
		{409, "worker_boot_failed", true}, {500, "worker_boot_failed", false},
		{502, "worker_boot_failed", false}, {400, "worker_boot_failed", false},
		{409, "WORKER_BOOT_FAILED", false},
	} {
		t.Run(fmt.Sprintf("%d-%s", tc.status, tc.code), func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]string{"code": tc.code})
			}))
			defer srv.Close()
			client, _ := New(Config{BaseURL: srv.URL, Token: "fixture-token"})
			_, err := client.CreateWorker(context.Background(), WorkerSpec{ClientRequestID: "run", ProjectID: "p", Harness: "codex", DisplayName: "test", Prompt: "test"})
			var rejected CreateRejectedError
			if errors.As(err, &rejected) != tc.definite || requests != 1 {
				t.Fatalf("err=%v requests=%d, definite=%t", err, requests, tc.definite)
			}
			if !tc.definite && !errors.Is(err, ErrOutcomeUnknown) {
				t.Fatalf("unproven failure = %v", err)
			}
		})
	}
}

func TestListSessionsRequiresCompleteInventoryAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, body         string
		complete, identity bool
	}{
		{"empty-proof", `{"sessions":[],"complete":true,"clientRequestIdComplete":true}`, true, true},
		{"identified", `{"sessions":[{"id":"s","projectId":"p","clientRequestId":"run"}],"complete":true,"clientRequestIdComplete":true,"serverStartedAt":"2026-10-26T12:00:00Z"}`, true, true},
		{"legacy-row", `{"sessions":[{"id":"s"}],"complete":true,"clientRequestIdComplete":true}`, true, false},
		{"legacy-api", `{"sessions":[]}`, false, false},
		{"missing-array", `{"complete":true,"clientRequestIdComplete":true}`, false, false},
		{"truncated", `{"sessions":[],"complete":false,"clientRequestIdComplete":true}`, false, true},
		{"bad-id", `{"sessions":[{"clientRequestId":"run"}],"complete":true,"clientRequestIdComplete":true}`, false, true},
		{"duplicate", `{"sessions":[{"id":"s","clientRequestId":"a"},{"id":"s","clientRequestId":"b"}],"complete":true,"clientRequestIdComplete":true}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/sessions/inventory" || r.URL.RawQuery != "" {
					t.Errorf("unexpected inventory request: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Error("missing authorization")
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			client, _ := New(Config{BaseURL: srv.URL, Token: "fixture-token"})
			inventory, err := client.ListSessions(context.Background())
			if err != nil || inventory.Complete != tc.complete || inventory.ClientRequestIDComplete != tc.identity || requests != 1 {
				t.Fatalf("inventory=%+v err=%v requests=%d", inventory, err, requests)
			}
			if tc.name == "identified" && (inventory.Sessions[0].ClientRequestID != "run" || inventory.ServerStartedAt.IsZero()) {
				t.Fatal("request identity was lost")
			}
		})
	}
}

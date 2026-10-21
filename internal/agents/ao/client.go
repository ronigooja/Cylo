// Package ao contains the narrow AO gateway used by the independent Cylo runner.
// It intentionally exposes worker creation, read-only reconciliation, and stop;
// it does not expose AO's autonomous orchestration, send, or arbitrary lifecycle APIs.
package ao

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

var ErrOutcomeUnknown = errors.New("AO operation outcome is unknown")

type Config struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

type Client struct {
	base   *url.URL
	token  string
	client *http.Client
}

type WorkerSpec struct {
	ClientRequestID string
	ProjectID       string
	Harness         string
	Model           string
	Effort          string
	ApprovalMode    string
	DisplayName     string
	Prompt          string
}

type Session struct {
	ID              string `json:"id"`
	ProjectID       string `json:"projectId"`
	Status          string `json:"status"`
	StatusReadiness string `json:"statusReadiness"`
	Activity        struct {
		State string `json:"state"`
	} `json:"activity"`
}

type SpawnResponse struct {
	Session Session `json:"session"`
}

type SessionResponse struct {
	Session Session `json:"session"`
}

type Turn struct {
	ID          string  `json:"id"`
	State       string  `json:"state"`
	RequestedAt string  `json:"requestedAt"`
	CompletedAt *string `json:"completedAt,omitempty"`
}

type Message struct {
	ID        string `json:"id"`
	TurnID    string `json:"turnId,omitempty"`
	Sequence  int64  `json:"sequence"`
	Revision  int64  `json:"revision"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	Streaming bool   `json:"streaming"`
	CreatedAt string `json:"createdAt"`
}

type Activity struct {
	ID           string         `json:"id"`
	TurnID       string         `json:"turnId,omitempty"`
	Sequence     int64          `json:"sequence"`
	ActivityKind string         `json:"activityKind"`
	Status       string         `json:"status"`
	Summary      string         `json:"summary"`
	Detail       map[string]any `json:"detail,omitempty"`
	CreatedAt    string         `json:"createdAt"`
}

type Conversation struct {
	SessionID      string     `json:"sessionId"`
	Controller     string     `json:"controller"`
	LatestSequence int64      `json:"latestSequence"`
	Turns          []Turn     `json:"turns"`
	Messages       []Message  `json:"messages"`
	Activities     []Activity `json:"activities"`
}

type KillResponse struct {
	OK               bool              `json:"ok"`
	SessionID        string            `json:"sessionId"`
	Freed            bool              `json:"freed"`
	CleanupPending   bool              `json:"cleanupPending"`
	TerminationProof *TerminationProof `json:"terminationProof,omitempty"`
}

type TerminationProof struct {
	Source          string    `json:"source"`
	Method          string    `json:"method"`
	SessionID       string    `json:"sessionId"`
	VerifiedAt      time.Time `json:"verifiedAt"`
	CleanupComplete bool      `json:"cleanupComplete"`
}

func New(cfg Config) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("AO base URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("AO token is required")
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{base: u, token: cfg.Token, client: client}, nil
}

// CreateWorker uses AO's worker-only session endpoint. When the response is
// lost, it repeats the identical request with the same clientRequestId; AO
// deduplicates that key and rejects a different request body.
func (c *Client) CreateWorker(ctx context.Context, spec WorkerSpec) (Session, error) {
	if spec.ClientRequestID == "" || len(spec.ClientRequestID) > 128 || spec.ProjectID == "" || spec.Harness == "" || spec.DisplayName == "" || spec.Prompt == "" {
		return Session{}, errors.New("AO worker spec is incomplete")
	}
	body := map[string]any{
		"clientRequestId": spec.ClientRequestID,
		"projectId":       spec.ProjectID,
		"kind":            "worker",
		"mode":            "chat",
		"harness":         spec.Harness,
		"displayName":     spec.DisplayName,
		"prompt":          spec.Prompt,
	}
	if spec.Model != "" {
		body["model"] = spec.Model
	}
	if spec.Effort != "" {
		body["effort"] = spec.Effort
	}
	if spec.ApprovalMode != "" {
		body["approvalMode"] = spec.ApprovalMode
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return Session{}, fmt.Errorf("encode AO worker request: %w", err)
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		var response SpawnResponse
		status, err := c.do(ctx, http.MethodPost, "/api/v1/sessions", encoded, &response)
		if err == nil {
			if status < 200 || status >= 300 {
				return Session{}, fmt.Errorf("AO worker creation rejected (HTTP %d)", status)
			}
			if response.Session.ID == "" {
				// A successful response without the identifier does not prove that AO
				// failed to create the worker. Replay the identical idempotent request.
				lastErr = fmt.Errorf("AO worker creation returned no session id (HTTP %d)", status)
				continue
			}
			return response.Session, nil
		}
		lastErr = err
		// A malformed/truncated 2xx response can follow a successful create. The
		// endpoint's clientRequestId makes an identical replay safe.
		if !retryableUnknown(err) {
			return Session{}, err
		}
	}
	return Session{}, fmt.Errorf("%w: %v", ErrOutcomeUnknown, lastErr)
}

func (c *Client) GetSession(ctx context.Context, id string) (Session, error) {
	if id == "" {
		return Session{}, errors.New("AO session id is required")
	}
	var response SessionResponse
	_, err := c.do(ctx, http.MethodGet, "/api/v1/sessions/"+url.PathEscape(id), nil, &response)
	if err != nil {
		return Session{}, err
	}
	if response.Session.ID == "" {
		return Session{}, errors.New("AO session read returned no session id")
	}
	return response.Session, nil
}

func (c *Client) GetConversation(ctx context.Context, id string) (Conversation, error) {
	if id == "" {
		return Conversation{}, errors.New("AO session id is required")
	}
	var response Conversation
	_, err := c.do(ctx, http.MethodGet, "/api/v1/sessions/"+url.PathEscape(id)+"/conversation?limit=200", nil, &response)
	if err != nil {
		return Conversation{}, err
	}
	return response, nil
}

// StopWorker requests AO termination once, then verifies the session state.
// A lost kill response is never retried automatically because AO kill is not
// documented as an idempotent operation; the caller must reconcile via GET.
func (c *Client) StopWorker(ctx context.Context, id string) (Session, KillResponse, error) {
	if id == "" {
		return Session{}, KillResponse{}, errors.New("AO session id is required")
	}
	encoded, _ := json.Marshal(map[string]bool{"deleteArtifacts": false, "requireTerminationProof": true})
	var result KillResponse
	_, err := c.do(ctx, http.MethodPost, "/api/v1/sessions/"+url.PathEscape(id)+"/kill", encoded, &result)
	if err != nil {
		return Session{}, KillResponse{}, fmt.Errorf("%w: AO kill request: %v", ErrOutcomeUnknown, err)
	}
	if result.SessionID != id {
		return Session{}, KillResponse{}, errors.New("AO kill response session id mismatch")
	}
	session, err := c.GetSession(ctx, id)
	if err != nil {
		return Session{}, result, fmt.Errorf("%w: verify AO termination: %v", ErrOutcomeUnknown, err)
	}
	return session, result, nil
}

func (c *Client) do(ctx context.Context, method, endpoint string, body []byte, out any) (int, error) {
	u := *c.base
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil {
		return 0, fmt.Errorf("parse AO endpoint: %w", err)
	}
	u.Path = path.Join(c.base.Path, parsedEndpoint.Path)
	u.RawQuery = parsedEndpoint.RawQuery
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, statusError{status: response.StatusCode}
	}
	if out == nil || response.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return response.StatusCode, nil
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4<<20))
	if err := decoder.Decode(out); err != nil {
		return response.StatusCode, responseDecodeError{err: err}
	}
	return response.StatusCode, nil
}

type statusError struct{ status int }

func (e statusError) Error() string { return fmt.Sprintf("AO HTTP status %d", e.status) }

type responseDecodeError struct{ err error }

func (e responseDecodeError) Error() string { return fmt.Sprintf("decode AO response: %v", e.err) }
func (e responseDecodeError) Unwrap() error { return e.err }

func retryableUnknown(err error) bool {
	var decodeErr responseDecodeError
	if errors.As(err, &decodeErr) {
		return true
	}
	var status statusError
	if errors.As(err, &status) {
		return status.status >= 500 || status.status == http.StatusRequestTimeout || status.status == http.StatusGatewayTimeout
	}
	var netErr net.Error
	return errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF)
}

// Package opencode is the narrow HTTP client for Zoe's per-run OpenCode server.
package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

var ErrOutcomeUnknown = errors.New("OpenCode operation outcome is unknown")

type HTTPError struct {
	Status int
	Code   string
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("OpenCode HTTP %d%s", e.Status, func() string {
		if e.Code == "" {
			return ""
		}
		return " (" + e.Code + ")"
	}())
}

type Config struct {
	BaseURL, Username, Password string
	Client                      *http.Client
}
type Client struct {
	base               *url.URL
	username, password string
	client             *http.Client
}

type Session struct {
	ID     string         `json:"id"`
	Title  string         `json:"title"`
	Time   map[string]any `json:"time,omitempty"`
	Tokens map[string]any `json:"tokens,omitempty"`
	Cost   float64        `json:"cost,omitempty"`
}
type Status struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}
type Message struct {
	Info struct {
		ID   string `json:"id"`
		Role string `json:"role"`
		Time struct {
			Created   int64 `json:"created"`
			Completed int64 `json:"completed"`
		} `json:"time"`
		Finish string `json:"finish"`
		Error  any    `json:"error,omitempty"`
	} `json:"info"`
	Parts []map[string]any `json:"parts,omitempty"`
}
type Event struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties,omitempty"`
}
type Health struct {
	Healthy bool   `json:"healthy"`
	Version string `json:"version"`
}

func New(cfg Config) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("OpenCode base URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	if strings.TrimSpace(cfg.Password) == "" {
		return nil, errors.New("OpenCode server password is required")
	}
	hc := cfg.Client
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	if cfg.Username == "" {
		cfg.Username = "opencode"
	}
	return &Client{base: u, username: cfg.Username, password: cfg.Password, client: hc}, nil
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	var h Health
	_, err := c.do(ctx, http.MethodGet, "/global/health", nil, &h)
	return h, err
}
func (c *Client) CreateSession(ctx context.Context, title string) (Session, error) {
	if strings.TrimSpace(title) == "" {
		return Session{}, errors.New("OpenCode session title is required")
	}
	body, _ := json.Marshal(map[string]string{"title": title})
	var s Session
	_, err := c.do(ctx, http.MethodPost, "/session", body, &s)
	if err != nil {
		return s, err
	}
	if s.ID == "" {
		return s, fmt.Errorf("%w: session response has no id", ErrOutcomeUnknown)
	}
	return s, nil
}
func (c *Client) PromptAsync(ctx context.Context, sessionID string, prompt any) error {
	if !validID(sessionID) {
		return errors.New("OpenCode session id is required")
	}
	body, err := json.Marshal(prompt)
	if err != nil {
		return fmt.Errorf("encode prompt: %w", err)
	}
	_, err = c.do(ctx, http.MethodPost, "/session/"+url.PathEscape(sessionID)+"/prompt_async", body, nil)
	if err != nil {
		return fmt.Errorf("%w: prompt_async: %v", ErrOutcomeUnknown, err)
	}
	return nil
}
func (c *Client) Status(ctx context.Context) (map[string]Status, error) {
	var s map[string]Status
	_, err := c.do(ctx, http.MethodGet, "/session/status", nil, &s)
	return s, err
}
func (c *Client) Messages(ctx context.Context, sessionID string) ([]Message, error) {
	var m []Message
	_, err := c.do(ctx, http.MethodGet, "/session/"+url.PathEscape(sessionID)+"/message", nil, &m)
	return m, err
}
func (c *Client) Abort(ctx context.Context, sessionID string) error {
	_, err := c.do(ctx, http.MethodPost, "/session/"+url.PathEscape(sessionID)+"/abort", []byte("{}"), nil)
	if err != nil {
		return fmt.Errorf("%w: abort: %v", ErrOutcomeUnknown, err)
	}
	return nil
}

// Completion reports terminal state only after the server is no longer busy and
// the latest assistant message carries a completion, finish, or error marker.
func Completion(status Status, messages []Message) (Message, bool, error) {
	if status.Type == "busy" || len(messages) == 0 {
		return Message{}, false, nil
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Info.Role != "assistant" {
			continue
		}
		m := messages[i]
		if m.Info.Time.Completed == 0 && m.Info.Finish == "" && m.Info.Error == nil {
			return Message{}, false, nil
		}
		if m.Info.Error != nil {
			return m, true, fmt.Errorf("OpenCode assistant error: %v", m.Info.Error)
		}
		return m, true, nil
	}
	return Message{}, false, nil
}

// Events streams /event SSE records. The caller must cancel ctx to close it.
func (c *Client) Events(ctx context.Context) (<-chan Event, <-chan error) {
	out := make(chan Event)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		req, err := c.request(ctx, http.MethodGet, "/event", nil)
		if err != nil {
			errs <- err
			return
		}
		resp, err := c.client.Do(req)
		if err != nil {
			errs <- err
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errs <- &HTTPError{Status: resp.StatusCode}
			return
		}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		var data strings.Builder
		flush := func() {
			if data.Len() == 0 {
				return
			}
			var e Event
			if json.Unmarshal([]byte(data.String()), &e) == nil {
				select {
				case out <- e:
				case <-ctx.Done():
				}
			}
			data.Reset()
		}
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				flush()
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		flush()
		if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
			errs <- err
		}
	}()
	return out, errs
}

func (c *Client) request(ctx context.Context, method, endpoint string, body []byte) (*http.Request, error) {
	u := *c.base
	p, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	u.Path = path.Join(c.base.Path, p.Path)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}
func (c *Client) do(ctx context.Context, method, endpoint string, body []byte, out any) (int, error) {
	req, err := c.request(ctx, method, endpoint, body)
	if err != nil {
		return 0, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		var e struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(b, &e)
		return resp.StatusCode, &HTTPError{Status: resp.StatusCode, Code: e.Code, Body: string(b)}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("decode OpenCode response: %w", err)
	}
	return resp.StatusCode, nil
}
func validID(id string) bool { return strings.TrimSpace(id) != "" && len(id) <= 256 }

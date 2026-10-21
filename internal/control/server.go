package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/storage"
)

type Config struct {
	BootstrapCode      string
	RunnerToken        string
	RunnerID           string
	RunnerWorkspaceIDs []string
	RunnerScopes       []string
	DefaultProfileID   string
	RuntimeRouteID     string
	ModelRouteID       string
	Profiles           map[string]RuntimeProfile
	PairingTTL         time.Duration
	MaxRequestBytes    int64
}

type RuntimeProfile struct {
	ID              string
	RuntimeRouteID  string
	ModelRouteID    string
	Role            string
	AOAgentKind     string
	IsolationPolicy string
	CapabilityIDs   []string
	Scopes          []string
}

type CommandContext struct {
	CommandID      string
	Principal      contracts.Principal
	IdempotencyKey string
	AcceptedAt     time.Time
}

type CommandHandler func(context.Context, *sql.Tx, CommandContext, contracts.CommandRequest) (contracts.CommandResult, []contracts.Event, error)

type APIError struct {
	Status  int
	Code    string
	Message string
	Details any
	Cause   error
}

func (e *APIError) Error() string {
	if e.Cause != nil {
		return e.Code + ": " + e.Cause.Error()
	}
	return e.Code + ": " + e.Message
}
func (e *APIError) Unwrap() error { return e.Cause }

func apiError(status int, code, message string, details any) *APIError {
	return &APIError{Status: status, Code: code, Message: message, Details: details}
}

func NewAPIError(status int, code, message string, details any) *APIError {
	return apiError(status, code, message, details)
}

type Server struct {
	store    *storage.Store
	cfg      Config
	commands map[string]CommandHandler
	public   *http.ServeMux
	device   *http.ServeMux
	runner   *http.ServeMux
	mu       sync.RWMutex
	sealed   bool
}

func NewServer(store *storage.Store, cfg Config) (*Server, error) {
	if store == nil || store.DB() == nil {
		return nil, errors.New("control server requires an open store")
	}
	if cfg.PairingTTL <= 0 {
		cfg.PairingTTL = 15 * time.Minute
	}
	if cfg.MaxRequestBytes <= 0 {
		cfg.MaxRequestBytes = 1 << 20
	}
	for id, profile := range cfg.Profiles {
		if id == "" || profile.ID != id || profile.RuntimeRouteID == "" || profile.ModelRouteID == "" || profile.Role == "" || profile.AOAgentKind != "worker" || profile.IsolationPolicy == "" {
			return nil, fmt.Errorf("runtime profile %q is incomplete or does not select AO worker execution", id)
		}
	}
	if cfg.DefaultProfileID != "" {
		if _, ok := cfg.Profiles[cfg.DefaultProfileID]; !ok {
			return nil, errors.New("default profile is not present in configured profiles")
		}
	}
	s := &Server{store: store, cfg: cfg, commands: make(map[string]CommandHandler), public: http.NewServeMux(), device: http.NewServeMux(), runner: http.NewServeMux()}
	s.registerCoreRoutes()
	return s, nil
}

func (s *Server) RegisterCommand(kind string, h CommandHandler) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealed {
		return errors.New("server registrations are sealed")
	}
	if strings.TrimSpace(kind) == "" || h == nil {
		return errors.New("command kind and handler are required")
	}
	if _, ok := s.commands[kind]; ok {
		return fmt.Errorf("command %q already registered", kind)
	}
	s.commands[kind] = h
	return nil
}

// RegisterAuthenticatedRoutes registers feature routes behind device bearer auth.
func (s *Server) RegisterAuthenticatedRoutes(register func(*http.ServeMux)) error {
	if register == nil {
		return errors.New("route registration callback is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealed {
		return errors.New("server registrations are sealed")
	}
	register(s.device)
	return nil
}

// RegisterRunnerRoutes registers runner routes behind the independent runner credential.
func (s *Server) RegisterRunnerRoutes(register func(*http.ServeMux)) error {
	if register == nil {
		return errors.New("route registration callback is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealed {
		return errors.New("server registrations are sealed")
	}
	register(s.runner)
	return nil
}

func HasScope(p contracts.Principal, scope string) bool {
	for _, value := range p.Scopes {
		if value == scope || value == "*" {
			return true
		}
	}
	return false
}

func HasRunnerScope(p contracts.RunnerPrincipal, scope, workspaceID string) bool {
	allowed := false
	for _, value := range p.Scopes {
		if value == scope || value == "*" {
			allowed = true
			break
		}
	}
	if !allowed {
		return false
	}
	for _, value := range p.WorkspaceIDs {
		if value == workspaceID || value == "*" {
			return true
		}
	}
	return false
}

// DeviceTransaction binds a read or write transaction to the authenticated principal in ctx.
// Mutating feature handlers should prefer CommandHandler so receipt and outbox remain atomic.
func (s *Server) DeviceTransaction(ctx context.Context, fn func(*sql.Tx, contracts.Principal) error) error {
	p, ok := PrincipalFromContext(ctx)
	if !ok {
		return apiError(http.StatusUnauthorized, "unauthenticated", "device authentication required", nil)
	}
	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx, p); err != nil {
		return err
	}
	return tx.Commit()
}

// RunnerTransaction binds a transaction to the runner identity, workspace allowlist, and scopes in ctx.
func (s *Server) RunnerTransaction(ctx context.Context, fn func(*sql.Tx, contracts.RunnerPrincipal) error) error {
	p, ok := RunnerPrincipalFromContext(ctx)
	if !ok {
		return apiError(http.StatusUnauthorized, "unauthenticated", "runner authentication required", nil)
	}
	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx, p); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) Handler() http.Handler {
	s.mu.Lock()
	s.sealed = true
	s.mu.Unlock()
	root := http.NewServeMux()
	root.HandleFunc("POST /v1/bootstrap", s.handleBootstrap)
	root.HandleFunc("POST /v1/pairings", s.handlePairingCreate)
	root.Handle("GET /v1/pairings/{pairing_id}", s.deviceAuth(http.HandlerFunc(s.handlePairingStatus)))
	root.Handle("/v1/", s.deviceAuth(s.device))
	root.Handle("/runner/v1/", s.runnerAuth(s.runner))
	root.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))
	root.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, apiError(http.StatusNotFound, "not_found", "route not found", nil))
	}))
	return requestLimit(s.cfg.MaxRequestBytes, root)
}

func PrincipalFromContext(ctx context.Context) (contracts.Principal, bool) {
	p, ok := ctx.Value(devicePrincipalKey{}).(contracts.Principal)
	return p, ok
}

func RunnerPrincipalFromContext(ctx context.Context) (contracts.RunnerPrincipal, bool) {
	p, ok := ctx.Value(runnerPrincipalKey{}).(contracts.RunnerPrincipal)
	return p, ok
}

type devicePrincipalKey struct{}
type runnerPrincipalKey struct{}

func writeError(w http.ResponseWriter, err error) {
	var api *APIError
	if !errors.As(err, &api) {
		api = apiError(http.StatusInternalServerError, "internal", "internal server error", nil)
	}
	writeJSON(w, api.Status, contracts.ErrorEnvelope{Error: contracts.APIError{Code: api.Code, Message: api.Message, Details: api.Details}})
}

func WriteError(w http.ResponseWriter, err error) { writeError(w, err) }

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return apiError(http.StatusBadRequest, "invalid_request", "invalid JSON request", nil)
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return apiError(http.StatusBadRequest, "invalid_request", "request must contain one JSON value", nil)
	}
	return nil
}

func requestLimit(max int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, max)
		}
		next.ServeHTTP(w, r)
	})
}

func jsonMarshal(v any) ([]byte, error)   { return json.Marshal(v) }
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

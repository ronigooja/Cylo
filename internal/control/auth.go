package control

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ronigooja/Cylo/contracts"
)

func tokenBytes(token string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != token {
		return nil, errors.New("invalid token encoding")
	}
	return b, nil
}

func tokenHash(token string) ([]byte, error) {
	b, err := tokenBytes(token)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(b)
	return h[:], nil
}

func bearer(r *http.Request) string {
	value := r.Header.Get("Authorization")
	if len(value) < 8 || !strings.EqualFold(value[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(value[7:])
}

func (s *Server) deviceAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided, err := tokenHash(bearer(r))
		if err != nil {
			writeError(w, apiError(http.StatusUnauthorized, "unauthenticated", "valid device bearer required", nil))
			return
		}
		var p contracts.Principal
		var status, expires string
		err = s.store.DB().QueryRowContext(r.Context(), `SELECT workspace_id,subject_id,id,status,coalesce(pairing_expires_at,'') FROM devices WHERE token_hash=?`, provided).Scan(&p.WorkspaceID, &p.SubjectID, &p.DeviceID, &status, &expires)
		if err != nil {
			writeError(w, apiError(http.StatusUnauthorized, "unauthenticated", "device credential is not recognized", nil))
			return
		}
		if status == "pending" && r.Method == http.MethodGet && r.URL.Path == "/v1/pairings/"+p.DeviceID {
			if expires != "" {
				deadline, parseErr := time.Parse(time.RFC3339Nano, expires)
				if parseErr == nil && time.Now().After(deadline) {
					writeError(w, apiError(http.StatusGone, "pairing_expired", "pairing request has expired", nil))
					return
				}
			}
			p.Scopes = []string{"pairing:status"}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), devicePrincipalKey{}, p)))
			return
		}
		if status != "approved" {
			writeError(w, apiError(http.StatusForbidden, "device_not_approved", "device is not approved", nil))
			return
		}
		p.Scopes = append([]string(nil), s.cfg.DeviceScopes...)
		if required := requiredCoreDeviceScope(r); required != "" && !HasScope(p, required) {
			writeError(w, apiError(http.StatusForbidden, "forbidden", "device lacks required scope", map[string]string{"scope": required}))
			return
		}
		_, _ = s.store.DB().ExecContext(r.Context(), `UPDATE devices SET last_seen_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, p.DeviceID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), devicePrincipalKey{}, p)))
	})
}

func requiredCoreDeviceScope(r *http.Request) string {
	switch r.URL.Path {
	case "/v1/snapshot", "/v1/events", "/v1/notifications":
		return "client:read"
	case "/v1/commands":
		if r.Method == http.MethodGet {
			return "client:read"
		}
		if r.Method == http.MethodPost {
			return "client:command"
		}
	}
	return ""
}

func (s *Server) runnerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash, err := tokenHash(bearer(r))
		if err != nil {
			writeError(w, apiError(http.StatusUnauthorized, "unauthenticated", "valid runner bearer required", nil))
			return
		}
		var p contracts.RunnerPrincipal
		var workspaceJSON, scopesJSON []byte
		var revoked sql.NullString
		err = s.store.DB().QueryRowContext(r.Context(), `SELECT runner_id,workspace_ids,scopes,revoked_at FROM runner_credentials WHERE token_hash=?`, hash).Scan(&p.RunnerID, &workspaceJSON, &scopesJSON, &revoked)
		if err == nil {
			if revoked.Valid {
				writeError(w, apiError(http.StatusUnauthorized, "unauthenticated", "runner credential is revoked", nil))
				return
			}
			if e := jsonUnmarshal(workspaceJSON, &p.WorkspaceIDs); e != nil {
				writeError(w, apiError(500, "internal", "runner credential record is invalid", nil))
				return
			}
			if e := jsonUnmarshal(scopesJSON, &p.Scopes); e != nil {
				writeError(w, apiError(500, "internal", "runner credential record is invalid", nil))
				return
			}
		} else if errors.Is(err, sql.ErrNoRows) && s.cfg.RunnerToken != "" {
			configured, e := tokenHash(s.cfg.RunnerToken)
			if e == nil && len(configured) == len(hash) && subtle.ConstantTimeCompare(configured, hash) == 1 {
				p = contracts.RunnerPrincipal{RunnerID: s.cfg.RunnerID, WorkspaceIDs: append([]string(nil), s.cfg.RunnerWorkspaceIDs...), Scopes: append([]string(nil), s.cfg.RunnerScopes...)}
				if p.RunnerID == "" {
					p.RunnerID = "configured-runner"
				}
				if len(p.Scopes) == 0 {
					p.Scopes = []string{"runner:claim", "runner:observe", "runner:control:read", "runner:propose"}
				}
			} else {
				writeError(w, apiError(http.StatusUnauthorized, "unauthenticated", "runner credential is not recognized", nil))
				return
			}
		} else {
			writeError(w, apiError(http.StatusUnauthorized, "unauthenticated", "runner credential is not recognized", nil))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), runnerPrincipalKey{}, p)))
	})
}

// RegisterRunnerCredential stores only a digest of a deployment-provisioned bearer.
func (s *Server) RegisterRunnerCredential(ctx context.Context, token, runnerID string, workspaceIDs, scopes []string) error {
	return RegisterRunnerCredentialIfMissing(ctx, s.store, token, runnerID, workspaceIDs, scopes)
}

// RegisterRunnerCredentialIfMissing provisions the deployment credential once
// and preserves a later database revocation across server restarts.
func RegisterRunnerCredentialIfMissing(ctx context.Context, store interface{ DB() *sql.DB }, token, runnerID string, workspaceIDs, scopes []string) error {
	if store == nil || store.DB() == nil {
		return errors.New("open control store is required")
	}
	hash, err := tokenHash(token)
	if err != nil {
		return err
	}
	workspaces, _ := jsonMarshal(workspaceIDs)
	scopeBytes, _ := jsonMarshal(scopes)
	_, err = store.DB().ExecContext(ctx, `INSERT OR IGNORE INTO runner_credentials(token_hash,runner_id,workspace_ids,scopes) VALUES(?,?,?,?)`, hash, runnerID, workspaces, scopeBytes)
	return err
}

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
		p.Scopes = []string{"client:read", "client:command", "device:manage"}
		_, _ = s.store.DB().ExecContext(r.Context(), `UPDATE devices SET last_seen_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, p.DeviceID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), devicePrincipalKey{}, p)))
	})
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
					p.Scopes = []string{"runner:claim", "runner:observe", "runner:control:read"}
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
	hash, err := tokenHash(token)
	if err != nil {
		return err
	}
	workspaces, _ := jsonMarshal(workspaceIDs)
	scopeBytes, _ := jsonMarshal(scopes)
	_, err = s.store.DB().ExecContext(ctx, `INSERT INTO runner_credentials(token_hash,runner_id,workspace_ids,scopes) VALUES(?,?,?,?)`, hash, runnerID, workspaces, scopeBytes)
	return err
}

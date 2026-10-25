package control

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/storage"
)

func tokenBytes(token string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != token {
		return nil, errors.New("invalid token encoding")
	}
	return b, nil
}

func tokenHash(token string) ([]byte, error) { return instanceKeyHash(token) }

func bearer(r *http.Request) string {
	value := r.Header.Get("Authorization")
	if len(value) < 8 || !strings.EqualFold(value[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(value[7:])
}

// GenerateInstanceKey returns a 256-bit URL-safe bearer suitable for QR import.
func GenerateInstanceKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func instanceKeyHash(key string) ([]byte, error) {
	b, err := tokenBytes(key)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	return sum[:], nil
}

// InitializeInstanceAuth provisions the first key or verifies the configured
// deployment key. It returns the cleartext key only when it was generated now.
func InitializeInstanceAuth(ctx context.Context, store *storage.Store, configured string) (string, bool, error) {
	if store == nil || store.DB() == nil {
		return "", false, errors.New("open control store is required")
	}
	configured = strings.TrimSpace(configured)
	if configured != "" {
		if _, err := instanceKeyHash(configured); err != nil {
			return "", false, errors.New("instance key must be a 32-byte base64url credential")
		}
	}
	var hash []byte
	err := store.DB().QueryRowContext(ctx, `SELECT key_hash FROM instance_auth WHERE singleton=1`).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		key := configured
		generated := false
		if key == "" {
			key, err = GenerateInstanceKey()
			if err != nil {
				return "", false, err
			}
			generated = true
		}
		digest, e := instanceKeyHash(key)
		if e != nil {
			return "", false, e
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, e = store.DB().ExecContext(ctx, `INSERT INTO instance_auth(singleton,key_hash,created_at,rotated_at) VALUES(1,?,?,?)`, digest, now, now); e != nil {
			return "", false, e
		}
		if e = ensureInstanceDevice(ctx, store.DB(), digest, now); e != nil {
			return "", false, e
		}
		return key, generated, nil
	}
	if err != nil {
		return "", false, err
	}
	if configured != "" {
		digest, e := instanceKeyHash(configured)
		if e != nil {
			return "", false, e
		}
		if len(hash) != len(digest) || subtle.ConstantTimeCompare(hash, digest) != 1 {
			return "", false, errors.New("configured instance key does not match the stored instance credential")
		}
	}
	if err = ensureInstanceDevice(ctx, store.DB(), hash, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return "", false, err
	}
	return "", false, nil
}

func ensureInstanceDevice(ctx context.Context, db *sql.DB, digest []byte, now string) error {
	// Existing domain tables retain a synthetic owner row for foreign-key and
	// audit compatibility; it is never used for authentication or approval.
	_, err := db.ExecContext(ctx, `INSERT INTO devices(id,workspace_id,subject_id,name,status,token_hash,created_at,approved_at) VALUES('instance','default','owner','This instance','approved',?,?,?) ON CONFLICT(id) DO UPDATE SET token_hash=excluded.token_hash,status='approved',revoked_at=NULL`, digest, now, now)
	return err
}

func (s *Server) instanceAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided, err := instanceKeyHash(bearer(r))
		if err != nil {
			writeError(w, apiError(http.StatusUnauthorized, "unauthenticated", "valid instance bearer required", nil))
			return
		}
		var expected []byte
		if err = s.store.DB().QueryRowContext(r.Context(), `SELECT key_hash FROM instance_auth WHERE singleton=1`).Scan(&expected); err != nil || len(expected) != len(provided) || subtle.ConstantTimeCompare(expected, provided) != 1 {
			writeError(w, apiError(http.StatusUnauthorized, "unauthenticated", "instance credential is not recognized", nil))
			return
		}
		p := contracts.Principal{WorkspaceID: "default", SubjectID: "owner", DeviceID: "instance", Scopes: append([]string(nil), s.cfg.InstanceScopes...)}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), devicePrincipalKey{}, p)))
	})
}

// Recheck within each mutation transaction. Instance credentials are rotated
// atomically by replacing the digest, so no device/session revocation lookup is
// needed here.
func requireOwnerSessionTx(ctx context.Context, tx *sql.Tx, p contracts.Principal) error {
	if p.WorkspaceID != "default" || p.SubjectID != "owner" || p.DeviceID != "instance" {
		return apiError(401, "unauthenticated", "instance session is invalid", nil)
	}
	var present int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM instance_auth WHERE singleton=1`).Scan(&present); err != nil {
		return err
	}
	if present != 1 {
		return apiError(401, "unauthenticated", "instance credential is not initialized", nil)
	}
	return nil
}

func (s *Server) runnerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := bearer(r)
		digest, err := instanceKeyHash(provided)
		if err != nil {
			writeError(w, apiError(http.StatusUnauthorized, "unauthenticated", "valid instance bearer required", nil))
			return
		}
		var expected []byte
		if err = s.store.DB().QueryRowContext(r.Context(), `SELECT key_hash FROM instance_auth WHERE singleton=1`).Scan(&expected); err != nil || len(expected) != len(digest) || subtle.ConstantTimeCompare(expected, digest) != 1 {
			writeError(w, apiError(http.StatusUnauthorized, "unauthenticated", "instance credential is not recognized", nil))
			return
		}
		scopes := append([]string(nil), s.cfg.RunnerScopes...)
		if len(scopes) == 0 {
			scopes = []string{"runner:claim", "runner:observe", "runner:control:read", "runner:propose", "runner:budget"}
		}
		workspaces := append([]string(nil), s.cfg.RunnerWorkspaceIDs...)
		if len(workspaces) == 0 {
			workspaces = []string{"default"}
		}
		id := s.cfg.RunnerID
		if id == "" {
			id = "instance-runner"
		}
		p := contracts.RunnerPrincipal{RunnerID: id, WorkspaceIDs: workspaces, Scopes: scopes}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), runnerPrincipalKey{}, p)))
	})
}

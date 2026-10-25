package control

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ronigooja/Cylo/contracts"
)

const ownerPasswordIterations = 600000

func ownerPassword(password string, salt []byte, iterations int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, iterations, 32)
}

func (s *Server) handleOwnerStatus(w http.ResponseWriter, r *http.Request) {
	var required bool
	if err := s.store.DB().QueryRowContext(r.Context(), `SELECT password_hash IS NULL FROM owner_auth WHERE id=1`).Scan(&required); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, contracts.OwnerAuthStatus{SetupRequired: required})
}
func (s *Server) handleOwnerSetup(w http.ResponseWriter, r *http.Request) {
	s.ownerAuthenticate(w, r, "setup")
}
func (s *Server) handleOwnerLogin(w http.ResponseWriter, r *http.Request) {
	s.ownerAuthenticate(w, r, "login")
}

func (s *Server) ownerAuthenticate(w http.ResponseWriter, r *http.Request, kind string) {
	var req contracts.OwnerAuthRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.DeviceName = strings.TrimSpace(req.DeviceName)
	token, err := tokenHash(req.DeviceToken)
	if err != nil || req.RequestID == "" || len(req.RequestID) > 128 || req.DeviceName == "" || len(req.DeviceName) > 128 || len(req.Password) < 12 || len(req.Password) > 1024 {
		writeError(w, apiError(400, "invalid_request", "request ID, device name, random device token and password of 12–1024 bytes are required", nil))
		return
	}
	// Exclude password from the durable request fingerprint. Every retry still
	// verifies the adaptive password hash; no fast password verifier is stored.
	fields, _ := json.Marshal([]string{kind, req.RequestID, req.DeviceName, req.DeviceToken, req.Code})
	fp := sha256.Sum256(fields)
	tx, err := s.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback()
	var subject string
	var salt, hash []byte
	var generation, iterations, failures int
	var blocked int64
	err = tx.QueryRowContext(r.Context(), `SELECT subject_id,password_salt,password_hash,password_iterations,generation,failed_attempts,blocked_until FROM owner_auth WHERE id=1`).Scan(&subject, &salt, &hash, &iterations, &generation, &failures, &blocked)
	if err != nil {
		writeError(w, err)
		return
	}
	now := time.Now().UTC()
	if blocked > now.Unix() {
		w.Header().Set("Retry-After", "900")
		writeError(w, apiError(429, "auth_rate_limited", "too many failed authentication attempts; retry later", nil))
		return
	}
	if blocked != 0 {
		failures = 0
	}
	deny := func() {
		failures++
		until := int64(0)
		if failures >= 5 {
			until = now.Add(15 * time.Minute).Unix()
		}
		if _, e := tx.ExecContext(r.Context(), `UPDATE owner_auth SET failed_attempts=?,blocked_until=? WHERE id=1`, failures, until); e != nil {
			writeError(w, e)
			return
		}
		if e := tx.Commit(); e != nil {
			writeError(w, e)
			return
		}
		writeError(w, apiError(401, "invalid_credentials", "owner credentials are invalid", nil))
	}
	var oldFP []byte
	var oldID, oldKind string
	var oldGeneration int
	retryErr := tx.QueryRowContext(r.Context(), `SELECT fingerprint,device_id,kind,generation FROM owner_auth_requests WHERE request_id=?`, req.RequestID).Scan(&oldFP, &oldID, &oldKind, &oldGeneration)
	if retryErr != nil && !errors.Is(retryErr, sql.ErrNoRows) {
		writeError(w, retryErr)
		return
	}
	retry := retryErr == nil
	if retry && (oldKind != kind || oldGeneration != generation || subtle.ConstantTimeCompare(oldFP, fp[:]) != 1) {
		writeError(w, apiError(409, "idempotency_conflict", "request ID was already used with different authentication input", nil))
		return
	}
	if hash != nil {
		derived, e := ownerPassword(req.Password, salt, iterations)
		if e != nil {
			writeError(w, e)
			return
		}
		if subtle.ConstantTimeCompare(derived, hash) != 1 {
			deny()
			return
		}
		if kind == "setup" && !retry {
			writeError(w, apiError(409, "owner_already_setup", "owner password is already configured", nil))
			return
		}
	} else {
		if kind != "setup" {
			writeError(w, apiError(409, "setup_required", "owner password setup is required", nil))
			return
		}
		var stored []byte
		var consumed string
		e := tx.QueryRowContext(r.Context(), `SELECT code_hash,coalesce(consumed_by_device_id,'') FROM bootstrap_state WHERE id=1`).Scan(&stored, &consumed)
		if errors.Is(e, sql.ErrNoRows) && s.cfg.BootstrapCode != "" {
			stored = codeHash(s.cfg.BootstrapCode)
			_, e = tx.ExecContext(r.Context(), `INSERT INTO bootstrap_state(id,code_hash) VALUES(1,?)`, stored)
		}
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			writeError(w, e)
			return
		}
		if len(stored) == 0 || consumed != "" || subtle.ConstantTimeCompare(stored, codeHash(req.Code)) != 1 {
			deny()
			return
		}
		salt = make([]byte, 32)
		if _, err = rand.Read(salt); err != nil {
			writeError(w, err)
			return
		}
		hash, err = ownerPassword(req.Password, salt, ownerPasswordIterations)
		if err != nil {
			writeError(w, err)
			return
		}
		if subject == "" {
			subject, err = randomID()
			if err != nil {
				writeError(w, err)
				return
			}
		}
		if _, err = tx.ExecContext(r.Context(), `UPDATE owner_auth SET subject_id=?,password_salt=?,password_hash=?,password_iterations=? WHERE id=1`, subject, salt, hash, ownerPasswordIterations); err != nil {
			writeError(w, err)
			return
		}
	}
	if retry {
		device, e := scanDevice(tx.QueryRowContext(r.Context(), deviceIDQuery, oldID))
		if e != nil {
			writeError(w, e)
			return
		}
		if device.Status != "approved" {
			writeError(w, apiError(409, "device_revoked", "session has been revoked; create a new login request", nil))
			return
		}
		if _, e = tx.ExecContext(r.Context(), `UPDATE owner_auth SET failed_attempts=0,blocked_until=0 WHERE id=1`); e != nil {
			writeError(w, e)
			return
		}
		if e = tx.Commit(); e != nil {
			writeError(w, e)
			return
		}
		writeJSON(w, 200, contracts.DeviceRegistration{Device: device})
		return
	}
	var count int
	if err = tx.QueryRowContext(r.Context(), `SELECT count(*) FROM devices WHERE token_hash=?`, token).Scan(&count); err != nil {
		writeError(w, err)
		return
	}
	if count != 0 {
		writeError(w, apiError(409, "device_token_conflict", "use a fresh random device token for a new login", nil))
		return
	}
	id, err := randomID()
	if err != nil {
		writeError(w, err)
		return
	}
	at := now.Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO devices(id,workspace_id,subject_id,name,status,token_hash,created_at,approved_at,owner_generation) VALUES(?,'default',?,?,'approved',?,?,?,?)`, id, subject, req.DeviceName, token, at, at, generation); err != nil {
		writeError(w, err)
		return
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO owner_auth_requests(request_id,kind,fingerprint,device_id,generation,created_at) VALUES(?,?,?,?,?,?)`, req.RequestID, kind, fp[:], id, generation, at); err != nil {
		writeError(w, err)
		return
	}
	if kind == "setup" {
		if _, err = tx.ExecContext(r.Context(), `UPDATE bootstrap_state SET consumed_by_device_id=?,consumed_at=? WHERE id=1`, id, at); err != nil {
			writeError(w, err)
			return
		}
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE owner_auth SET failed_attempts=0,blocked_until=0 WHERE id=1`); err != nil {
		writeError(w, err)
		return
	}
	device, err := scanDevice(tx.QueryRowContext(r.Context(), deviceIDQuery, id))
	if err != nil {
		writeError(w, err)
		return
	}
	principal := contracts.Principal{WorkspaceID: "default", SubjectID: subject, DeviceID: id}
	events := []contracts.Event{event("device.registered", "device", id, 1, map[string]any{"device_id": id, "method": kind})}
	if kind == "setup" {
		events = append(events, event("auth.owner_configured", "owner", subject, int64(generation), map[string]any{"device_id": id}))
	}
	if err = s.AppendEventsTx(r.Context(), tx, principal, events); err != nil {
		writeError(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 201, contracts.DeviceRegistration{Device: device})
}

func (s *Server) handleOwnerSession(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFromContext(r.Context())
	writeJSON(w, 200, contracts.OwnerSession{DeviceID: p.DeviceID})
}
func (s *Server) handleOwnerLogout(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFromContext(r.Context())
	err := s.DeviceTransaction(r.Context(), func(tx *sql.Tx, _ contracts.Principal) error {
		_, events, e := s.revokeOwnerDeviceTx(r.Context(), tx, CommandContext{Principal: p, AcceptedAt: time.Now().UTC()}, devicePayload{DeviceID: p.DeviceID, Reason: "logout"}, nil)
		if e != nil {
			return e
		}
		return s.AppendEventsTx(r.Context(), tx, p, events)
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"logged_out": true})
}

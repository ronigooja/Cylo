package control

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ronigooja/Cylo/contracts"
)

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func codeHash(code string) []byte { h := sha256.Sum256([]byte(code)); return h[:] }

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	var req contracts.BootstrapRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.DeviceName = strings.TrimSpace(req.DeviceName)
	token, err := tokenHash(req.DeviceToken)
	if err != nil {
		writeError(w, apiError(400, "invalid_request", "device_token must be 32 random bytes encoded as base64url", nil))
		return
	}
	if req.RequestID == "" || req.DeviceName == "" || len(req.DeviceName) > 128 {
		writeError(w, apiError(400, "invalid_request", "request_id and device_name are required", nil))
		return
	}
	if s.cfg.BootstrapCode == "" {
		writeError(w, apiError(503, "bootstrap_unavailable", "bootstrap is not configured", nil))
		return
	}
	fingerprint := sha256.Sum256([]byte("bootstrap\x00" + req.RequestID + "\x00" + req.DeviceName + "\x00" + hex.EncodeToString(token) + "\x00" + hex.EncodeToString(codeHash(req.Code))))
	tx, err := s.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback()
	var existingFP []byte
	var deviceID string
	err = tx.QueryRowContext(r.Context(), `SELECT fingerprint,device_id FROM auth_requests WHERE request_id=? AND kind='bootstrap'`, req.RequestID).Scan(&existingFP, &deviceID)
	if err == nil {
		if subtle.ConstantTimeCompare(existingFP, fingerprint[:]) != 1 {
			writeError(w, apiError(409, "idempotency_conflict", "request_id was already used with different bootstrap input", nil))
			return
		}
		device, err := scanDevice(tx.QueryRowContext(r.Context(), deviceIDQuery, deviceID))
		if err != nil {
			writeError(w, err)
			return
		}
		if err = tx.Commit(); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, contracts.DeviceRegistration{Device: device})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		writeError(w, err)
		return
	}
	configuredHash := codeHash(s.cfg.BootstrapCode)
	var storedHash []byte
	var consumed string
	err = tx.QueryRowContext(r.Context(), `SELECT code_hash,coalesce(consumed_by_device_id,'') FROM bootstrap_state WHERE id=1`).Scan(&storedHash, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO bootstrap_state(id,code_hash) VALUES(1,?)`, configuredHash); err != nil {
			writeError(w, err)
			return
		}
		storedHash = configuredHash
		err = nil
	}
	if err != nil {
		writeError(w, err)
		return
	}
	provided := codeHash(req.Code)
	if len(provided) != len(storedHash) || subtle.ConstantTimeCompare(provided, storedHash) != 1 || len(configuredHash) != len(storedHash) || subtle.ConstantTimeCompare(configuredHash, storedHash) != 1 {
		writeError(w, apiError(403, "bootstrap_denied", "bootstrap code is invalid", nil))
		return
	}
	if consumed != "" {
		writeError(w, apiError(409, "bootstrap_consumed", "one-use bootstrap has already created a device", nil))
		return
	}
	deviceID, err = randomID()
	if err != nil {
		writeError(w, err)
		return
	}
	subjectID, err := randomID()
	if err != nil {
		writeError(w, err)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(r.Context(), `INSERT INTO devices(id,workspace_id,subject_id,name,status,token_hash,version,created_at,approved_at) VALUES(?,?,?,?, 'approved',?,1,?,?)`, deviceID, "default", subjectID, req.DeviceName, token, now, now)
	if err != nil {
		writeError(w, err)
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO auth_requests(request_id,kind,fingerprint,token_hash,device_id,created_at) VALUES(?,'bootstrap',?,?,?,?)`, req.RequestID, fingerprint[:], token, deviceID, now)
	if err != nil {
		writeError(w, err)
		return
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE bootstrap_state SET consumed_by_device_id=?,consumed_at=? WHERE id=1`, deviceID, now)
	if err != nil {
		writeError(w, err)
		return
	}
	device := contracts.Device{ID: deviceID, WorkspaceID: "default", Name: req.DeviceName, Status: "approved", Version: 1, CreatedAt: time.Now().UTC(), ApprovedAt: timePointer(time.Now().UTC())}
	if err = tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 201, contracts.DeviceRegistration{Device: device})
}

func (s *Server) handlePairingCreate(w http.ResponseWriter, r *http.Request) {
	var req contracts.PairingRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.DeviceName = strings.TrimSpace(req.DeviceName)
	token, err := tokenHash(req.DeviceToken)
	if err != nil {
		writeError(w, apiError(400, "invalid_request", "device_token must be 32 random bytes encoded as base64url", nil))
		return
	}
	if req.RequestID == "" || req.DeviceName == "" || len(req.DeviceName) > 128 {
		writeError(w, apiError(400, "invalid_request", "request_id and device_name are required", nil))
		return
	}
	fingerprint := sha256.Sum256([]byte("pairing\x00" + req.RequestID + "\x00" + req.DeviceName + "\x00" + hex.EncodeToString(token)))
	tx, err := s.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback()
	var oldFP []byte
	var deviceID string
	err = tx.QueryRowContext(r.Context(), `SELECT fingerprint,device_id FROM auth_requests WHERE request_id=? AND kind='pairing'`, req.RequestID).Scan(&oldFP, &deviceID)
	if err == nil {
		if subtle.ConstantTimeCompare(oldFP, fingerprint[:]) != 1 {
			writeError(w, apiError(409, "idempotency_conflict", "request_id was already used with different pairing input", nil))
			return
		}
		status, err := scanPairingStatus(tx, r.Context(), deviceID)
		if err != nil {
			writeError(w, err)
			return
		}
		if err = tx.Commit(); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, status)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		writeError(w, err)
		return
	}
	var subjectID string
	err = tx.QueryRowContext(r.Context(), `SELECT subject_id FROM devices WHERE workspace_id='default' AND status='approved' ORDER BY created_at LIMIT 1`).Scan(&subjectID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, apiError(409, "bootstrap_required", "bootstrap must approve the first device before pairing", nil))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	deviceID, err = randomID()
	if err != nil {
		writeError(w, err)
		return
	}
	now := time.Now().UTC()
	expires := now.Add(s.cfg.PairingTTL).Format(time.RFC3339Nano)
	_, err = tx.ExecContext(r.Context(), `INSERT INTO devices(id,workspace_id,subject_id,name,status,token_hash,version,created_at,pairing_expires_at) VALUES(?,'default',?,?,'pending',?,1,?,?)`, deviceID, subjectID, req.DeviceName, token, now.Format(time.RFC3339Nano), expires)
	if err != nil {
		writeError(w, err)
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO auth_requests(request_id,kind,fingerprint,token_hash,device_id,created_at) VALUES(?,'pairing',?,?,?,?)`, req.RequestID, fingerprint[:], token, deviceID, now.Format(time.RFC3339Nano))
	if err != nil {
		writeError(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 201, contracts.PairingStatus{PairingID: deviceID, Status: "pending", ExpiresAt: now.Add(s.cfg.PairingTTL)})
}

func (s *Server) handlePairingStatus(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, apiError(401, "unauthenticated", "pairing bearer required", nil))
		return
	}
	if r.PathValue("pairing_id") != p.DeviceID {
		writeError(w, apiError(404, "not_found", "pairing request not found", nil))
		return
	}
	status, err := scanPairingStatus(s.store.DB(), r.Context(), p.DeviceID)
	if err != nil {
		writeError(w, err)
		return
	}
	if status.Status == "pending" && time.Now().After(status.ExpiresAt) {
		writeError(w, apiError(410, "pairing_expired", "pairing request has expired", nil))
		return
	}
	writeJSON(w, 200, status)
}

const deviceIDQuery = `SELECT id,workspace_id,name,status,version,created_at,approved_at,last_seen_at FROM devices WHERE id=?`

type scanner interface{ Scan(...any) error }

func scanDevice(row scanner) (contracts.Device, error) {
	var d contracts.Device
	var created string
	var approved, last sql.NullString
	err := row.Scan(&d.ID, &d.WorkspaceID, &d.Name, &d.Status, &d.Version, &created, &approved, &last)
	if err != nil {
		return d, apiError(404, "not_found", "device not found", nil)
	}
	d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if approved.Valid {
		t, _ := time.Parse(time.RFC3339Nano, approved.String)
		d.ApprovedAt = &t
	}
	if last.Valid {
		t, _ := time.Parse(time.RFC3339Nano, last.String)
		d.LastSeenAt = &t
	}
	return d, nil
}
func scanPairingStatus(q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, ctx context.Context, id string) (contracts.PairingStatus, error) {
	var p contracts.PairingStatus
	var expires, approved sql.NullString
	err := q.QueryRowContext(ctx, `SELECT id,status,coalesce(pairing_expires_at,''),approved_at FROM devices WHERE id=?`, id).Scan(&p.PairingID, &p.Status, &expires, &approved)
	if err != nil {
		return p, apiError(404, "not_found", "pairing request not found", nil)
	}
	if expires.Valid && expires.String != "" {
		t, _ := time.Parse(time.RFC3339Nano, expires.String)
		p.ExpiresAt = t
	}
	if approved.Valid {
		t, _ := time.Parse(time.RFC3339Nano, approved.String)
		p.ApprovedAt = &t
	}
	if p.Status != "pending" {
		p.DeviceID = id
	}
	return p, nil
}
func timePointer(t time.Time) *time.Time { return &t }

var _ = fmt.Sprintf

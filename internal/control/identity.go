package control

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
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

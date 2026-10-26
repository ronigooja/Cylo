package acceptance

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"testing"
	"time"
)

func addMembershipDevice(t *testing.T, db *sql.DB, token, subject, deviceID string) {
	t.Helper()
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(decoded)
	at := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = db.Exec(`INSERT INTO devices(id,workspace_id,subject_id,name,status,token_hash,created_at,approved_at) VALUES(?, 'default', ?, 'peer', 'approved', ?, ?, ?)`, deviceID, subject, hash[:], at, at); err != nil {
		t.Fatal(err)
	}
}

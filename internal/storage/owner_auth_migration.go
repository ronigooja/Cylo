package storage

import "errors"

// RegisterOwnerAuthMigration is retained as a compile-time guard for callers
// from pre-single-key tooling. New deployments never register or create the
// owner/password/device approval schema.
func RegisterOwnerAuthMigration(*Store) error {
	return errors.New("owner authentication migration was removed; redeploy as a single-key instance")
}

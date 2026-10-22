package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ronigooja/Cylo/internal/storage"
)

type DeletionHead struct {
	LineageID      string   `json:"lineage_id"`
	Generation     int64    `json:"generation"`
	Sequence       int64    `json:"sequence"`
	Replayed       int      `json:"replayed"`
	GateCleared    bool     `json:"gate_cleared"`
	BlockedTargets []string `json:"blocked_targets"`
}

// TrustedDeletionHead is copied from the authenticated backup manifest into
// the private restore-state record by cylo-restore.py. The snapshot fields bind
// replay to the exact restored authority image; head fields bind it to the
// complete retained external journal.
type TrustedDeletionHead struct {
	SnapshotLineageID  string `json:"snapshot_lineage_id"`
	SnapshotGeneration int64  `json:"snapshot_generation"`
	SnapshotSequence   int64  `json:"snapshot_sequence"`
	SnapshotEventCount int64  `json:"snapshot_event_count"`
	SnapshotEventSum   int64  `json:"snapshot_event_cursor_sum"`
	JournalLineageID   string `json:"journal_lineage_id"`
	JournalGeneration  int64  `json:"journal_generation"`
	JournalSequence    int64  `json:"journal_sequence"`
	JournalHeadSHA256  string `json:"journal_head_sha256"`
}

func ReconcileDeletionJournalWithTrustedHead(ctx context.Context, store *storage.Store, trusted TrustedDeletionHead) (DeletionHead, error) {
	if store == nil || trusted.SnapshotLineageID == "" || trusted.JournalLineageID == "" || (trusted.JournalSequence > 0 && trusted.JournalHeadSHA256 == "") || trusted.SnapshotGeneration < 1 || trusted.JournalGeneration < 1 || trusted.SnapshotSequence < 0 || trusted.JournalSequence < 0 || trusted.SnapshotEventCount < 0 || trusted.SnapshotEventSum < 0 {
		return DeletionHead{}, errors.New("protected restore state has an incomplete deletion baseline")
	}
	tx, err := store.DB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return DeletionHead{}, err
	}
	var lineage string
	var generation, sequence, eventCount, eventSum int64
	if err = tx.QueryRowContext(ctx, `SELECT lineage_id,generation,deletion_sequence FROM cylo_instance_state WHERE singleton=1`).Scan(&lineage, &generation, &sequence); err != nil {
		_ = tx.Rollback()
		return DeletionHead{}, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(cursor),0) FROM events`).Scan(&eventCount, &eventSum); err != nil {
		_ = tx.Rollback()
		return DeletionHead{}, err
	}
	rollbackErr := tx.Rollback()
	if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
		return DeletionHead{}, rollbackErr
	}
	if lineage != trusted.SnapshotLineageID || generation != trusted.SnapshotGeneration || sequence != trusted.SnapshotSequence || eventCount != trusted.SnapshotEventCount || eventSum != trusted.SnapshotEventSum {
		return DeletionHead{}, errors.New("restored authority does not match the authenticated snapshot baseline")
	}
	journal, err := storage.NewDeletionJournal(store.Path())
	if err != nil {
		return DeletionHead{}, err
	}
	records, err := journal.ReadAll()
	if err != nil {
		return DeletionHead{}, err
	}
	if int64(len(records)) != trusted.JournalSequence {
		return DeletionHead{}, errors.New("retained journal does not match the authenticated external head")
	}
	if len(records) == 0 {
		if trusted.JournalLineageID != trusted.SnapshotLineageID || trusted.JournalGeneration != trusted.SnapshotGeneration || trusted.JournalHeadSHA256 != "" {
			return DeletionHead{}, errors.New("empty deletion journal does not match its trusted head")
		}
	} else {
		last := records[len(records)-1]
		if last.LineageID != trusted.JournalLineageID || last.Generation != trusted.JournalGeneration || last.Hash != trusted.JournalHeadSHA256 {
			return DeletionHead{}, errors.New("retained journal identity does not match the authenticated external head")
		}
	}
	return reconcileDeletionJournal(ctx, store, trusted.JournalLineageID, trusted.JournalGeneration, trusted.JournalSequence)
}

// replayPendingDeletionJournalTx repairs the crash window where the independent
// log was fsynced but the surrounding SQLite command transaction rolled back.
// The caller owns the authority transaction; a log ahead of the DB is replayed
// before a new deletion can allocate its next sequence.
func replayPendingDeletionJournalTx(ctx context.Context, tx *sql.Tx, journal *storage.DeletionJournal, lineage string, generation int64, sequence *int64) error {
	if journal == nil {
		return errors.New("independent deletion journal is unavailable")
	}
	records, err := journal.ReadAll()
	if err != nil {
		return fmt.Errorf("verify independent deletion journal: %w", err)
	}
	if int64(len(records)) < *sequence {
		return errors.New("authority deletion sequence is ahead of independent journal")
	}
	for _, record := range records {
		if record.LineageID != lineage || record.Generation > generation {
			return errors.New("deletion journal lineage or generation does not match authority")
		}
		if record.Sequence <= *sequence {
			continue
		}
		if record.Sequence != *sequence+1 {
			return errors.New("deletion journal has a sequence gap")
		}
		if err := replayMemoryDeletionTx(ctx, tx, record); err != nil {
			return fmt.Errorf("replay pending deletion sequence %d: %w", record.Sequence, err)
		}
		result, err := tx.ExecContext(ctx, `UPDATE cylo_instance_state SET deletion_sequence=? WHERE singleton=1 AND deletion_sequence=?`, record.Sequence, *sequence)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return errors.New("authority deletion sequence changed during journal replay")
		}
		*sequence = record.Sequence
	}
	return nil
}

// ReconcileDeletionJournal replays the independent sidecar log under the
// caller's exclusive Store lock. Trusted head arguments must come from the
// deployment's retained journal/backup inventory, never the restored DB.
func reconcileDeletionJournal(ctx context.Context, store *storage.Store, trustedLineage string, trustedGeneration, trustedHead int64) (DeletionHead, error) {
	var out DeletionHead
	if store == nil {
		return out, errors.New("store is required")
	}
	journal, err := storage.NewDeletionJournal(store.Path())
	if err != nil {
		return out, err
	}
	records, err := journal.ReadAll()
	if err != nil {
		return out, fmt.Errorf("verify independent deletion journal: %w", err)
	}
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var currentLineage string
	var currentGeneration, currentSequence int64
	var gate int
	if err = tx.QueryRowContext(ctx, `SELECT lineage_id,generation,deletion_sequence,restore_deletion_reconciliation_required FROM cylo_instance_state WHERE singleton=1`).Scan(&currentLineage, &currentGeneration, &currentSequence, &gate); err != nil {
		return out, err
	}
	if trustedLineage == "" || trustedLineage != currentLineage || trustedGeneration != currentGeneration || trustedHead != int64(len(records)) {
		return out, errors.New("trusted deletion head does not match the retained independent journal")
	}
	if currentSequence > trustedHead {
		return out, errors.New("restored deletion sequence is ahead of trusted journal head")
	}
	if err = verifyDeletionSnapshotBaseline(ctx, tx, records, currentLineage, currentGeneration, currentSequence); err != nil {
		return out, err
	}
	for _, record := range records {
		if record.LineageID != currentLineage || record.Generation > currentGeneration {
			return out, errors.New("deletion journal contains a foreign lineage or future generation")
		}
		if record.Sequence <= currentSequence {
			continue
		}
		if record.Sequence != currentSequence+1 {
			return out, errors.New("deletion journal has a sequence gap")
		}
		if record.EntityKind != "memory_claim" {
			return out, fmt.Errorf("unsupported deletion entity kind %q", record.EntityKind)
		}
		if err = replayMemoryDeletionTx(ctx, tx, record); err != nil {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE cylo_instance_state SET deletion_sequence=? WHERE singleton=1 AND deletion_sequence=?`, record.Sequence, currentSequence); err != nil {
			return out, err
		}
		currentSequence = record.Sequence
		out.Replayed++
	}
	if currentSequence != trustedHead {
		return out, errors.New("replay did not reach trusted deletion head")
	}
	if trustedHead == 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE cylo_deletion_reconciliation_checkpoints SET completed_sequence=0,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),last_error=''`); err != nil {
			return out, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cylo_deletion_reconciliation_checkpoints SET completed_sequence=?,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),last_error='' WHERE target='source_ledger'`, currentSequence); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT target,completed_sequence,last_error FROM cylo_deletion_reconciliation_checkpoints ORDER BY target`)
	if err != nil {
		return out, err
	}
	allComplete := true
	for rows.Next() {
		var target, lastError string
		var completed int64
		if err = rows.Scan(&target, &completed, &lastError); err != nil {
			_ = rows.Close()
			return out, err
		}
		if completed < trustedHead || lastError != "" {
			allComplete = false
			if lastError == "" {
				lastError = "adapter_missing"
			}
			out.BlockedTargets = append(out.BlockedTargets, target+":"+lastError)
		}
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return out, err
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	if gate == 1 && allComplete {
		if _, err = tx.ExecContext(ctx, `UPDATE cylo_instance_state SET restore_deletion_reconciliation_required=0,derived_retrieval_enabled=1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE singleton=1`); err != nil {
			return out, err
		}
		var stageGuard int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='cylo_restore_stage_guard'`).Scan(&stageGuard); err != nil {
			return out, err
		}
		if stageGuard > 0 {
			if _, err = tx.ExecContext(ctx, `UPDATE cylo_restore_stage_guard SET pending=0 WHERE singleton=1`); err != nil {
				return out, err
			}
		}
		out.GateCleared = true
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	out.LineageID, out.Generation, out.Sequence = currentLineage, currentGeneration, currentSequence
	return out, nil
}

func verifyDeletionSnapshotBaseline(ctx context.Context, tx *sql.Tx, records []storage.DeletionRecord, lineage string, generation, sequence int64) error {
	if sequence < 0 || sequence > int64(len(records)) {
		return errors.New("snapshot deletion sequence is outside the trusted journal")
	}
	var laterEvents int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM memory_deletion_events WHERE instance_deletion_sequence>?`, sequence).Scan(&laterEvents); err != nil {
		return err
	}
	if laterEvents != 0 {
		return errors.New("snapshot deletion event ledger is ahead of its authority sequence")
	}
	for index := int64(0); index < sequence; index++ {
		record := records[index]
		if record.Sequence != index+1 || record.LineageID != lineage || record.Generation > generation || record.EntityKind != "memory_claim" {
			return fmt.Errorf("snapshot deletion baseline does not match journal sequence %d", index+1)
		}
		var eventCount int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM memory_deletion_events WHERE instance_deletion_sequence=? AND instance_lineage_id=? AND instance_generation=? AND entity_kind=? AND workspace_id=? AND claim_id=? AND revision=? AND entity_target_version=?`, record.Sequence, record.LineageID, record.Generation, record.EntityKind, record.WorkspaceID, record.EntityID, record.EntityVersion, record.EntityVersion).Scan(&eventCount); err != nil {
			return err
		}
		if eventCount != 1 {
			return fmt.Errorf("snapshot deletion event does not match journal sequence %d", record.Sequence)
		}
		var status string
		var revision int64
		err := tx.QueryRowContext(ctx, `SELECT status,revision FROM memory_claims WHERE workspace_id=? AND id=?`, record.WorkspaceID, record.EntityID).Scan(&status, &revision)
		if err == nil && (status != string(ClaimTombstoned) || revision < record.EntityVersion) {
			return fmt.Errorf("snapshot claim does not preserve deletion sequence %d", record.Sequence)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return nil
}

func replayMemoryDeletionTx(ctx context.Context, tx *sql.Tx, record storage.DeletionRecord) error {
	var status string
	var revision int64
	queryErr := tx.QueryRowContext(ctx, `SELECT status,revision FROM memory_claims WHERE workspace_id=? AND id=?`, record.WorkspaceID, record.EntityID).Scan(&status, &revision)
	if queryErr != nil && !errors.Is(queryErr, sql.ErrNoRows) {
		return queryErr
	}
	if err := clearMemoryClaimLocalArtifactsTx(ctx, tx, record.WorkspaceID, record.EntityID, record.DeletedAt); err != nil {
		return err
	}
	var err error
	if errors.Is(queryErr, sql.ErrNoRows) {
		// The authority row may have been physically removed by an earlier
		// cleanup. The journal still advances and downstream deletion remains idempotent.
	} else if revision > record.EntityVersion {
		return errors.New("restored memory revision is newer than deletion journal target")
	} else if status != string(ClaimTombstoned) || revision != record.EntityVersion {
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_revision_sources WHERE workspace_id=? AND claim_id=?`, record.WorkspaceID, record.EntityID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_terms WHERE workspace_id=? AND claim_id=?`, record.WorkspaceID, record.EntityID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_revisions WHERE claim_id=?`, record.EntityID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE memory_claims SET status='tombstoned',revision=?,deleted_at=?,updated_at=? WHERE workspace_id=? AND id=?`, record.EntityVersion, record.DeletedAt, record.DeletedAt, record.WorkspaceID, record.EntityID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_revisions(claim_id,revision,content,authority_kind,status,recorded_at,actor_id) SELECT id,?,'',authority_kind,'tombstoned',?,? FROM memory_claims WHERE workspace_id=? AND id=?`, record.EntityVersion, record.DeletedAt, record.ActorID, record.WorkspaceID, record.EntityID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_tombstones(claim_id,workspace_id,revision,actor_id,reason,deleted_at) VALUES(?,?,?,?,?,?) ON CONFLICT(claim_id) DO UPDATE SET revision=excluded.revision,actor_id=excluded.actor_id,reason=excluded.reason,deleted_at=excluded.deleted_at`, record.EntityID, record.WorkspaceID, record.EntityVersion, record.ActorID, record.ReasonCode, record.DeletedAt); err != nil {
			return err
		}
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM memory_deletion_events WHERE workspace_id=? AND claim_id=? AND revision=?`, record.WorkspaceID, record.EntityID, record.EntityVersion).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		var ledgerGeneration int
		if err = tx.QueryRowContext(ctx, `SELECT ledger_generation FROM memory_deletion_ledger_state WHERE singleton=1`).Scan(&ledgerGeneration); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_deletion_events(ledger_generation,event_type,workspace_id,claim_id,revision,deleted_at,instance_lineage_id,instance_generation,entity_kind,entity_target_version,instance_deletion_sequence) VALUES(?,'claim_deleted',?,?,?,?,?,?, 'memory_claim',?,?)`, ledgerGeneration, record.WorkspaceID, record.EntityID, record.EntityVersion, record.DeletedAt, record.LineageID, record.Generation, record.EntityVersion, record.Sequence); err != nil {
			return err
		}
	}
	for _, target := range record.CleanupTargets {
		if target == "claim" || target == "source_links" || target == "proposals" || target == "source_ledger" {
			continue
		}
		payload := fmt.Sprintf(`{"claim_id":%q,"target":%q,"deletion_sequence":%d}`, record.EntityID, target, record.Sequence)
		if target == "backup_delete_manifest" {
			payload = fmt.Sprintf(`{"claim_id":%q,"target":%q,"deletion_sequence":%d,"expire_after_days":30}`, record.EntityID, target, record.Sequence)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_jobs(id,workspace_id,kind,entity_id,entity_version,target,payload,status,available_at,created_at,updated_at) VALUES(lower(hex(randomblob(16))),?, 'delete_cleanup', ?, ?, ?, ?, 'pending', ?, ?, ?) ON CONFLICT(workspace_id,kind,entity_id,entity_version,target) DO NOTHING`, record.WorkspaceID, record.EntityID, record.EntityVersion, target, payload, record.DeletedAt, record.DeletedAt, record.DeletedAt); err != nil {
			return err
		}
	}
	if err = completeLocalDeletionCheckpointsTx(ctx, tx, record.Sequence, record.DeletedAt); err != nil {
		return err
	}
	return nil
}

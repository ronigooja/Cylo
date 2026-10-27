package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ronigooja/Zoe/contracts"
	"github.com/ronigooja/Zoe/internal/control"
	"github.com/ronigooja/Zoe/internal/storage"
)

type backupDeleteManifestCleanupRequest struct {
	WorkspaceID   string `json:"workspace_id"`
	EntityID      string `json:"entity_id"`
	EntityVersion int64  `json:"entity_version"`
	Attempt       int    `json:"attempt"`
	JobID         string `json:"job_id"`
}

func (s *derivedService) handleBackupDeleteManifestCleanup(w http.ResponseWriter, r *http.Request) {
	p, ok := control.RunnerPrincipalFromContext(r.Context())
	if !ok || p.RunnerID == "" || !exactRunnerHasScope(p, derivedCompleteScope) {
		control.WriteError(w, control.NewAPIError(http.StatusForbidden, "forbidden", "runner deletion completion scope required", nil))
		return
	}
	var request backupDeleteManifestCleanupRequest
	if err := decodeJSON(r, &request); err != nil {
		control.WriteError(w, err)
		return
	}
	if request.JobID == "" || request.WorkspaceID == "" || request.EntityID == "" || request.EntityVersion < 1 || request.Attempt < 1 {
		control.WriteError(w, control.NewAPIError(http.StatusBadRequest, "invalid_request", "backup deletion cleanup job fields are required", nil))
		return
	}
	job, sequence, err := s.loadBackupManifestJob(r.Context(), p, request)
	if err != nil {
		control.WriteError(w, err)
		return
	}
	if s.cfg.BackupDeleteManifestPath == "" {
		control.WriteError(w, control.NewAPIError(http.StatusServiceUnavailable, "adapter_missing", "backup deletion manifest is not configured", nil))
		return
	}
	records, err := s.deletionRecordsThrough(sequence, job)
	if err != nil {
		control.WriteError(w, control.NewAPIError(http.StatusServiceUnavailable, "adapter_missing", "backup deletion record is unavailable", nil))
		return
	}
	trustedJournal, err := s.deletionJournal.ReadAll()
	if err != nil {
		control.WriteError(w, control.NewAPIError(http.StatusServiceUnavailable, "adapter_missing", "trusted backup deletion journal is unavailable", nil))
		return
	}
	if job.Status == "done" && job.Attempt == request.Attempt && job.LeaseOwner == p.RunnerID {
		if err = s.appendBackupManifestRecords(records, trustedJournal); err != nil {
			control.WriteError(w, control.NewAPIError(http.StatusServiceUnavailable, "adapter_missing", "backup deletion manifest could not be durably updated", nil))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "completed"})
		return
	}
	validation, err := s.validate(r.Context(), p, job.ID, request.Attempt)
	if err != nil {
		control.WriteError(w, err)
		return
	}
	if !validation.Allowed {
		control.WriteError(w, control.NewAPIError(http.StatusConflict, "deletion_cleanup_not_allowed", "deletion cleanup is not currently authorized", map[string]string{"disposition": validation.Disposition}))
		return
	}
	if err = s.appendBackupManifestRecords(records, trustedJournal); err != nil {
		control.WriteError(w, control.NewAPIError(http.StatusServiceUnavailable, "adapter_missing", "backup deletion manifest could not be durably updated", nil))
		return
	}
	result, err := s.finish(r.Context(), p, job.ID, request.Attempt, "", false, true)
	if err != nil {
		control.WriteError(w, err)
		return
	}
	if result.Status != "done" {
		control.WriteError(w, control.NewAPIError(http.StatusServiceUnavailable, "cleanup_pending", "backup deletion cleanup remains pending", nil))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "completed"})
}

func (s *derivedService) appendBackupManifestRecords(records, trustedJournal []storage.DeletionRecord) error {
	for _, record := range records {
		if err := control.AppendBackupDeleteManifest(s.cfg.BackupDeleteManifestPath, record, trustedJournal); err != nil {
			return err
		}
	}
	return nil
}

type backupManifestJob struct {
	ID, WorkspaceID, EntityID, Target string
	Status, LeaseOwner                string
	EntityVersion                     int64
	Payload                           []byte
	Attempt                           int
}

func (s *derivedService) loadBackupManifestJob(ctx context.Context, p contracts.RunnerPrincipal, request backupDeleteManifestCleanupRequest) (backupManifestJob, int64, error) {
	var job backupManifestJob
	var kind string
	var payload []byte
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,kind,entity_id,entity_version,target,payload,attempts,status,lease_owner FROM memory_jobs WHERE id=?`, request.JobID).Scan(&job.ID, &job.WorkspaceID, &kind, &job.EntityID, &job.EntityVersion, &job.Target, &payload, &job.Attempt, &job.Status, &job.LeaseOwner)
	if errors.Is(err, sql.ErrNoRows) {
		return job, 0, control.NewAPIError(http.StatusNotFound, "not_found", "memory deletion job not found", nil)
	}
	if err != nil {
		return job, 0, err
	}
	if kind != "delete_cleanup" || job.Target != "backup_delete_manifest" || !exactRunnerHasWorkspace(p, job.WorkspaceID) {
		return job, 0, control.NewAPIError(http.StatusForbidden, "forbidden", "route accepts only authorized backup manifest cleanup jobs", nil)
	}
	if job.WorkspaceID != request.WorkspaceID || job.EntityID != request.EntityID || job.EntityVersion != request.EntityVersion || job.Attempt != request.Attempt {
		return job, 0, control.NewAPIError(http.StatusConflict, "lease_lost", "cleanup request does not match the leased job", nil)
	}
	job.Payload = payload
	var body struct {
		DeletionSequence int64 `json:"deletion_sequence"`
	}
	if err = json.Unmarshal(payload, &body); err != nil || body.DeletionSequence < 1 {
		return job, 0, control.NewAPIError(http.StatusServiceUnavailable, "adapter_missing", "backup deletion job sequence is unavailable", nil)
	}
	return job, body.DeletionSequence, nil
}

func (s *derivedService) deletionRecordsThrough(sequence int64, job backupManifestJob) ([]storage.DeletionRecord, error) {
	if s.deletionJournal == nil {
		return nil, errors.New("deletion journal is unavailable")
	}
	records, err := s.deletionJournal.ReadAll()
	if err != nil {
		return nil, err
	}
	if sequence < 1 || int64(len(records)) < sequence {
		return nil, errors.New("matching deletion journal record is missing")
	}
	selected := make([]storage.DeletionRecord, 0, sequence)
	for _, record := range records[:sequence] {
		if record.WorkspaceID != job.WorkspaceID {
			continue
		}
		if !containsTarget(record.CleanupTargets, "backup_delete_manifest") {
			return nil, errors.New("backup deletion journal target history is incomplete")
		}
		selected = append(selected, record)
	}
	current := records[sequence-1]
	if current.WorkspaceID != job.WorkspaceID || current.EntityID != job.EntityID || current.EntityVersion != job.EntityVersion {
		return nil, errors.New("matching deletion journal record is missing")
	}
	return selected, nil
}

func containsTarget(targets []string, target string) bool {
	for _, value := range targets {
		if value == target {
			return true
		}
	}
	return false
}

package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/storage"
)

const (
	derivedClaimScope    = "memory:jobs:claim"
	derivedCompleteScope = "memory:jobs:complete"
	derivedMaxPayload    = 64 << 10
)

// DerivedConfig bounds leases, retries, batches, and backoff. Zero values use
// conservative defaults. Runner identity and authority are always taken from
// authenticated request context, never from the request body.
type DerivedConfig struct {
	LeaseDuration time.Duration
	MaxAttempts   int
	MaxBatch      int
	RetryBase     time.Duration
	RetryMax      time.Duration
	ExtractorID   string
}

func (c DerivedConfig) withDefaults() DerivedConfig {
	if c.LeaseDuration <= 0 || c.LeaseDuration > 15*time.Minute {
		c.LeaseDuration = 2 * time.Minute
	}
	if c.MaxAttempts < 1 || c.MaxAttempts > 20 {
		c.MaxAttempts = 8
	}
	if c.MaxBatch < 1 || c.MaxBatch > 100 {
		c.MaxBatch = 20
	}
	if c.RetryBase <= 0 {
		c.RetryBase = 2 * time.Second
	}
	if c.RetryMax < c.RetryBase || c.RetryMax > 24*time.Hour {
		c.RetryMax = 15 * time.Minute
	}
	if strings.TrimSpace(c.ExtractorID) == "" {
		c.ExtractorID = "mem0-controlled-v1"
	}
	return c
}

// RegisterDerived installs the durable memory worker API. Call it after
// Register; its routes use the same provisioned runner credential boundary.
func RegisterDerived(s *control.Server, store *storage.Store, config DerivedConfig) error {
	if s == nil || store == nil || store.DB() == nil {
		return errorsf("derived memory registration requires control server and storage")
	}
	cfg := config.withDefaults()
	svc := &derivedService{db: store.DB(), cfg: cfg}
	return s.RegisterRunnerRoutes(func(mux *http.ServeMux) {
		mux.HandleFunc("POST /runner/v1/memory/jobs/claim", svc.handleClaim)
		mux.HandleFunc("POST /runner/v1/memory/jobs/{job_id}/complete", svc.handleComplete)
		mux.HandleFunc("POST /runner/v1/memory/jobs/{job_id}/fail", svc.handleFail)
	})
}

type derivedService struct {
	db  *sql.DB
	cfg DerivedConfig
}

type derivedClaimRequest struct {
	Limit int `json:"limit"`
}

type derivedJob struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	Kind          string          `json:"kind"`
	EntityID      string          `json:"entity_id"`
	EntityVersion int64           `json:"entity_version"`
	Target        string          `json:"target"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	Attempt       int             `json:"attempt"`
	LeaseUntil    time.Time       `json:"lease_until"`
	Disposition   string          `json:"disposition,omitempty"`
	Claim         *derivedClaim   `json:"claim,omitempty"`
	Source        *derivedSource  `json:"source,omitempty"`
}

type derivedClaim struct {
	ID             string                 `json:"id"`
	WorkspaceID    string                 `json:"workspace_id"`
	Scope          Scope                  `json:"scope"`
	Subject        string                 `json:"subject"`
	Key            string                 `json:"key"`
	Content        string                 `json:"content"`
	ContentHash    string                 `json:"content_hash"`
	Authority      string                 `json:"authority_kind"`
	Status         string                 `json:"status"`
	Revision       int64                  `json:"revision"`
	SourceIDs      []string               `json:"source_ids"`
	SourceVersions []derivedSourceVersion `json:"source_versions"`
}

type derivedSourceVersion struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

type derivedSource struct {
	ID             string `json:"id"`
	Version        int64  `json:"version"`
	Scope          Scope  `json:"scope"`
	Kind           string `json:"kind"`
	Reference      string `json:"reference"`
	ContentHash    string `json:"content_hash"`
	ExtractorInput string `json:"extractor_input"`
}

type derivedLeaseRequest struct {
	Attempt int `json:"attempt"`
}

type derivedFailRequest struct {
	Attempt   int    `json:"attempt"`
	ErrorCode string `json:"error_code"`
	Permanent bool   `json:"permanent"`
}

type derivedJobResult struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Disposition string `json:"disposition,omitempty"`
}

var safeMemoryErrorCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func (s *derivedService) handleClaim(w http.ResponseWriter, r *http.Request) {
	principal, ok := control.RunnerPrincipalFromContext(r.Context())
	if !ok || principal.RunnerID == "" || !exactRunnerHasScope(principal, derivedClaimScope) {
		control.WriteError(w, control.NewAPIError(http.StatusForbidden, "forbidden", "runner memory job claim scope required", nil))
		return
	}
	var req derivedClaimRequest
	if err := decodeJSON(r, &req); err != nil {
		control.WriteError(w, err)
		return
	}
	limit := req.Limit
	if limit <= 0 || limit > s.cfg.MaxBatch {
		limit = s.cfg.MaxBatch
	}
	jobs, err := s.claim(r.Context(), principal, limit)
	if err != nil {
		control.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": jobs})
}

func (s *derivedService) claim(ctx context.Context, p contracts.RunnerPrincipal, limit int) ([]derivedJob, error) {
	workspaces := exactRunnerWorkspaces(p)
	if len(workspaces) == 0 {
		return nil, control.NewAPIError(http.StatusForbidden, "forbidden", "runner has no exact workspace allowlist", nil)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	for _, workspace := range workspaces {
		if err := s.reconcileTx(ctx, tx, workspace, now); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE memory_jobs SET status='blocked',lease_owner='',last_error_code='attempt_limit',updated_at=? WHERE workspace_id=? AND status='leased' AND lease_until<=? AND attempts>=?`, nowText, workspace, nowText, s.cfg.MaxAttempts); err != nil {
			return nil, err
		}
	}
	var jobs []derivedJob
	for _, workspace := range workspaces {
		if len(jobs) >= limit {
			break
		}
		rows, err := tx.QueryContext(ctx, `SELECT id,workspace_id,kind,entity_id,entity_version,target,payload,attempts FROM memory_jobs
WHERE workspace_id=? AND attempts<? AND ((status IN ('pending','retry') AND available_at<=?) OR (status='leased' AND lease_until<=?))
ORDER BY available_at,created_at,id LIMIT ?`, workspace, s.cfg.MaxAttempts, nowText, nowText, limit-len(jobs))
		if err != nil {
			return nil, err
		}
		var candidates []derivedJob
		for rows.Next() {
			var job derivedJob
			var payload []byte
			if err := rows.Scan(&job.ID, &job.WorkspaceID, &job.Kind, &job.EntityID, &job.EntityVersion, &job.Target, &payload, &job.Attempt); err != nil {
				_ = rows.Close()
				return nil, err
			}
			job.Payload = append(json.RawMessage(nil), payload...)
			candidates = append(candidates, job)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
		for _, job := range candidates {
			leaseUntil := now.Add(s.cfg.LeaseDuration)
			res, err := tx.ExecContext(ctx, `UPDATE memory_jobs SET status='leased',attempts=attempts+1,lease_owner=?,lease_until=?,updated_at=?
WHERE id=? AND workspace_id=? AND attempts=? AND ((status IN ('pending','retry') AND available_at<=?) OR (status='leased' AND lease_until<=?))`, p.RunnerID, leaseUntil.Format(time.RFC3339Nano), nowText, job.ID, workspace, job.Attempt, nowText, nowText)
			if err != nil {
				return nil, err
			}
			changed, err := res.RowsAffected()
			if err != nil || changed != 1 {
				if err != nil {
					return nil, err
				}
				continue
			}
			job.Attempt++
			job.LeaseUntil = leaseUntil
			current, disposition, err := s.materializeTx(ctx, tx, &job)
			if err != nil {
				return nil, err
			}
			if disposition != "" {
				state, code := "done", ""
				if disposition == "deletion_tombstone_missing" {
					state, code = "blocked", disposition
				}
				if _, err := tx.ExecContext(ctx, `UPDATE memory_jobs SET status=?,last_error_code=?,updated_at=? WHERE id=? AND lease_owner=? AND attempts=?`, state, code, nowText, job.ID, p.RunnerID, job.Attempt); err != nil {
					return nil, err
				}
				continue
			}
			job.Disposition = disposition
			job.Claim = current.Claim
			job.Source = current.Source
			jobs = append(jobs, job)
			if len(jobs) >= limit {
				break
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (s *derivedService) materializeTx(ctx context.Context, tx *sql.Tx, job *derivedJob) (*derivedJob, string, error) {
	out := &derivedJob{ID: job.ID, WorkspaceID: job.WorkspaceID, Kind: job.Kind, EntityID: job.EntityID, EntityVersion: job.EntityVersion, Target: job.Target, Attempt: job.Attempt, LeaseUntil: job.LeaseUntil, Payload: job.Payload}
	switch job.Kind {
	case "index", "extract":
		var claim derivedClaim
		var scopeKind string
		err := tx.QueryRowContext(ctx, `SELECT c.id,c.workspace_id,c.scope_kind,c.scope_id,c.principal_id,c.subject,c.claim_key,c.authority_kind,c.status,c.revision,r.content
FROM memory_claims c JOIN memory_revisions r ON r.claim_id=c.id AND r.revision=c.revision
WHERE c.workspace_id=? AND c.id=? AND c.revision=? AND c.status IN ('active','conflicted') AND c.deleted_at IS NULL`, job.WorkspaceID, job.EntityID, job.EntityVersion).Scan(&claim.ID, &claim.WorkspaceID, &scopeKind, &claim.Scope.ID, &claim.Scope.PrincipalID, &claim.Subject, &claim.Key, &claim.Authority, &claim.Status, &claim.Revision, &claim.Content)
		if errors.Is(err, sql.ErrNoRows) {
			return out, "stale_generation", nil
		}
		if err != nil {
			return nil, "", err
		}
		claim.Scope.Kind = ScopeKind(scopeKind)
		claim.ContentHash = hashString(claim.Content)
		if job.Kind == "index" {
			sources, sourceVersions, valid, err := loadCurrentSources(ctx, tx, job.WorkspaceID, claim.ID, claim.Revision)
			if err != nil {
				return nil, "", err
			}
			if !valid {
				return out, "source_unavailable", nil
			}
			claim.SourceIDs = sources
			claim.SourceVersions = sourceVersions
			out.Claim = &claim
			return out, "", nil
		}
		source, material, err := loadExtractionSource(ctx, tx, job.WorkspaceID, claim.ID, claim.Revision, job.Target)
		if errors.Is(err, ErrInvalidSource) {
			return out, "source_unavailable", nil
		}
		if err != nil {
			return nil, "", err
		}
		if source == nil {
			return out, "source_unavailable", nil
		}
		if strings.TrimSpace(material) == "" {
			return out, "empty_source", nil
		}
		out.Claim = &claim
		out.Source = &derivedSource{ID: source.ID, Version: source.Version, Scope: source.Scope, Kind: source.Kind, Reference: source.Reference, ContentHash: source.ContentHash, ExtractorInput: material}
		return out, "", nil
	case "delete_cleanup":
		if job.Target != "mem0" && job.Target != "qdrant" && job.Target != "cache" && job.Target != "exports" && job.Target != "backups" && job.Target != "source_ledger" {
			return nil, "", fmt.Errorf("unsupported memory cleanup target")
		}
		var revision int64
		err := tx.QueryRowContext(ctx, `SELECT revision FROM memory_tombstones WHERE workspace_id=? AND claim_id=?`, job.WorkspaceID, job.EntityID).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) {
			return out, "deletion_tombstone_missing", nil
		}
		if err != nil {
			return nil, "", err
		}
		if revision != job.EntityVersion {
			return out, "stale_generation", nil
		}
		var currentRevision int64
		var status string
		err = tx.QueryRowContext(ctx, `SELECT revision,status FROM memory_claims WHERE workspace_id=? AND id=?`, job.WorkspaceID, job.EntityID).Scan(&currentRevision, &status)
		if errors.Is(err, sql.ErrNoRows) || currentRevision != job.EntityVersion || status != string(ClaimTombstoned) {
			return out, "stale_generation", nil
		}
		return out, "", nil
	default:
		return nil, "", fmt.Errorf("unknown memory job kind")
	}
}

func loadCurrentSources(ctx context.Context, q querySource, workspace, claimID string, revision int64) ([]string, []derivedSourceVersion, bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT s.id,s.version,s.scope_kind,s.scope_id,s.principal_id,s.kind,s.reference,s.content_hash,s.recorded_at,s.producer
FROM memory_revision_sources rs JOIN memory_sources s ON s.workspace_id=rs.workspace_id AND s.id=rs.source_id AND s.version=rs.source_version
WHERE rs.workspace_id=? AND rs.claim_id=? AND rs.revision=? AND s.deleted_at IS NULL ORDER BY s.id,s.version`, workspace, claimID, revision)
	if err != nil {
		return nil, nil, false, err
	}
	defer rows.Close()
	var ids []string
	var versions []derivedSourceVersion
	for rows.Next() {
		var source Source
		var kind, recorded string
		if err := rows.Scan(&source.ID, &source.Version, &kind, &source.Scope.ID, &source.Scope.PrincipalID, &source.Kind, &source.Reference, &source.ContentHash, &recorded, &source.Producer); err != nil {
			return nil, nil, false, err
		}
		source.Scope.Kind = ScopeKind(kind)
		source.RecordedAt, _ = time.Parse(time.RFC3339Nano, recorded)
		valid, err := sourceStillAvailable(ctx, q, workspace, source)
		if err != nil {
			return nil, nil, false, err
		}
		if !valid {
			return nil, nil, false, nil
		}
		ids = append(ids, source.ID)
		versions = append(versions, derivedSourceVersion{ID: source.ID, Version: source.Version})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, false, err
	}
	if len(ids) == 0 {
		return nil, nil, false, nil
	}
	return ids, versions, true, nil
}

func loadExtractionSource(ctx context.Context, q querySource, workspace, claimID string, claimRevision int64, sourceID string) (*Source, string, error) {
	var source Source
	var scopeKind, recorded string
	err := q.QueryRowContext(ctx, `SELECT s.id,s.version,s.scope_kind,s.scope_id,s.principal_id,s.kind,s.reference,s.content_hash,s.recorded_at,s.producer
FROM memory_revision_sources rs JOIN memory_sources s ON s.workspace_id=rs.workspace_id AND s.id=rs.source_id AND s.version=rs.source_version
WHERE rs.workspace_id=? AND rs.claim_id=? AND rs.revision=? AND rs.source_id=? AND s.deleted_at IS NULL`, workspace, claimID, claimRevision, sourceID).Scan(&source.ID, &source.Version, &scopeKind, &source.Scope.ID, &source.Scope.PrincipalID, &source.Kind, &source.Reference, &source.ContentHash, &recorded, &source.Producer)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrInvalidSource
	}
	if err != nil {
		return nil, "", err
	}
	source.Scope.Kind = ScopeKind(scopeKind)
	source.RecordedAt, _ = time.Parse(time.RFC3339Nano, recorded)
	valid, err := sourceStillAvailable(ctx, q, workspace, source)
	if err != nil {
		return nil, "", err
	}
	if !valid {
		return nil, "", ErrInvalidSource
	}
	var material string
	switch source.Kind {
	case "task_entry":
		id := strings.TrimPrefix(source.Reference, "task_entry:")
		err = q.QueryRowContext(ctx, `SELECT content FROM task_entries WHERE id=? AND workspace_id=?`, id, workspace).Scan(&material)
	case "event":
		id := strings.TrimPrefix(source.Reference, "event:")
		var payload []byte
		err = q.QueryRowContext(ctx, `SELECT payload FROM events WHERE event_id=? AND workspace_id=?`, id, workspace).Scan(&payload)
		material = string(payload)
	case "evidence":
		id := strings.TrimPrefix(source.Reference, "evidence:")
		err = q.QueryRowContext(ctx, `SELECT summary FROM evidence WHERE id=? AND workspace_id=?`, id, workspace).Scan(&material)
	default:
		return &source, "", ErrInvalidSource
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrInvalidSource
	}
	if err != nil {
		return nil, "", err
	}
	if !utf8.ValidString(material) {
		material = strings.ToValidUTF8(material, "�")
	}
	if len(material) > 32<<10 {
		material = material[:32<<10]
		for !utf8.ValidString(material) {
			material = material[:len(material)-1]
		}
	}
	return &source, material, nil
}

func (s *derivedService) reconcileTx(ctx context.Context, tx *sql.Tx, workspace string, now time.Time) error {
	nowText := now.Format(time.RFC3339Nano)
	rows, err := tx.QueryContext(ctx, `SELECT c.id,c.revision FROM memory_claims c
WHERE c.workspace_id=? AND c.deleted_at IS NULL AND c.status IN ('active','conflicted') ORDER BY c.updated_at DESC LIMIT 500`, workspace)
	if err != nil {
		return err
	}
	type generation struct {
		id       string
		revision int64
	}
	var claims []generation
	for rows.Next() {
		var item generation
		if err := rows.Scan(&item.id, &item.revision); err != nil {
			_ = rows.Close()
			return err
		}
		claims = append(claims, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, claim := range claims {
		if err := insertDerivedJob(ctx, tx, workspace, "index", claim.id, claim.revision, "qdrant", map[string]any{"claim_id": claim.id, "revision": claim.revision}, nowText); err != nil {
			return err
		}
		refs, err := tx.QueryContext(ctx, `SELECT rs.source_id,rs.source_version,s.kind FROM memory_revision_sources rs JOIN memory_sources s ON s.workspace_id=rs.workspace_id AND s.id=rs.source_id AND s.version=rs.source_version
WHERE rs.workspace_id=? AND rs.claim_id=? AND rs.revision=? AND s.deleted_at IS NULL AND s.kind IN ('task_entry','event','evidence') ORDER BY rs.source_id LIMIT 100`, workspace, claim.id, claim.revision)
		if err != nil {
			return err
		}
		type ref struct {
			id      string
			version int64
			kind    string
		}
		var sources []ref
		for refs.Next() {
			var source ref
			if err := refs.Scan(&source.id, &source.version, &source.kind); err != nil {
				_ = refs.Close()
				return err
			}
			sources = append(sources, source)
		}
		if err := refs.Err(); err != nil {
			_ = refs.Close()
			return err
		}
		_ = refs.Close()
		for _, source := range sources {
			if err := insertDerivedJob(ctx, tx, workspace, "extract", claim.id, claim.revision, source.id, map[string]any{"claim_id": claim.id, "claim_revision": claim.revision, "source_id": source.id, "source_version": source.version, "extractor_version": s.cfg.ExtractorID}, nowText); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertDerivedJob(ctx context.Context, tx *sql.Tx, workspace, kind, entityID string, version int64, target string, payload any, now string) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO memory_jobs(id,workspace_id,kind,entity_id,entity_version,target,payload,status,available_at,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,'pending',?,?,?) ON CONFLICT(workspace_id,kind,entity_id,entity_version,target) DO NOTHING`, uuid.NewString(), workspace, kind, entityID, version, target, data, now, now, now)
	return err
}

func (s *derivedService) handleComplete(w http.ResponseWriter, r *http.Request) {
	p, ok := control.RunnerPrincipalFromContext(r.Context())
	if !ok || p.RunnerID == "" || !exactRunnerHasScope(p, derivedCompleteScope) {
		control.WriteError(w, control.NewAPIError(http.StatusForbidden, "forbidden", "runner memory job completion scope required", nil))
		return
	}
	var req derivedLeaseRequest
	if err := decodeJSON(r, &req); err != nil {
		control.WriteError(w, err)
		return
	}
	result, err := s.finish(r.Context(), p, r.PathValue("job_id"), req.Attempt, "", false, true)
	if err != nil {
		control.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *derivedService) handleFail(w http.ResponseWriter, r *http.Request) {
	p, ok := control.RunnerPrincipalFromContext(r.Context())
	if !ok || p.RunnerID == "" || !exactRunnerHasScope(p, derivedCompleteScope) {
		control.WriteError(w, control.NewAPIError(http.StatusForbidden, "forbidden", "runner memory job completion scope required", nil))
		return
	}
	var req derivedFailRequest
	if err := decodeJSON(r, &req); err != nil {
		control.WriteError(w, err)
		return
	}
	if !safeMemoryErrorCode.MatchString(req.ErrorCode) {
		control.WriteError(w, control.NewAPIError(http.StatusBadRequest, "invalid_request", "error_code must be a short safe identifier", nil))
		return
	}
	result, err := s.finish(r.Context(), p, r.PathValue("job_id"), req.Attempt, req.ErrorCode, req.Permanent, false)
	if err != nil {
		control.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *derivedService) finish(ctx context.Context, p contracts.RunnerPrincipal, id string, attempt int, code string, permanent, complete bool) (derivedJobResult, error) {
	if id == "" || attempt < 1 {
		return derivedJobResult{}, control.NewAPIError(http.StatusBadRequest, "invalid_request", "job ID and positive lease attempt are required", nil)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return derivedJobResult{}, err
	}
	defer tx.Rollback()
	var workspace, status, owner, lastCode string
	var attempts int
	var leaseUntil sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT workspace_id,status,lease_owner,lease_until,attempts,last_error_code FROM memory_jobs WHERE id=?`, id).Scan(&workspace, &status, &owner, &leaseUntil, &attempts, &lastCode)
	if errors.Is(err, sql.ErrNoRows) {
		return derivedJobResult{}, control.NewAPIError(http.StatusNotFound, "not_found", "memory job not found", nil)
	}
	if err != nil {
		return derivedJobResult{}, err
	}
	if !exactRunnerHasWorkspace(p, workspace) || !exactRunnerHasScope(p, derivedCompleteScope) {
		return derivedJobResult{}, control.NewAPIError(http.StatusForbidden, "forbidden", "runner cannot complete this workspace job", nil)
	}
	if status == "done" && attempts == attempt && owner == p.RunnerID && complete {
		return derivedJobResult{ID: id, Status: "done"}, nil
	}
	if attempts != attempt || owner != p.RunnerID || status != "leased" || !leaseUntil.Valid {
		return derivedJobResult{}, control.NewAPIError(http.StatusConflict, "lease_lost", "memory job lease is no longer current", nil)
	}
	deadline, parseErr := time.Parse(time.RFC3339Nano, leaseUntil.String)
	if parseErr != nil || !deadline.After(time.Now().UTC()) {
		return derivedJobResult{}, control.NewAPIError(http.StatusConflict, "lease_expired", "memory job lease expired", nil)
	}
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	if complete {
		_, err = tx.ExecContext(ctx, `UPDATE memory_jobs SET status='done',last_error_code='',updated_at=? WHERE id=? AND status='leased' AND lease_owner=? AND attempts=?`, nowText, id, p.RunnerID, attempt)
		if err != nil {
			return derivedJobResult{}, err
		}
		if err = tx.Commit(); err != nil {
			return derivedJobResult{}, err
		}
		return derivedJobResult{ID: id, Status: "done"}, nil
	}
	state := "retry"
	available := now.Add(retryDelay(s.cfg, attempt))
	if permanent || attempt >= s.cfg.MaxAttempts {
		state = "blocked"
		available = now
	}
	_, err = tx.ExecContext(ctx, `UPDATE memory_jobs SET status=?,available_at=?,last_error_code=?,updated_at=? WHERE id=? AND status='leased' AND lease_owner=? AND attempts=?`, state, available.Format(time.RFC3339Nano), code, nowText, id, p.RunnerID, attempt)
	if err != nil {
		return derivedJobResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return derivedJobResult{}, err
	}
	return derivedJobResult{ID: id, Status: state}, nil
}

func retryDelay(c DerivedConfig, attempt int) time.Duration {
	delay := c.RetryBase
	for n := 1; n < attempt && delay < c.RetryMax/2; n++ {
		delay *= 2
	}
	if delay > c.RetryMax {
		return c.RetryMax
	}
	return delay
}

func exactRunnerHasScope(p contracts.RunnerPrincipal, scope string) bool {
	for _, candidate := range p.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

func exactRunnerHasWorkspace(p contracts.RunnerPrincipal, workspace string) bool {
	for _, candidate := range p.WorkspaceIDs {
		if candidate == workspace {
			return true
		}
	}
	return false
}

func exactRunnerWorkspaces(p contracts.RunnerPrincipal) []string {
	var result []string
	for _, workspace := range p.WorkspaceIDs {
		if workspace != "" && workspace != "*" {
			result = append(result, workspace)
		}
	}
	sort.Strings(result)
	return result
}

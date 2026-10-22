package control

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ronigooja/Cylo/contracts"
)

const maxScopedExportBytes int64 = 64 << 20

// RegisterExportRoutes adds the permission-scoped authoritative-data export.
func RegisterExportRoutes(s *Server) error {
	if s == nil {
		return errors.New("control server is required")
	}
	return s.RegisterAuthenticatedRoutes(func(mux *http.ServeMux) {
		mux.HandleFunc("GET /v1/export", s.handleScopedExport)
	})
}

type exportCounter struct {
	Projects     int64 `json:"projects"`
	Tasks        int64 `json:"tasks"`
	Dependencies int64 `json:"task_dependencies"`
	TaskEntries  int64 `json:"task_entries"`
	Approvals    int64 `json:"approvals"`
	Runs         int64 `json:"runs"`
	Evidence     int64 `json:"evidence"`
	Memories     int64 `json:"memories"`
	Revisions    int64 `json:"memory_revisions"`
	Sources      int64 `json:"memory_sources"`
}

type exportFile struct {
	*os.File
	count int64
	hash  io.Writer
}

func (f *exportFile) Write(p []byte) (int, error) {
	if int64(len(p)) > maxScopedExportBytes-f.count {
		return 0, errExportTooLarge
	}
	n, err := f.File.Write(p)
	if n > 0 {
		f.count += int64(n)
		_, _ = f.hash.Write(p[:n])
	}
	return n, err
}

var errExportTooLarge = errors.New("export exceeds configured size limit")

func (s *Server) handleScopedExport(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, apiError(http.StatusUnauthorized, "unauthenticated", "device authentication required", nil))
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, apiError(http.StatusMethodNotAllowed, "method_not_allowed", "GET is required", nil))
		return
	}
	scope := r.URL.Query().Get("scope")
	if scope != "personal" && scope != "workspace" {
		writeError(w, apiError(http.StatusBadRequest, "invalid_scope", "scope must be personal or workspace", nil))
		return
	}
	if !HasScope(p, "export:read") || !HasScope(p, "client:read") {
		writeError(w, apiError(http.StatusForbidden, "forbidden", "export:read and client:read scopes are required", nil))
		return
	}
	if scope == "personal" && !HasScope(p, "memory:read:personal") || scope == "workspace" && !hasAnyExportMemoryGrant(p) {
		writeError(w, apiError(http.StatusForbidden, "forbidden", "the requested memory read scope is required", nil))
		return
	}

	exportID, err := randomID()
	if err != nil {
		writeExportFailure(w)
		return
	}
	f, err := os.CreateTemp("", "cylo-export-*.json")
	if err != nil {
		writeExportFailure(w)
		return
	}
	path := f.Name()
	defer os.Remove(path)
	defer f.Close()
	h := sha256.New()
	output := &exportFile{File: f, hash: h}

	tx, err := s.store.DB().BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		writeExportFailure(w)
		return
	}
	defer tx.Rollback()
	var cursor int64
	if err = tx.QueryRowContext(r.Context(), `SELECT current_cursor FROM event_counters WHERE workspace_id=?`, p.WorkspaceID).Scan(&cursor); err != nil {
		writeExportFailure(w)
		return
	}
	permissionFingerprint, err := exportAuthorizationFingerprint(r.Context(), tx, p, scope)
	if err != nil {
		writeExportFailure(w)
		return
	}
	counters := exportCounter{}
	if err = writeExportDocument(r.Context(), tx, output, p, scope, exportID, cursor, &counters); err != nil {
		if errors.Is(err, errExportTooLarge) {
			writeError(w, apiError(http.StatusRequestEntityTooLarge, "export_too_large", "authorized export exceeds the 64 MiB limit", nil))
			return
		}
		writeExportFailure(w)
		return
	}
	if err = tx.Commit(); err != nil {
		writeExportFailure(w)
		return
	}
	if err = f.Sync(); err != nil {
		writeExportFailure(w)
		return
	}

	// Reject a stale artifact if any authoritative event or device revocation
	// occurred after the export snapshot was read.
	auditTx, err := s.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		writeExportFailure(w)
		return
	}
	defer auditTx.Rollback()
	var currentCursor int64
	var deviceStatus string
	if err = auditTx.QueryRowContext(r.Context(), `SELECT current_cursor FROM event_counters WHERE workspace_id=?`, p.WorkspaceID).Scan(&currentCursor); err != nil || currentCursor != cursor {
		writeError(w, apiError(http.StatusConflict, "export_snapshot_stale", "authoritative data changed while preparing the export; retry the request", nil))
		return
	}
	if err = auditTx.QueryRowContext(r.Context(), `SELECT status FROM devices WHERE id=? AND workspace_id=? AND subject_id=?`, p.DeviceID, p.WorkspaceID, p.SubjectID).Scan(&deviceStatus); err != nil || deviceStatus != "approved" {
		writeError(w, apiError(http.StatusForbidden, "forbidden", "device authorization changed while preparing the export", nil))
		return
	}
	currentPermissionFingerprint, err := exportAuthorizationFingerprint(r.Context(), auditTx, p, scope)
	if err != nil {
		writeExportFailure(w)
		return
	}
	if !equalBytes(permissionFingerprint, currentPermissionFingerprint) {
		writeError(w, apiError(http.StatusConflict, "export_snapshot_stale", "authoritative permissions changed while preparing the export; retry the request", nil))
		return
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		writeExportFailure(w)
		return
	}
	auditPayload, marshalErr := json.Marshal(map[string]any{"schema_version": 1, "scope": scope, "bytes": output.count, "sha256": hex.EncodeToString(h.Sum(nil)), "counts": counters})
	if marshalErr != nil {
		writeExportFailure(w)
		return
	}
	if err = s.AppendEventsTx(r.Context(), auditTx, p, []contracts.Event{{
		Type: "data.export.prepared", AggregateType: "data_export", AggregateID: exportID, AggregateVersion: 1,
		Payload: auditPayload,
	}}); err != nil {
		writeExportFailure(w)
		return
	}
	if err = auditTx.Commit(); err != nil {
		writeExportFailure(w)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.cylo.export+json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="cylo-export-`+exportID+`.json"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.FormatInt(output.count, 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

func writeExportFailure(w http.ResponseWriter) {
	writeError(w, apiError(http.StatusInternalServerError, "export_failed", "export could not be prepared", nil))
}

func exportAuthorizationFingerprint(ctx context.Context, q *sql.Tx, p contracts.Principal, exportScope string) ([]byte, error) {
	h := sha256.New()
	visible := exportVisibilityPredicate()
	for _, statement := range []string{
		`SELECT p.id FROM projects p WHERE p.workspace_id=?1 AND ` + visible.project + ` ORDER BY p.id`,
		`SELECT t.id FROM tasks t WHERE t.workspace_id=?1 AND ` + visible.task + ` ORDER BY t.id`,
	} {
		rows, err := q.QueryContext(ctx, statement, p.WorkspaceID, p.SubjectID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			_, _ = io.WriteString(h, id)
			_, _ = io.WriteString(h, "\x00")
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	var claims *sql.Rows
	var err error
	if exportScope == "personal" {
		claims, err = q.QueryContext(ctx, `SELECT id,scope_kind,scope_id,principal_id,revision,status FROM memory_claims WHERE workspace_id=? AND scope_kind='personal' AND principal_id=? AND deleted_at IS NULL AND status!='tombstoned' ORDER BY id`, p.WorkspaceID, p.SubjectID)
	} else {
		claims, err = q.QueryContext(ctx, `SELECT id,scope_kind,scope_id,principal_id,revision,status FROM memory_claims WHERE workspace_id=? AND scope_kind IN ('workspace','project','task') AND deleted_at IS NULL AND status!='tombstoned' ORDER BY id`, p.WorkspaceID)
	}
	if err != nil {
		return nil, err
	}
	for claims.Next() {
		var id, kind, scopeID, principalID, status string
		var revision int64
		if err = claims.Scan(&id, &kind, &scopeID, &principalID, &revision, &status); err != nil {
			claims.Close()
			return nil, err
		}
		allowed, accessErr := exportMemoryScopeAllowed(ctx, q, p, kind, scopeID, principalID)
		if accessErr != nil {
			claims.Close()
			return nil, accessErr
		}
		if !allowed {
			continue
		}
		_, _ = io.WriteString(h, id+"\x00"+strconv.FormatInt(revision, 10)+"\x00"+status+"\x00")
		revisions, queryErr := q.QueryContext(ctx, `SELECT revision,content,authority_kind,status,recorded_at,extractor_version FROM memory_revisions WHERE claim_id=? ORDER BY revision`, id)
		if queryErr != nil {
			claims.Close()
			return nil, queryErr
		}
		for revisions.Next() {
			var rev int64
			var content, authority, revStatus, recorded, extractor string
			if queryErr = revisions.Scan(&rev, &content, &authority, &revStatus, &recorded, &extractor); queryErr != nil {
				break
			}
			_, _ = io.WriteString(h, strconv.FormatInt(rev, 10)+"\x00"+content+"\x00"+authority+"\x00"+revStatus+"\x00"+recorded+"\x00"+extractor+"\x00")
			sources, sourceErr := q.QueryContext(ctx, `SELECT s.id,s.version,s.scope_kind,s.scope_id,s.principal_id,s.reference,coalesce(s.deleted_at,'') FROM memory_revision_sources rs JOIN memory_sources s ON s.workspace_id=rs.workspace_id AND s.id=rs.source_id AND s.version=rs.source_version WHERE rs.claim_id=? AND rs.revision=? AND rs.workspace_id=? ORDER BY s.id,s.version`, id, rev, p.WorkspaceID)
			if sourceErr != nil {
				queryErr = sourceErr
				break
			}
			for sources.Next() {
				var sourceID, sourceKind, sourceScopeID, sourcePrincipalID, reference, deleted string
				var sourceVersion int64
				if sourceErr = sources.Scan(&sourceID, &sourceVersion, &sourceKind, &sourceScopeID, &sourcePrincipalID, &reference, &deleted); sourceErr != nil {
					break
				}
				sourceAllowed, permissionErr := exportMemoryScopeAllowed(ctx, q, p, sourceKind, sourceScopeID, sourcePrincipalID)
				if permissionErr != nil {
					sourceErr = permissionErr
					break
				}
				if sourceAllowed {
					_, _ = io.WriteString(h, sourceID+"\x00"+strconv.FormatInt(sourceVersion, 10)+"\x00"+reference+"\x00"+deleted+"\x00")
				}
			}
			if sourceErr == nil {
				sourceErr = sources.Err()
			}
			closeSourceErr := sources.Close()
			if sourceErr != nil {
				queryErr = sourceErr
				break
			}
			if closeSourceErr != nil {
				queryErr = closeSourceErr
				break
			}
		}
		if queryErr == nil {
			queryErr = revisions.Err()
		}
		closeRevisionErr := revisions.Close()
		if queryErr != nil {
			claims.Close()
			return nil, queryErr
		}
		if closeRevisionErr != nil {
			claims.Close()
			return nil, closeRevisionErr
		}
	}
	err = claims.Err()
	closeErr := claims.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return h.Sum(nil), nil
}

func writeExportDocument(ctx context.Context, tx *sql.Tx, out io.Writer, p contracts.Principal, scope, exportID string, cursor int64, counts *exportCounter) error {
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	manifest := map[string]any{
		"format": "cylo.authority-export", "schema_version": 1, "export_id": exportID,
		"created_at": stamp, "workspace_id": p.WorkspaceID, "subject_id": p.SubjectID, "scope": scope,
		"authority": map[string]any{"snapshot_event_cursor": strconv.FormatInt(cursor, 10), "source": "sqlite_authority"},
		"coverage":  map[string]any{"large_artifacts": "referenced_not_included", "derived_stores": "not_included", "backup_restore": "not_included"},
	}
	if _, err := io.WriteString(out, `{"manifest":`); err != nil {
		return err
	}
	if err := writeJSONValue(out, manifest); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `,"projects":`); err != nil {
		return err
	}
	visible := exportVisibilityPredicate()
	args := []any{p.WorkspaceID, p.SubjectID}
	if err := writeRows(ctx, tx, out, &counts.Projects, `SELECT json_object('id',p.id,'name',p.name,'description',p.description,'status',p.status,'version',p.version,'created_at',p.created_at,'updated_at',p.updated_at) FROM projects p WHERE p.workspace_id=?1 AND `+visible.project+` ORDER BY p.created_at,p.id`, append([]any(nil), args...)); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `,"tasks":`); err != nil {
		return err
	}
	if err := writeRows(ctx, tx, out, &counts.Tasks, `SELECT json_object('id',t.id,'project_id',coalesce(t.project_id,''),'title',t.title,'goal',t.goal,'source',t.source,'profile_id',t.profile_id,'status',t.status,'version',t.version,'created_at',t.created_at,'updated_at',t.updated_at,'accepted_at',t.accepted_at,'started_at',t.started_at,'finished_at',t.finished_at,'result',t.result,'error',t.error) FROM tasks t WHERE t.workspace_id=?1 AND `+visible.task+` ORDER BY t.created_at,t.id`, append([]any(nil), args...)); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `,"task_dependencies":`); err != nil {
		return err
	}
	if err := writeRows(ctx, tx, out, &counts.Dependencies, `SELECT json_object('task_id',d.task_id,'dependency_task_id',d.dependency_task_id,'status',d.status,'created_at',d.created_at) FROM task_dependencies d JOIN tasks t ON t.id=d.task_id AND t.workspace_id=d.workspace_id JOIN tasks dep ON dep.id=d.dependency_task_id AND dep.workspace_id=d.workspace_id WHERE d.workspace_id=?1 AND `+visible.task+` AND `+visible.taskOther+` ORDER BY d.task_id,d.dependency_task_id`, append([]any(nil), args...)); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `,"task_entries":`); err != nil {
		return err
	}
	if err := writeRows(ctx, tx, out, &counts.TaskEntries, `SELECT json_object('id',e.id,'task_id',e.task_id,'kind',e.kind,'content',e.content,'created_at',e.created_at) FROM task_entries e JOIN tasks t ON t.id=e.task_id AND t.workspace_id=e.workspace_id WHERE e.workspace_id=?1 AND `+visible.taskOnT+` ORDER BY e.created_at,e.id`, append([]any(nil), args...)); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `,"approvals":`); err != nil {
		return err
	}
	if err := writeRows(ctx, tx, out, &counts.Approvals, `SELECT json_object('id',a.id,'task_id',a.task_id,'task_version',a.task_version,'status',a.status,'prompt',a.prompt,'version',a.version,'created_at',a.created_at,'expires_at',a.expires_at) FROM approvals a JOIN tasks t ON t.id=a.task_id AND t.workspace_id=a.workspace_id WHERE a.workspace_id=?1 AND `+visible.taskOnT+` ORDER BY a.created_at,a.id`, append([]any(nil), args...)); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `,"runs":`); err != nil {
		return err
	}
	if err := writeRows(ctx, tx, out, &counts.Runs, `SELECT json_object('id',r.id,'task_id',r.task_id,'attempt_id',r.attempt_id,'status',r.status,'desired_control',r.desired_control,'control_version',r.control_version,'started_at',r.started_at,'stopped_at',r.stopped_at) FROM runs r JOIN tasks t ON t.id=r.task_id AND t.workspace_id=r.workspace_id WHERE r.workspace_id=?1 AND `+visible.taskOnT+` ORDER BY r.started_at,r.id`, append([]any(nil), args...)); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `,"evidence":`); err != nil {
		return err
	}
	if err := writeRows(ctx, tx, out, &counts.Evidence, `SELECT json_object('id',e.id,'task_id',e.task_id,'run_id',coalesce(e.run_id,''),'kind',e.kind,'summary',e.summary,'source',e.source,'created_at',e.created_at,'verified',e.verified) FROM evidence e JOIN tasks t ON t.id=e.task_id AND t.workspace_id=e.workspace_id WHERE e.workspace_id=?1 AND `+visible.taskOnT+` ORDER BY e.created_at,e.id`, append([]any(nil), args...)); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `,"memory":`); err != nil {
		return err
	}
	if err := writeMemoryRows(ctx, tx, out, p, scope, counts); err != nil {
		return err
	}
	_, err := io.WriteString(out, `}`)
	return err
}

type visibility struct{ project, task, taskOther, taskOnT string }

func exportVisibilityPredicate() visibility {
	return visibility{
		project:   `(p.owner_subject_id=?2 OR EXISTS(SELECT 1 FROM project_memberships m WHERE m.workspace_id=p.workspace_id AND m.project_id=p.id AND m.subject_id=?2 AND m.status='active'))`,
		task:      `(t.owner_subject_id=?2 OR (t.project_id IS NOT NULL AND EXISTS(SELECT 1 FROM project_memberships m WHERE m.workspace_id=t.workspace_id AND m.project_id=t.project_id AND m.subject_id=?2 AND m.status='active')))`,
		taskOther: `(dep.owner_subject_id=?2 OR (dep.project_id IS NOT NULL AND EXISTS(SELECT 1 FROM project_memberships m WHERE m.workspace_id=dep.workspace_id AND m.project_id=dep.project_id AND m.subject_id=?2 AND m.status='active')))`,
		taskOnT:   `(t.owner_subject_id=?2 OR (t.project_id IS NOT NULL AND EXISTS(SELECT 1 FROM project_memberships m WHERE m.workspace_id=t.workspace_id AND m.project_id=t.project_id AND m.subject_id=?2 AND m.status='active')))`}
}

func hasAnyExportMemoryGrant(p contracts.Principal) bool {
	for _, scope := range p.Scopes {
		if scope == "memory:read:workspace" || strings.HasPrefix(scope, "memory:read:project:") || strings.HasPrefix(scope, "memory:read:task:") {
			return true
		}
	}
	return false
}

func writeRows(ctx context.Context, tx *sql.Tx, out io.Writer, target *int64, query string, args []any) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if _, err = io.WriteString(out, `[`); err != nil {
		return err
	}
	first := true
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		if !first {
			if _, err = io.WriteString(out, `,`); err != nil {
				return err
			}
		}
		first = false
		if _, err = io.WriteString(out, raw); err != nil {
			return err
		}
		(*target)++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	_, err = io.WriteString(out, `]`)
	return err
}

func writeJSONValue(out io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	return err
}

func writeMemoryRows(ctx context.Context, tx *sql.Tx, out io.Writer, p contracts.Principal, exportScope string, counts *exportCounter) error {
	var rows *sql.Rows
	var err error
	if exportScope == "personal" {
		rows, err = tx.QueryContext(ctx, `SELECT id,scope_kind,scope_id,principal_id,subject,claim_key,authority_kind,status,revision,coalesce(valid_from,''),coalesce(valid_to,''),coalesce(supersedes_id,'') FROM memory_claims WHERE workspace_id=? AND scope_kind='personal' AND principal_id=? AND deleted_at IS NULL AND status!='tombstoned' ORDER BY created_at,id`, p.WorkspaceID, p.SubjectID)
	} else {
		rows, err = tx.QueryContext(ctx, `SELECT id,scope_kind,scope_id,principal_id,subject,claim_key,authority_kind,status,revision,coalesce(valid_from,''),coalesce(valid_to,''),coalesce(supersedes_id,'') FROM memory_claims WHERE workspace_id=? AND scope_kind IN ('workspace','project','task') AND deleted_at IS NULL AND status!='tombstoned' ORDER BY created_at,id`, p.WorkspaceID)
	}
	if err != nil {
		return err
	}
	defer rows.Close()
	if _, err = io.WriteString(out, `[`); err != nil {
		return err
	}
	first := true
	for rows.Next() {
		var id, kind, scopeID, principalID, subject, key, authority, status, validFrom, validTo, supersedes string
		var revision int64
		if err = rows.Scan(&id, &kind, &scopeID, &principalID, &subject, &key, &authority, &status, &revision, &validFrom, &validTo, &supersedes); err != nil {
			return err
		}
		allowed, err := exportMemoryScopeAllowed(ctx, tx, p, kind, scopeID, principalID)
		if err != nil {
			return err
		}
		if !allowed {
			continue
		}
		if !first {
			if _, err = io.WriteString(out, `,`); err != nil {
				return err
			}
		}
		first = false
		counts.Memories++
		if _, err = io.WriteString(out, `{"id":`); err != nil {
			return err
		}
		if err = writeJSONValue(out, id); err != nil {
			return err
		}
		claim := map[string]any{"scope": map[string]any{"kind": kind, "id": scopeID, "principal_id": principalID}, "subject": subject, "key": key, "authority_kind": authority, "status": status, "revision": revision}
		if validFrom != "" {
			claim["valid_from"] = validFrom
		}
		if validTo != "" {
			claim["valid_to"] = validTo
		}
		if supersedes != "" {
			claim["supersedes"] = supersedes
		}
		if _, err = io.WriteString(out, `,"record":`); err != nil {
			return err
		}
		if err = writeJSONValue(out, claim); err != nil {
			return err
		}
		if _, err = io.WriteString(out, `,"revisions":`); err != nil {
			return err
		}
		if err = writeMemoryRevisions(ctx, tx, out, p, id, counts); err != nil {
			return err
		}
		if _, err = io.WriteString(out, `}`); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	_, err = io.WriteString(out, `]`)
	return err
}

func exportMemoryScopeAllowed(ctx context.Context, tx *sql.Tx, p contracts.Principal, kind, id, owner string) (bool, error) {
	switch kind {
	case "personal":
		return owner == p.SubjectID && HasScope(p, "memory:read:personal"), nil
	case "workspace":
		return HasScope(p, "memory:read:workspace"), nil
	case "project":
		if !HasScope(p, "memory:read:workspace") && !HasScope(p, "memory:read:project:"+id) {
			return false, nil
		}
		return PrincipalCanAccessProject(ctx, tx, p, id)
	case "task":
		if !HasScope(p, "memory:read:workspace") && !HasScope(p, "memory:read:task:"+id) {
			return false, nil
		}
		return PrincipalCanAccessTask(ctx, tx, p, id)
	default:
		return false, nil
	}
}

func writeMemoryRevisions(ctx context.Context, tx *sql.Tx, out io.Writer, p contracts.Principal, claimID string, counts *exportCounter) error {
	rows, err := tx.QueryContext(ctx, `SELECT revision,content,authority_kind,status,recorded_at,extractor_version FROM memory_revisions WHERE claim_id=? ORDER BY revision`, claimID)
	if err != nil {
		return err
	}
	defer rows.Close()
	if _, err = io.WriteString(out, `[`); err != nil {
		return err
	}
	first := true
	for rows.Next() {
		var revision int64
		var content, authority, status, recorded, extractor string
		if err = rows.Scan(&revision, &content, &authority, &status, &recorded, &extractor); err != nil {
			return err
		}
		if !first {
			if _, err = io.WriteString(out, `,`); err != nil {
				return err
			}
		}
		first = false
		counts.Revisions++
		if _, err = io.WriteString(out, `{"revision":`); err != nil {
			return err
		}
		if err = writeJSONValue(out, revision); err != nil {
			return err
		}
		if _, err = io.WriteString(out, `,"content":`); err != nil {
			return err
		}
		if err = writeJSONValue(out, content); err != nil {
			return err
		}
		if _, err = io.WriteString(out, `,"authority_kind":`); err != nil {
			return err
		}
		if err = writeJSONValue(out, authority); err != nil {
			return err
		}
		if _, err = io.WriteString(out, `,"status":`); err != nil {
			return err
		}
		if err = writeJSONValue(out, status); err != nil {
			return err
		}
		if _, err = io.WriteString(out, `,"recorded_at":`); err != nil {
			return err
		}
		if err = writeJSONValue(out, recorded); err != nil {
			return err
		}
		if _, err = io.WriteString(out, `,"extractor_version":`); err != nil {
			return err
		}
		if err = writeJSONValue(out, extractor); err != nil {
			return err
		}
		if _, err = io.WriteString(out, `,"sources":`); err != nil {
			return err
		}
		if err = writeMemorySources(ctx, tx, out, p, claimID, revision, counts); err != nil {
			return err
		}
		if _, err = io.WriteString(out, `}`); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	_, err = io.WriteString(out, `]`)
	return err
}

func writeMemorySources(ctx context.Context, tx *sql.Tx, out io.Writer, p contracts.Principal, claimID string, revision int64, counts *exportCounter) error {
	rows, err := tx.QueryContext(ctx, `SELECT s.id,s.version,s.scope_kind,s.scope_id,s.principal_id,s.kind,s.reference,s.content_hash,s.recorded_at,s.producer
FROM memory_revision_sources rs JOIN memory_sources s ON s.workspace_id=rs.workspace_id AND s.id=rs.source_id AND s.version=rs.source_version
WHERE rs.claim_id=? AND rs.revision=? AND rs.workspace_id=? AND s.deleted_at IS NULL ORDER BY s.id,s.version`, claimID, revision, p.WorkspaceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	if _, err = io.WriteString(out, `[`); err != nil {
		return err
	}
	first := true
	for rows.Next() {
		var id, kind, scopeID, principalID, sourceKind, reference, contentHash, recorded, producer string
		var version int64
		if err = rows.Scan(&id, &version, &kind, &scopeID, &principalID, &sourceKind, &reference, &contentHash, &recorded, &producer); err != nil {
			return err
		}
		allowed, err := exportMemoryScopeAllowed(ctx, tx, p, kind, scopeID, principalID)
		if err != nil {
			return err
		}
		if !allowed {
			continue
		}
		if !first {
			if _, err = io.WriteString(out, `,`); err != nil {
				return err
			}
		}
		first = false
		counts.Sources++
		if err = writeJSONValue(out, map[string]any{"id": id, "version": version, "scope": map[string]any{"kind": kind, "id": scopeID, "principal_id": principalID}, "kind": sourceKind, "reference": reference, "content_hash": contentHash, "recorded_at": recorded, "producer": producer}); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	_, err = io.WriteString(out, `]`)
	return err
}

package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ronigooja/Cylo/contracts"
)

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, apiError(401, "unauthenticated", "device authentication required", nil))
		return
	}
	tx, err := s.store.DB().BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback()
	snapshot, err := loadSnapshot(r.Context(), tx, p.WorkspaceID, p.SubjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, snapshot)
}

func loadSnapshot(ctx context.Context, tx *sql.Tx, workspace, subject string) (contracts.Snapshot, error) {
	out := contracts.Snapshot{ServerVersion: "1", Projects: []contracts.Project{}, Tasks: []contracts.Task{}, Approvals: []contracts.Approval{}, Devices: []contracts.Device{}, Runs: []contracts.Run{}}
	var cursor int64
	if err := tx.QueryRowContext(ctx, `SELECT current_cursor FROM event_counters WHERE workspace_id=?`, workspace).Scan(&cursor); err != nil {
		return out, err
	}
	out.EventCursor = strconv.FormatInt(cursor, 10)
	projects, err := tx.QueryContext(ctx, `SELECT id,name,description,status,version,created_at,updated_at FROM projects WHERE workspace_id=? ORDER BY created_at,id`, workspace)
	if err != nil {
		return out, err
	}
	for projects.Next() {
		var p contracts.Project
		var created, updated string
		if err = projects.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.Version, &created, &updated); err != nil {
			projects.Close()
			return out, err
		}
		p.CreatedAt = parseTime(created)
		p.UpdatedAt = parseTime(updated)
		out.Projects = append(out.Projects, p)
	}
	if err = projects.Err(); err != nil {
		projects.Close()
		return out, err
	}
	projects.Close()
	tasks, err := tx.QueryContext(ctx, `SELECT id,coalesce(project_id,''),title,goal,source,profile_id,status,version,created_at,updated_at,accepted_at,started_at,finished_at,result,error FROM tasks WHERE workspace_id=? ORDER BY created_at,id`, workspace)
	if err != nil {
		return out, err
	}
	for tasks.Next() {
		var t contracts.Task
		var created, updated string
		var accepted, started, finished sql.NullString
		if err = tasks.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Goal, &t.Source, &t.ProfileID, &t.Status, &t.Version, &created, &updated, &accepted, &started, &finished, &t.Result, &t.Error); err != nil {
			tasks.Close()
			return out, err
		}
		t.CreatedAt = parseTime(created)
		t.UpdatedAt = parseTime(updated)
		if accepted.Valid {
			v := parseTime(accepted.String)
			t.AcceptedAt = &v
		}
		if started.Valid {
			v := parseTime(started.String)
			t.StartedAt = &v
		}
		if finished.Valid {
			v := parseTime(finished.String)
			t.FinishedAt = &v
		}
		t.Evidence, err = loadEvidence(ctx, tx, workspace, t.ID)
		if err != nil {
			tasks.Close()
			return out, err
		}
		out.Tasks = append(out.Tasks, t)
	}
	if err = tasks.Err(); err != nil {
		tasks.Close()
		return out, err
	}
	tasks.Close()
	approvals, err := tx.QueryContext(ctx, `SELECT id,task_id,status,prompt,version,created_at,expires_at,requested_action,requested_action_digest FROM approvals WHERE workspace_id=? AND status='pending' ORDER BY created_at,id`, workspace)
	if err != nil {
		return out, err
	}
	for approvals.Next() {
		var a contracts.Approval
		var created string
		var expires sql.NullString
		var action []byte
		if err = approvals.Scan(&a.ID, &a.TaskID, &a.Status, &a.Prompt, &a.Version, &created, &expires, &action, &a.RequestedActionDigest); err != nil {
			approvals.Close()
			return out, err
		}
		a.CreatedAt = parseTime(created)
		if expires.Valid {
			v := parseTime(expires.String)
			a.ExpiresAt = &v
		}
		if len(action) > 0 {
			a.RequestedAction = json.RawMessage(action)
		}
		out.Approvals = append(out.Approvals, a)
	}
	if err = approvals.Err(); err != nil {
		approvals.Close()
		return out, err
	}
	approvals.Close()
	devices, err := tx.QueryContext(ctx, `SELECT id,workspace_id,name,status,version,created_at,approved_at,last_seen_at FROM devices WHERE workspace_id=? AND subject_id=? ORDER BY created_at,id`, workspace, subject)
	if err != nil {
		return out, err
	}
	for devices.Next() {
		var d contracts.Device
		var created string
		var approved, last sql.NullString
		if err = devices.Scan(&d.ID, &d.WorkspaceID, &d.Name, &d.Status, &d.Version, &created, &approved, &last); err != nil {
			devices.Close()
			return out, err
		}
		d.CreatedAt = parseTime(created)
		if approved.Valid {
			v := parseTime(approved.String)
			d.ApprovedAt = &v
		}
		if last.Valid {
			v := parseTime(last.String)
			d.LastSeenAt = &v
		}
		out.Devices = append(out.Devices, d)
	}
	if err = devices.Err(); err != nil {
		devices.Close()
		return out, err
	}
	devices.Close()
	runs, err := tx.QueryContext(ctx, `SELECT id,task_id,attempt_id,status,desired_control,control_version,started_at,stopped_at FROM runs WHERE workspace_id=? ORDER BY started_at,id`, workspace)
	if err != nil {
		return out, err
	}
	for runs.Next() {
		var v contracts.Run
		var started string
		var stopped sql.NullString
		if err = runs.Scan(&v.ID, &v.TaskID, &v.AttemptID, &v.Status, &v.DesiredControl, &v.ControlVersion, &started, &stopped); err != nil {
			runs.Close()
			return out, err
		}
		v.StartedAt = parseTime(started)
		if stopped.Valid {
			t := parseTime(stopped.String)
			v.StoppedAt = &t
		}
		v.Evidence, err = loadRunEvidence(ctx, tx, workspace, v.ID)
		if err != nil {
			runs.Close()
			return out, err
		}
		out.Runs = append(out.Runs, v)
	}
	if err = runs.Err(); err != nil {
		runs.Close()
		return out, err
	}
	runs.Close()
	return out, nil
}

func loadEvidence(ctx context.Context, tx *sql.Tx, workspace, task string) ([]contracts.Evidence, error) {
	return queryEvidence(ctx, tx, `SELECT id,kind,summary,source,created_at,verified FROM evidence WHERE workspace_id=? AND task_id=? ORDER BY created_at,id`, workspace, task)
}
func loadRunEvidence(ctx context.Context, tx *sql.Tx, workspace, run string) ([]contracts.Evidence, error) {
	return queryEvidence(ctx, tx, `SELECT id,kind,summary,source,created_at,verified FROM evidence WHERE workspace_id=? AND run_id=? ORDER BY created_at,id`, workspace, run)
}
func queryEvidence(ctx context.Context, tx *sql.Tx, q string, args ...any) ([]contracts.Evidence, error) {
	out := []contracts.Evidence{}
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var e contracts.Evidence
		var created string
		var verified int
		if err = rows.Scan(&e.ID, &e.Kind, &e.Summary, &e.Source, &created, &verified); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTime(created)
		e.Verified = verified == 1
		out = append(out, e)
	}
	return out, rows.Err()
}
func parseTime(s string) time.Time { t, _ := time.Parse(time.RFC3339Nano, s); return t }

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, apiError(401, "unauthenticated", "device authentication required", nil))
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		writeError(w, apiError(500, "stream_unavailable", "streaming is unavailable", nil))
		return
	}
	cursorText := r.Header.Get("Last-Event-ID")
	cursor := int64(0)
	if cursorText != "" {
		n, err := strconv.ParseInt(cursorText, 10, 64)
		if err != nil || n < 0 {
			writeSnapshotRequired(w, "invalid_cursor")
			return
		}
		cursor = n
	}
	var high int64
	if err := s.store.DB().QueryRowContext(r.Context(), `SELECT current_cursor FROM event_counters WHERE workspace_id=?`, p.WorkspaceID).Scan(&high); err != nil {
		writeError(w, err)
		return
	}
	if cursor > high {
		writeSnapshotRequired(w, "cursor_ahead")
		return
	}
	oldest := high + 1
	var earliest sql.NullInt64
	_ = s.store.DB().QueryRowContext(r.Context(), `SELECT min(cursor) FROM events WHERE workspace_id=?`, p.WorkspaceID).Scan(&earliest)
	if earliest.Valid {
		oldest = earliest.Int64
	}
	if cursor > 0 && oldest > 1 && cursor < oldest-1 {
		writeSnapshotRequired(w, "cursor_expired")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	flusher := w.(http.Flusher)
	flusher.Flush()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	credential, _ := tokenHash(bearer(r))
	for {
		tx, err := s.store.DB().BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return
		}
		var status string
		if err = tx.QueryRowContext(r.Context(), `SELECT status FROM devices WHERE token_hash=?`, credential).Scan(&status); err != nil || status != "approved" {
			_ = tx.Rollback()
			return
		}
		rows, err := tx.QueryContext(r.Context(), `SELECT cursor,event_type,payload FROM events WHERE workspace_id=? AND cursor>? ORDER BY cursor LIMIT 100`, p.WorkspaceID, cursor)
		if err != nil {
			_ = tx.Rollback()
			return
		}
		type record struct {
			cursor  int64
			kind    string
			payload []byte
		}
		records := make([]record, 0, 100)
		for rows.Next() {
			var v record
			if err = rows.Scan(&v.cursor, &v.kind, &v.payload); err != nil {
				rows.Close()
				_ = tx.Rollback()
				return
			}
			records = append(records, v)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			_ = tx.Rollback()
			return
		}
		rows.Close()
		if tx.Commit() != nil {
			return
		}
		for _, v := range records {
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", v.cursor, v.kind, v.payload)
			cursor = v.cursor
		}
		if len(records) > 0 {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func writeSnapshotRequired(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	_, _ = fmt.Fprintf(w, "event: snapshot_required\ndata: %s\n\n", `{"reason":"`+reason+`"}`)
}

var _ context.Context
var _ = http.StatusOK

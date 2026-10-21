package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ronigooja/Cylo/contracts"
)

type projectCreatePayload struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}
type taskCreatePayload struct {
	ProjectID      string `json:"project_id,omitempty"`
	Title          string `json:"title"`
	Goal           string `json:"goal"`
	Source         string `json:"source"`
	ProfileID      string `json:"profile_id,omitempty"`
	InitialMessage string `json:"initial_message,omitempty"`
}
type taskIDPayload struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason,omitempty"`
}
type appendPayload struct {
	TaskID  string `json:"task_id"`
	Content string `json:"content"`
}
type approvePayload struct {
	TaskID     string `json:"task_id"`
	ApprovalID string `json:"approval_id"`
	Decision   string `json:"decision"`
	Comment    string `json:"comment,omitempty"`
}
type devicePayload struct {
	DeviceID string `json:"device_id"`
	Reason   string `json:"reason,omitempty"`
}

func decodePayload(raw json.RawMessage, dst any) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return apiError(400, "invalid_request", "command payload is invalid", nil)
	}
	return nil
}
func event(kind, aggType, aggID string, version int64, payload any) contracts.Event {
	raw, _ := json.Marshal(payload)
	return contracts.Event{Type: kind, AggregateType: aggType, AggregateID: aggID, AggregateVersion: version, Payload: raw}
}
func (s *Server) commandProjectCreate(ctx context.Context, tx *sql.Tx, c CommandContext, req contracts.CommandRequest) (contracts.CommandResult, []contracts.Event, error) {
	var p projectCreatePayload
	if err := decodePayload(req.Payload, &p); err != nil {
		return contracts.CommandResult{}, nil, err
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len(p.Name) > 200 || len(p.Description) > 4000 {
		return contracts.CommandResult{}, nil, apiError(400, "invalid_request", "project name is required and fields exceed limits", nil)
	}
	id, err := randomID()
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	at := c.AcceptedAt.Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO projects(id,workspace_id,name,description,status,version,created_at,updated_at) VALUES(?,?,?,?,'active',1,?,?)`, id, c.Principal.WorkspaceID, p.Name, p.Description, at, at)
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	project := contracts.Project{ID: id, Name: p.Name, Description: p.Description, Status: "active", Version: 1, CreatedAt: c.AcceptedAt, UpdatedAt: c.AcceptedAt}
	return contracts.CommandResult{ResourceType: "project", ResourceID: id, ResourceVersion: 1}, []contracts.Event{event("project.created", "project", id, 1, map[string]any{"project": project})}, nil
}

func (s *Server) commandTaskCreate(ctx context.Context, tx *sql.Tx, c CommandContext, req contracts.CommandRequest) (contracts.CommandResult, []contracts.Event, error) {
	var p taskCreatePayload
	if err := decodePayload(req.Payload, &p); err != nil {
		return contracts.CommandResult{}, nil, err
	}
	p.Title = strings.TrimSpace(p.Title)
	p.Goal = strings.TrimSpace(p.Goal)
	if p.Title == "" || p.Goal == "" || len(p.Title) > 300 || len(p.Goal) > 20000 || len(p.InitialMessage) > 20000 {
		return contracts.CommandResult{}, nil, apiError(400, "invalid_request", "task title and goal are required and must fit limits", nil)
	}
	switch p.Source {
	case "user", "voice", "service", "system":
	default:
		return contracts.CommandResult{}, nil, apiError(400, "invalid_request", "source is invalid", nil)
	}
	if p.ProjectID != "" {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE id=? AND workspace_id=? AND status='active'`, p.ProjectID, c.Principal.WorkspaceID).Scan(&exists); err != nil {
			return contracts.CommandResult{}, nil, err
		}
		if exists != 1 {
			return contracts.CommandResult{}, nil, apiError(404, "not_found", "project not found", nil)
		}
	}
	profile := p.ProfileID
	if profile == "" {
		profile = s.cfg.DefaultProfileID
	}
	if _, ok := s.cfg.Profiles[profile]; !ok {
		return contracts.CommandResult{}, nil, apiError(409, "profile_unavailable", "requested execution profile is not configured and authorized", map[string]any{"profile_id": profile})
	}
	id, err := randomID()
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	at := c.AcceptedAt.Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO tasks(id,workspace_id,project_id,title,goal,source,profile_id,status,version,created_at,updated_at,accepted_at,created_by_device_id,command_id) VALUES(?,?,?,?,?,?,?,'queued',1,?,?,?, ?,?)`, id, c.Principal.WorkspaceID, nullString(p.ProjectID), p.Title, p.Goal, p.Source, profile, at, at, at, c.Principal.DeviceID, c.CommandID)
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	task := contracts.Task{ID: id, ProjectID: p.ProjectID, Title: p.Title, Goal: p.Goal, Source: p.Source, ProfileID: profile, Status: "queued", Version: 1, CreatedAt: c.AcceptedAt, UpdatedAt: c.AcceptedAt, AcceptedAt: &c.AcceptedAt, Evidence: []contracts.Evidence{}}
	events := []contracts.Event{event("task.created", "task", id, 1, map[string]any{"task": task})}
	if p.InitialMessage != "" {
		entryID, _ := randomID()
		_, err = tx.ExecContext(ctx, `INSERT INTO task_entries(id,task_id,workspace_id,kind,content,author_device_id,created_at) VALUES(?,?,?,'user_message',?,?,?)`, entryID, id, c.Principal.WorkspaceID, p.InitialMessage, c.Principal.DeviceID, at)
		if err != nil {
			return contracts.CommandResult{}, nil, err
		}
		entry := contracts.TaskEntry{ID: entryID, Kind: "user_message", Content: p.InitialMessage, AuthorDeviceID: c.Principal.DeviceID, CreatedAt: c.AcceptedAt}
		events = append(events, event("task.entry_appended", "task", id, 1, map[string]any{"task_id": id, "entry": entry}))
	}
	return contracts.CommandResult{ResourceType: "task", ResourceID: id, ResourceVersion: 1}, events, nil
}

func (s *Server) commandTaskAppend(ctx context.Context, tx *sql.Tx, c CommandContext, req contracts.CommandRequest) (contracts.CommandResult, []contracts.Event, error) {
	var p appendPayload
	if err := decodePayload(req.Payload, &p); err != nil {
		return contracts.CommandResult{}, nil, err
	}
	p.Content = strings.TrimSpace(p.Content)
	if p.TaskID == "" || p.Content == "" || len(p.Content) > 20000 {
		return contracts.CommandResult{}, nil, apiError(400, "invalid_request", "task_id and content are required", nil)
	}
	version, status, err := taskVersionStatus(ctx, tx, c.Principal.WorkspaceID, p.TaskID)
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	if err = checkVersion(req.ExpectedVersion, version); err != nil {
		return contracts.CommandResult{}, nil, err
	}
	if status == "complete" || status == "failed" || status == "canceled" {
		return contracts.CommandResult{}, nil, apiError(409, "state_conflict", "terminal task cannot accept entries", map[string]any{"status": status})
	}
	id, err := randomID()
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	at := c.AcceptedAt.Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO task_entries(id,task_id,workspace_id,kind,content,author_device_id,created_at) VALUES(?,?,?,'user_message',?,?,?)`, id, p.TaskID, c.Principal.WorkspaceID, p.Content, c.Principal.DeviceID, at)
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	version++
	if _, err = tx.ExecContext(ctx, `UPDATE tasks SET version=?,updated_at=? WHERE id=? AND workspace_id=?`, version, at, p.TaskID, c.Principal.WorkspaceID); err != nil {
		return contracts.CommandResult{}, nil, err
	}
	entry := contracts.TaskEntry{ID: id, Kind: "user_message", Content: p.Content, AuthorDeviceID: c.Principal.DeviceID, CreatedAt: c.AcceptedAt}
	return contracts.CommandResult{ResourceType: "task", ResourceID: p.TaskID, ResourceVersion: version}, []contracts.Event{event("task.entry_appended", "task", p.TaskID, version, map[string]any{"task_id": p.TaskID, "entry": entry})}, nil
}

func (s *Server) commandTaskStop(ctx context.Context, tx *sql.Tx, c CommandContext, req contracts.CommandRequest) (contracts.CommandResult, []contracts.Event, error) {
	var p taskIDPayload
	if err := decodePayload(req.Payload, &p); err != nil {
		return contracts.CommandResult{}, nil, err
	}
	version, status, err := taskVersionStatus(ctx, tx, c.Principal.WorkspaceID, p.TaskID)
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	if err = checkVersion(req.ExpectedVersion, version); err != nil {
		return contracts.CommandResult{}, nil, err
	}
	if status == "complete" || status == "failed" || status == "canceled" {
		return contracts.CommandResult{}, nil, apiError(409, "state_conflict", "terminal task cannot be stopped", map[string]any{"status": status})
	}
	at := c.AcceptedAt.Format(time.RFC3339Nano)
	if status == "queued" {
		version++
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET status='canceled',version=?,updated_at=?,finished_at=? WHERE id=? AND workspace_id=?`, version, at, at, p.TaskID, c.Principal.WorkspaceID)
		if err != nil {
			return contracts.CommandResult{}, nil, err
		}
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE runs SET desired_control='stop',control_version=control_version+1 WHERE task_id=? AND workspace_id=? AND status IN ('dispatching','active','uncertain')`, p.TaskID, c.Principal.WorkspaceID); err != nil {
			return contracts.CommandResult{}, nil, err
		}
		version++
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET version=?,updated_at=? WHERE id=? AND workspace_id=?`, version, at, p.TaskID, c.Principal.WorkspaceID)
		if err != nil {
			return contracts.CommandResult{}, nil, err
		}
	}
	return contracts.CommandResult{ResourceType: "task", ResourceID: p.TaskID, ResourceVersion: version}, []contracts.Event{event("task.stop_requested", "task", p.TaskID, version, map[string]any{"task_id": p.TaskID, "reason": p.Reason, "status": status})}, nil
}

func (s *Server) commandTaskApprove(ctx context.Context, tx *sql.Tx, c CommandContext, req contracts.CommandRequest) (contracts.CommandResult, []contracts.Event, error) {
	var p approvePayload
	if err := decodePayload(req.Payload, &p); err != nil {
		return contracts.CommandResult{}, nil, err
	}
	if p.TaskID == "" || p.ApprovalID == "" || (p.Decision != "approve" && p.Decision != "reject") {
		return contracts.CommandResult{}, nil, apiError(400, "invalid_request", "task_id, approval_id, and decision approve|reject are required", nil)
	}
	var taskID, status string
	var version int64
	var expires sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT task_id,status,version,expires_at FROM approvals WHERE id=? AND workspace_id=?`, p.ApprovalID, c.Principal.WorkspaceID).Scan(&taskID, &status, &version, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return contracts.CommandResult{}, nil, apiError(404, "not_found", "approval not found", nil)
	}
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	if taskID != p.TaskID {
		return contracts.CommandResult{}, nil, apiError(404, "not_found", "approval is not bound to this task", nil)
	}
	if err = checkVersion(req.ExpectedVersion, version); err != nil {
		return contracts.CommandResult{}, nil, err
	}
	if status != "pending" {
		return contracts.CommandResult{}, nil, apiError(409, "state_conflict", "approval is no longer pending", map[string]any{"status": status})
	}
	if expires.Valid {
		deadline, e := time.Parse(time.RFC3339Nano, expires.String)
		if e == nil && c.AcceptedAt.After(deadline) {
			return contracts.CommandResult{}, nil, apiError(409, "state_conflict", "approval has expired", nil)
		}
	}
	next := "approved"
	if p.Decision == "reject" {
		next = "rejected"
	}
	at := c.AcceptedAt.Format(time.RFC3339Nano)
	version++
	_, err = tx.ExecContext(ctx, `UPDATE approvals SET status=?,version=?,decided_at=?,decided_by_device_id=? WHERE id=? AND status='pending'`, next, version, at, c.Principal.DeviceID, p.ApprovalID)
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	taskVersion, _, err := taskVersionStatus(ctx, tx, c.Principal.WorkspaceID, p.TaskID)
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	taskVersion++
	newTaskStatus := "awaiting_user"
	if p.Decision == "approve" {
		newTaskStatus = "queued"
	}
	_, err = tx.ExecContext(ctx, `UPDATE tasks SET status=?,version=?,updated_at=? WHERE id=? AND workspace_id=?`, newTaskStatus, taskVersion, at, p.TaskID, c.Principal.WorkspaceID)
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	return contracts.CommandResult{ResourceType: "task", ResourceID: p.TaskID, ResourceVersion: taskVersion}, []contracts.Event{event("task.approval_decided", "task", p.TaskID, taskVersion, map[string]any{"task_id": p.TaskID, "approval_id": p.ApprovalID, "approval_version": version, "decision": p.Decision, "comment": p.Comment})}, nil
}

func (s *Server) commandDeviceApprove(ctx context.Context, tx *sql.Tx, c CommandContext, req contracts.CommandRequest) (contracts.CommandResult, []contracts.Event, error) {
	var p devicePayload
	if err := decodePayload(req.Payload, &p); err != nil {
		return contracts.CommandResult{}, nil, err
	}
	if p.DeviceID == "" {
		return contracts.CommandResult{}, nil, apiError(400, "invalid_request", "device_id is required", nil)
	}
	var version int64
	var status string
	var pairingExpires sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT version,status,pairing_expires_at FROM devices WHERE id=? AND workspace_id=? AND subject_id=?`, p.DeviceID, c.Principal.WorkspaceID, c.Principal.SubjectID).Scan(&version, &status, &pairingExpires)
	if errors.Is(err, sql.ErrNoRows) {
		return contracts.CommandResult{}, nil, apiError(404, "not_found", "device not found", nil)
	}
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	if err = checkVersion(req.ExpectedVersion, version); err != nil {
		return contracts.CommandResult{}, nil, err
	}
	if status == "revoked" {
		return contracts.CommandResult{}, nil, apiError(409, "state_conflict", "revoked device cannot be approved", nil)
	}
	if status == "approved" {
		return contracts.CommandResult{}, nil, apiError(409, "state_conflict", "device is already approved", nil)
	}
	if pairingExpires.Valid {
		deadline, parseErr := time.Parse(time.RFC3339Nano, pairingExpires.String)
		if parseErr == nil && !c.AcceptedAt.Before(deadline) {
			return contracts.CommandResult{}, nil, apiError(410, "pairing_expired", "pairing request has expired", nil)
		}
	}
	at := c.AcceptedAt.Format(time.RFC3339Nano)
	version++
	_, err = tx.ExecContext(ctx, `UPDATE devices SET status='approved',version=?,approved_at=?,pairing_expires_at=NULL WHERE id=?`, version, at, p.DeviceID)
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	return contracts.CommandResult{ResourceType: "device", ResourceID: p.DeviceID, ResourceVersion: version}, []contracts.Event{event("device.approved", "device", p.DeviceID, version, map[string]any{"device_id": p.DeviceID})}, nil
}

func (s *Server) commandDeviceRevoke(ctx context.Context, tx *sql.Tx, c CommandContext, req contracts.CommandRequest) (contracts.CommandResult, []contracts.Event, error) {
	var p devicePayload
	if err := decodePayload(req.Payload, &p); err != nil {
		return contracts.CommandResult{}, nil, err
	}
	var version int64
	var status string
	err := tx.QueryRowContext(ctx, `SELECT version,status FROM devices WHERE id=? AND workspace_id=? AND subject_id=?`, p.DeviceID, c.Principal.WorkspaceID, c.Principal.SubjectID).Scan(&version, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return contracts.CommandResult{}, nil, apiError(404, "not_found", "device not found", nil)
	}
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	if err = checkVersion(req.ExpectedVersion, version); err != nil {
		return contracts.CommandResult{}, nil, err
	}
	if status == "revoked" {
		return contracts.CommandResult{}, nil, apiError(409, "state_conflict", "device is already revoked", nil)
	}
	at := c.AcceptedAt.Format(time.RFC3339Nano)
	version++
	_, err = tx.ExecContext(ctx, `UPDATE devices SET status='revoked',version=?,revoked_at=? WHERE id=?`, version, at, p.DeviceID)
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE tasks SET status=CASE WHEN status='queued' THEN 'canceled' ELSE status END,version=version+1,updated_at=?,finished_at=CASE WHEN status='queued' THEN ? ELSE finished_at END WHERE created_by_device_id=? AND workspace_id=? AND status IN ('queued','active','waiting','awaiting_user','paused')`, at, at, p.DeviceID, c.Principal.WorkspaceID)
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE runs SET desired_control='stop',control_version=control_version+1 WHERE authorized_device_id=? AND workspace_id=? AND status IN ('dispatching','active','uncertain')`, p.DeviceID, c.Principal.WorkspaceID)
	if err != nil {
		return contracts.CommandResult{}, nil, err
	}
	return contracts.CommandResult{ResourceType: "device", ResourceID: p.DeviceID, ResourceVersion: version}, []contracts.Event{event("device.revoked", "device", p.DeviceID, version, map[string]any{"device_id": p.DeviceID, "reason": p.Reason})}, nil
}

func taskVersionStatus(ctx context.Context, tx *sql.Tx, workspace, id string) (int64, string, error) {
	var v int64
	var status string
	err := tx.QueryRowContext(ctx, `SELECT version,status FROM tasks WHERE id=? AND workspace_id=?`, id, workspace).Scan(&v, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", apiError(404, "not_found", "task not found", nil)
	}
	return v, status, err
}
func checkVersion(expected *int64, current int64) error {
	if expected != nil && *expected != current {
		return apiError(409, "version_conflict", "resource version is stale", map[string]any{"expected_version": *expected, "current_version": current})
	}
	return nil
}
func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

var _ = fmt.Sprintf
var _ = http.StatusOK

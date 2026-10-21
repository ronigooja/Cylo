package contracts

import (
	"encoding/json"
	"time"
)

// Principal is populated exclusively from authenticated server state.
type Principal struct {
	WorkspaceID string   `json:"-"`
	SubjectID   string   `json:"-"`
	DeviceID    string   `json:"-"`
	Scopes      []string `json:"-"`
}

type RunnerPrincipal struct {
	RunnerID     string   `json:"-"`
	WorkspaceIDs []string `json:"-"`
	Scopes       []string `json:"-"`
}

type ErrorEnvelope struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type CommandRequest struct {
	Kind            string          `json:"kind"`
	ExpectedVersion *int64          `json:"expected_version,omitempty"`
	Payload         json.RawMessage `json:"payload"`
}

type CommandReceipt struct {
	CommandID       string    `json:"command_id"`
	IdempotencyKey  string    `json:"idempotency_key"`
	WorkspaceID     string    `json:"workspace_id"`
	Kind            string    `json:"kind"`
	Status          string    `json:"status"`
	ResourceType    string    `json:"resource_type,omitempty"`
	ResourceID      string    `json:"resource_id,omitempty"`
	ResourceVersion int64     `json:"resource_version,omitempty"`
	AcceptedAt      time.Time `json:"accepted_at"`
	EventCursor     string    `json:"event_cursor"`
}

type CommandResult struct {
	ResourceType    string
	ResourceID      string
	ResourceVersion int64
}

type Event struct {
	EventID          string          `json:"event_id"`
	Cursor           string          `json:"cursor"`
	SchemaVersion    int             `json:"schema_version"`
	Type             string          `json:"type"`
	OccurredAt       time.Time       `json:"occurred_at"`
	WorkspaceID      string          `json:"workspace_id"`
	SubjectID        string          `json:"subject_id,omitempty"`
	AggregateType    string          `json:"aggregate_type"`
	AggregateID      string          `json:"aggregate_id"`
	AggregateVersion int64           `json:"aggregate_version"`
	Payload          json.RawMessage `json:"payload"`
}

type Evidence struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Summary   string    `json:"summary"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	Verified  bool      `json:"verified"`
}

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Task struct {
	ID              string     `json:"id"`
	ProjectID       string     `json:"project_id,omitempty"`
	Title           string     `json:"title"`
	Goal            string     `json:"goal"`
	Source          string     `json:"source"`
	ProfileID       string     `json:"profile_id,omitempty"`
	Status          string     `json:"status"`
	Version         int64      `json:"version"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	AcceptedAt      *time.Time `json:"accepted_at,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	Result          string     `json:"result,omitempty"`
	Error           string     `json:"error,omitempty"`
	Evidence        []Evidence `json:"evidence"`
	CanAcceptResult bool       `json:"can_accept_result"`
}

type TaskEntry struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	Content        string    `json:"content"`
	AuthorDeviceID string    `json:"author_device_id"`
	CreatedAt      time.Time `json:"created_at"`
}

type Device struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Version     int64      `json:"version"`
	CreatedAt   time.Time  `json:"created_at"`
	ApprovedAt  *time.Time `json:"approved_at,omitempty"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
}

type Approval struct {
	ID                    string          `json:"id"`
	TaskID                string          `json:"task_id"`
	TaskVersion           int64           `json:"task_version"`
	Status                string          `json:"status"`
	Prompt                string          `json:"prompt"`
	Version               int64           `json:"version"`
	CreatedAt             time.Time       `json:"created_at"`
	ExpiresAt             *time.Time      `json:"expires_at,omitempty"`
	RequestedAction       json.RawMessage `json:"requested_action,omitempty"`
	RequestedActionDigest string          `json:"requested_action_digest,omitempty"`
}

type Snapshot struct {
	ServerVersion string     `json:"server_version"`
	EventCursor   string     `json:"event_cursor"`
	Projects      []Project  `json:"projects"`
	Tasks         []Task     `json:"tasks"`
	Approvals     []Approval `json:"approvals"`
	Devices       []Device   `json:"devices"`
	Runs          []Run      `json:"runs"`
}

type Notification struct {
	ID            string     `json:"id"`
	Kind          string     `json:"kind"`
	Title         string     `json:"title"`
	Body          string     `json:"body"`
	AggregateType string     `json:"aggregate_type"`
	AggregateID   string     `json:"aggregate_id"`
	SourceEventID string     `json:"source_event_id"`
	CreatedAt     time.Time  `json:"created_at"`
	ReadAt        *time.Time `json:"read_at,omitempty"`
}

type Run struct {
	ID                   string     `json:"id"`
	TaskID               string     `json:"task_id"`
	AttemptID            string     `json:"attempt_id"`
	Status               string     `json:"status"`
	DesiredControl       string     `json:"desired_control"`
	ControlVersion       int64      `json:"control_version"`
	StartedAt            time.Time  `json:"started_at"`
	StoppedAt            *time.Time `json:"stopped_at,omitempty"`
	TerminationConfirmed bool       `json:"termination_confirmed"`
	Evidence             []Evidence `json:"evidence"`
}

type BootstrapRequest struct {
	RequestID   string `json:"request_id"`
	Code        string `json:"code"`
	DeviceName  string `json:"device_name"`
	DeviceToken string `json:"device_token"`
}

type PairingRequest struct {
	RequestID   string `json:"request_id"`
	DeviceName  string `json:"device_name"`
	DeviceToken string `json:"device_token"`
}

type PairingStatus struct {
	PairingID  string     `json:"pairing_id"`
	Status     string     `json:"status"`
	DeviceID   string     `json:"device_id,omitempty"`
	ExpiresAt  time.Time  `json:"expires_at"`
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
}

type DeviceRegistration struct {
	Device Device `json:"device"`
}

type JobAuthorization struct {
	CapabilityIDs []string `json:"capability_ids"`
	Scopes        []string `json:"scopes"`
}

type JobClaim struct {
	RunID          string           `json:"run_id"`
	TaskID         string           `json:"task_id"`
	AttemptID      string           `json:"attempt_id"`
	WorkspaceID    string           `json:"workspace_id"`
	TaskVersion    int64            `json:"task_version"`
	Goal           string           `json:"goal"`
	ProfileID      string           `json:"profile_id"`
	RuntimeRouteID string           `json:"runtime_route_id"`
	ModelRouteID   string           `json:"model_route_id"`
	Role           string           `json:"role"`
	AOProjectID    string           `json:"ao_project_id,omitempty"`
	AOSessionID    string           `json:"ao_session_id,omitempty"`
	Authorization  JobAuthorization `json:"authorization"`
	Fence          uint64           `json:"fence"`
}

type ClaimRecovery struct {
	Claims []JobClaim `json:"claims"`
}

type RunControl struct {
	RunID          string `json:"run_id"`
	DesiredControl string `json:"desired_control"`
	ControlVersion int64  `json:"control_version"`
}

type ExternalEffect struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Reference string `json:"reference,omitempty"`
}

type RunnerObservation struct {
	ObservationID    string            `json:"observation_id"`
	RunID            string            `json:"run_id"`
	AttemptID        string            `json:"attempt_id"`
	Fence            uint64            `json:"fence"`
	Kind             string            `json:"kind"`
	OccurredAt       time.Time         `json:"occurred_at"`
	AOProjectID      string            `json:"ao_project_id,omitempty"`
	AOSessionID      string            `json:"ao_session_id,omitempty"`
	Evidence         []Evidence        `json:"evidence"`
	Result           json.RawMessage   `json:"result,omitempty"`
	Error            string            `json:"error,omitempty"`
	ExternalEffects  []ExternalEffect  `json:"external_effects"`
	TerminationProof *TerminationProof `json:"termination_proof,omitempty"`
}

// TerminationProof carries the independent AO process-stop evidence required
// before the control plane releases a run's capacity slot.
type TerminationProof struct {
	Source          string    `json:"source"`
	Method          string    `json:"method"`
	SessionID       string    `json:"session_id"`
	VerifiedAt      time.Time `json:"verified_at"`
	CleanupComplete bool      `json:"cleanup_complete"`
}

package contracts

import (
	"encoding/json"
	"time"
)

// Principal is populated exclusively from authenticated server state.
type Principal struct {
	Generation     int64    `json:"-"`
	InstallationID string   `json:"-"`
	WorkspaceID    string   `json:"-"`
	SubjectID      string   `json:"-"`
	DeviceID       string   `json:"-"`
	Scopes         []string `json:"-"`
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
	QueueReason     string     `json:"queue_reason,omitempty"`
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

// TaskMessage is a task timeline entry returned by the paginated task history endpoint.
type TaskMessage struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	Kind      string    `json:"kind"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
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
	ExecutionCapacity ExecutionCapacity `json:"execution_capacity"`
	WorkspaceID       string            `json:"workspace_id"`
	SubjectID         string            `json:"subject_id"`
	ServerVersion     string            `json:"server_version"`
	EventCursor       string            `json:"event_cursor"`
	Authority         AuthorityVersion  `json:"authority"`
	Projects          []Project         `json:"projects"`
	Tasks             []Task            `json:"tasks"`
	Approvals         []Approval        `json:"approvals"`
	Runs              []Run             `json:"runs"`
}

// ExecutionCapacity describes the global dispatch gate without exposing other tasks.
type ExecutionCapacity struct {
	ActiveRuns      int    `json:"active_runs"`
	UncertainRuns   int    `json:"uncertain_runs"`
	MaxRuns         int    `json:"max_runs"`
	AvailableSlots  int    `json:"available_slots"`
	DispatchBlocked bool   `json:"dispatch_blocked"`
	ReasonCode      string `json:"reason_code,omitempty"`
}

type AuthorityVersion struct {
	InstanceLineageID string `json:"instance_lineage_id"`
	Generation        int64  `json:"generation"`
	DeletionSequence  string `json:"deletion_sequence"`
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

type JobAuthorization struct {
	CapabilityIDs []string `json:"capability_ids"`
	Scopes        []string `json:"scopes"`
}

type JobClaim struct {
	RunID                 string            `json:"run_id"`
	TaskID                string            `json:"task_id"`
	AttemptID             string            `json:"attempt_id"`
	WorkspaceID           string            `json:"workspace_id"`
	TaskVersion           int64             `json:"task_version"`
	Goal                  string            `json:"goal"`
	ProfileID             string            `json:"profile_id"`
	Harness               string            `json:"harness"`
	RuntimeRouteID        string            `json:"runtime_route_id"`
	ModelRouteID          string            `json:"model_route_id"`
	IsolationPolicy       string            `json:"isolation_policy"`
	Role                  string            `json:"role"`
	AOProjectID           string            `json:"ao_project_id,omitempty"`
	AOSessionID           string            `json:"ao_session_id,omitempty"`
	Authorization         JobAuthorization  `json:"authorization"`
	Fence                 uint64            `json:"fence"`
	LineageRootTaskID     string            `json:"lineage_root_task_id,omitempty"`
	LineageParentTaskID   string            `json:"lineage_parent_task_id,omitempty"`
	LineageDepth          int               `json:"lineage_depth"`
	PregrantCapabilityIDs []string          `json:"pregrant_capability_ids"`
	Budget                RunBudgetSnapshot `json:"budget"`
}

// RunBudgetSnapshot is immutable for a run and is copied into its claim. Usage
// counters are enforced by the authoritative control-plane ledger.
type RunBudgetSnapshot struct {
	HardDeadline     time.Time `json:"hard_deadline"`
	MaxModelCalls    int64     `json:"max_model_calls"`
	MaxInputUnits    int64     `json:"max_input_units"`
	MaxOutputUnits   int64     `json:"max_output_units"`
	MaxToolSteps     int64     `json:"max_tool_steps"`
	MaxArtifactBytes int64     `json:"max_artifact_bytes"`
}

type ChildTaskBudget struct {
	MaxChildRuns int `json:"max_child_runs"`
	MaxDepth     int `json:"max_depth"`
}

type ChildTaskProposalRequest struct {
	ProposalID             string          `json:"proposal_id"`
	Attempt                string          `json:"attempt"`
	Fence                  uint64          `json:"fence"`
	Kind                   string          `json:"kind"`
	Title                  string          `json:"title"`
	Goal                   string          `json:"goal"`
	ProfileID              string          `json:"profile_id"`
	DependencyTaskIDs      []string        `json:"dependency_task_ids"`
	RequestedCapabilityIDs []string        `json:"requested_capability_ids"`
	Budget                 ChildTaskBudget `json:"budget"`
}

type ChildTaskProposalReceipt struct {
	ProposalID string `json:"proposal_id"`
	Status     string `json:"status"`
	Version    int64  `json:"version"`
	TaskID     string `json:"task_id,omitempty"`
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
	ObservationID       string                `json:"observation_id"`
	RunID               string                `json:"run_id"`
	AttemptID           string                `json:"attempt_id"`
	Fence               uint64                `json:"fence"`
	Kind                string                `json:"kind"`
	OccurredAt          time.Time             `json:"occurred_at"`
	AOProjectID         string                `json:"ao_project_id,omitempty"`
	AOSessionID         string                `json:"ao_session_id,omitempty"`
	Evidence            []Evidence            `json:"evidence"`
	Result              json.RawMessage       `json:"result,omitempty"`
	Error               string                `json:"error,omitempty"`
	ExternalEffects     []ExternalEffect      `json:"external_effects"`
	TerminationProof    *TerminationProof     `json:"termination_proof,omitempty"`
	NoEffectProof       *NoEffectProof        `json:"no_effect_proof,omitempty"`
	CreateRejection     *CreateRejectionProof `json:"create_rejection,omitempty"`
	BudgetReservationID string                `json:"budget_reservation_id,omitempty"`
}

// CreateRejectionProof is a sanitized AO response contract proving that AO
// rejected creation before committing a session.
type CreateRejectionProof struct {
	HTTPStatus int    `json:"http_status"`
	ErrorCode  string `json:"error_code"`
}

// NoEffectProof is the runner journal receipt for a run with no AO runtime.
// It proves either creation never began or explicit read-only reconciliation
// established absence under a newer AO process. The control plane also checks
// its own observation and effect history before accepting this receipt.
type NoEffectProof struct {
	JournalPhase    string                  `json:"journal_phase"`
	AOCreateStarted bool                    `json:"ao_create_started"`
	AOSessionID     string                  `json:"ao_session_id,omitempty"`
	AOContainerID   string                  `json:"ao_container_id,omitempty"`
	Outcome         string                  `json:"outcome"`
	Reconciliation  *NoEffectReconciliation `json:"reconciliation,omitempty"`
}

// NoEffectReconciliation records explicit operator-authorized, read-only absence checks.
type NoEffectReconciliation struct {
	RunID                     string    `json:"run_id"`
	Actor                     string    `json:"actor"`
	Confirmed                 bool      `json:"confirmed"`
	VerifiedAt                time.Time `json:"verified_at"`
	AOServerStartedAt         time.Time `json:"ao_server_started_at"`
	JournalUpdatedAt          time.Time `json:"journal_updated_at"`
	AOQuery                   string    `json:"ao_query"`
	AOInventoryComplete       bool      `json:"ao_inventory_complete"`
	AOClientRequestIDComplete bool      `json:"ao_client_request_id_complete"`
	AOMatches                 int       `json:"ao_matches"`
	DockerQuery               string    `json:"docker_query"`
	DockerInventoryComplete   bool      `json:"docker_inventory_complete"`
	DockerRuntimeCount        int       `json:"docker_runtime_count"`
	InstanceID                string    `json:"instance_id"`
	InstanceEpoch             int64     `json:"instance_epoch"`
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

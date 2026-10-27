package contracts

// RunBudgetUsage is a vector of countable per-run resources. Units are kept in
// the upstream provider's native usage units; Zoe does not infer money.
type RunBudgetUsage struct {
	ModelCalls    int64 `json:"model_calls"`
	InputUnits    int64 `json:"input_units"`
	OutputUnits   int64 `json:"output_units"`
	ToolSteps     int64 `json:"tool_steps"`
	ArtifactBytes int64 `json:"artifact_bytes"`
}

type RunBudgetReserveRequest struct {
	AttemptID     string         `json:"attempt_id"`
	Fence         uint64         `json:"fence"`
	ReservationID string         `json:"reservation_id"`
	Usage         RunBudgetUsage `json:"usage"`
}

type RunBudgetSettleRequest struct {
	AttemptID    string         `json:"attempt_id"`
	Fence        uint64         `json:"fence"`
	SettlementID string         `json:"settlement_id"`
	Outcome      string         `json:"outcome"` // settled or unknown
	Actual       RunBudgetUsage `json:"actual,omitempty"`
	EvidenceRef  string         `json:"evidence_ref,omitempty"`
}

type RunBudgetReceipt struct {
	ReservationID string         `json:"reservation_id"`
	Status        string         `json:"status"`
	Allowed       bool           `json:"allowed"`
	StopRequested bool           `json:"stop_requested"`
	Reserved      RunBudgetUsage `json:"reserved"`
	Actual        RunBudgetUsage `json:"actual,omitempty"`
	Consumed      RunBudgetUsage `json:"consumed"`
}

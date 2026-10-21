package contracts

import "time"

// CapabilityManifest is immutable once its content digest is admitted. It is
// a constrained service description, not a general container specification.
type CapabilityManifest struct {
	SchemaVersion  int                      `json:"schema_version"`
	ArtifactRef    string                   `json:"artifact_ref"`
	ArtifactDigest string                   `json:"artifact_digest"`
	Entrypoint     string                   `json:"entrypoint"`
	Args           []string                 `json:"args,omitempty"`
	SecretRefs     map[string]string        `json:"secret_refs,omitempty"`
	Mounts         []CapabilityMount        `json:"mounts,omitempty"`
	Capabilities   []string                 `json:"capabilities"`
	Triggers       []CapabilityTrigger      `json:"triggers,omitempty"`
	Budget         CapabilityResourceBudget `json:"budget"`
	Network        CapabilityNetworkPolicy  `json:"network"`
}

type CapabilityMount struct {
	VolumeID string `json:"volume_id"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}

type CapabilityTrigger struct {
	Kind       string `json:"kind"`
	Source     string `json:"source"`
	FilterJSON string `json:"filter_json,omitempty"`
}

type CapabilityResourceBudget struct {
	CPUMillis      int64 `json:"cpu_millis"`
	MemoryBytes    int64 `json:"memory_bytes"`
	DiskBytes      int64 `json:"disk_bytes"`
	TimeoutSeconds int64 `json:"timeout_seconds"`
	Concurrency    int   `json:"concurrency"`
}

type CapabilityNetworkPolicy struct {
	Mode         string   `json:"mode"`
	AllowedHosts []string `json:"allowed_hosts,omitempty"`
}

type CapabilityRelease struct {
	ReleaseID string             `json:"release_id"`
	Digest    string             `json:"digest"`
	Manifest  CapabilityManifest `json:"manifest"`
	CreatedAt time.Time          `json:"created_at"`
}

type CapabilityGrant struct {
	GrantID     string                   `json:"grant_id"`
	ScopeKind   string                   `json:"scope_kind"`
	ScopeID     string                   `json:"scope_id,omitempty"`
	PrincipalID string                   `json:"principal_id,omitempty"`
	Actions     []string                 `json:"actions"`
	Budget      CapabilityResourceBudget `json:"budget"`
	ExpiresAt   time.Time                `json:"expires_at"`
}

type DeploymentClaim struct {
	WorkspaceID    string             `json:"workspace_id"`
	DeploymentID   string             `json:"deployment_id"`
	AttemptID      string             `json:"attempt_id"`
	Fence          int64              `json:"fence"`
	ServiceID      string             `json:"service_id"`
	ReleaseDigest  string             `json:"release_digest"`
	Manifest       CapabilityManifest `json:"manifest"`
	DesiredOp      string             `json:"desired_operation"`
	Grant          CapabilityGrant    `json:"grant"`
	ConfigRef      string             `json:"config_ref,omitempty"`
	ConfigSHA256   string             `json:"config_sha256,omitempty"`
	VolumeIDs      []string           `json:"volume_ids,omitempty"`
	RuntimeBackend string             `json:"runtime_backend"`
}

type DeploymentObservation struct {
	DeploymentID      string                      `json:"deployment_id"`
	AttemptID         string                      `json:"attempt_id"`
	Fence             int64                       `json:"fence"`
	ObservationID     string                      `json:"observation_id"`
	Operation         string                      `json:"operation"`
	State             string                      `json:"state"`
	ExternalEffectRef []string                    `json:"external_effect_refs,omitempty"`
	Health            CapabilityHealth            `json:"health"`
	TerminationProof  *CapabilityTerminationProof `json:"termination_proof,omitempty"`
	ErrorCode         string                      `json:"error_code,omitempty"`
	ObservedAt        time.Time                   `json:"observed_at"`
}

type CapabilityHealth struct {
	State       string    `json:"state"`
	CheckedAt   time.Time `json:"checked_at,omitempty"`
	EvidenceRef string    `json:"evidence_ref,omitempty"`
}

type CapabilityTerminationProof struct {
	EffectRef       string    `json:"effect_ref"`
	TerminatedAt    time.Time `json:"terminated_at"`
	CleanupComplete bool      `json:"cleanup_complete"`
}

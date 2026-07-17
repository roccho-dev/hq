package worker

import (
	"encoding/json"
	"time"

	"hq/internal/core"
	"hq/internal/workersafety"
)

const (
	InstructionVersionV1 = "instruction.v1"
	ValidationVersionV1  = "validation.v1"
	ResultVersionV1      = "result.v1"
	SessionVersionV1     = "session.v1"
	PlanVersionV1        = "worker.plan.v1"
)
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusBlocked   = "blocked"
	StatusTimeout   = "timeout"
	StatusCancelled = "cancelled"
)
const (
	ResultAccepted  = "accepted"
	ResultStarted   = "started"
	ResultStdout    = "stdout"
	ResultStderr    = "stderr"
	ResultCompleted = "completed"
	ResultFailed    = "failed"
	ResultBlocked   = "blocked"
	ResultTimeout   = "timeout"
	ResultCancelled = "cancelled"
)
const (
	PlanAccepted = "accepted"
	PlanBlocked  = "blocked"
)

type SourceRef struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Raw  string `json:"raw"`
}
type Instruction struct {
	ID        string          `json:"id"`
	Version   string          `json:"version"`
	Op        string          `json:"op"`
	Target    string          `json:"target"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"created_at"`
	Reason    *string         `json:"reason,omitempty"`
	Policy    json.RawMessage `json:"policy,omitempty"`
	ReplyTo   *string         `json:"reply_to,omitempty"`
	Labels    []string        `json:"labels,omitempty"`
}
type Diagnostic struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}
type ReadRow struct {
	Source        SourceRef
	Instruction   Instruction
	Provenance    *core.CompileProvenance
	UnknownFields []string
	ParseError    *Diagnostic
}
type PayloadSummary struct {
	Bytes int      `json:"bytes"`
	Keys  []string `json:"keys,omitempty"`
}
type PolicyDecision struct {
	Allowed         bool     `json:"allowed"`
	Code            string   `json:"code"`
	Message         string   `json:"message"`
	EffectiveCWD    string   `json:"effective_cwd,omitempty"`
	TimeoutSeconds  int64    `json:"timeout_seconds,omitempty"`
	EnvironmentKeys []string `json:"environment_keys,omitempty"`
}
type PlanRow struct {
	Version           string         `json:"version"`
	Source            SourceRef      `json:"source"`
	InstructionID     string         `json:"instruction_id,omitempty"`
	InstructionDigest string         `json:"instruction_digest,omitempty"`
	Target            string         `json:"target,omitempty"`
	Op                string         `json:"op,omitempty"`
	ExpectedAdapter   string         `json:"expected_adapter,omitempty"`
	Payload           PayloadSummary `json:"payload"`
	Decision          string         `json:"decision"`
	Validation        []Diagnostic   `json:"validation_errors,omitempty"`
	Policy            PolicyDecision `json:"policy"`
}
type ValidationRow struct {
	Version       string      `json:"version"`
	SourceLine    int         `json:"source_line"`
	Status        string      `json:"status"`
	Error         ResultError `json:"error"`
	InstructionID string      `json:"instruction_id,omitempty"`
	Target        string      `json:"target,omitempty"`
}
type ResultError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable *bool  `json:"retryable,omitempty"`
}
type FinalResult struct {
	Text string `json:"text,omitempty"`
	Path string `json:"path,omitempty"`
}
type ProviderEvidence struct {
	CapabilityID        string                       `json:"capability_id"`
	ProviderID          string                       `json:"provider_id"`
	ContractVersion     string                       `json:"contract_version"`
	DeploymentID        string                       `json:"deployment_id"`
	ProviderKind        string                       `json:"provider_kind"`
	IntegrityDigest     string                       `json:"integrity_digest"`
	ConfigurationDigest string                       `json:"configuration_digest,omitempty"`
	Dependencies        []ProviderDependencyEvidence `json:"dependencies,omitempty"`
	IdempotencyContract string                       `json:"idempotency_contract,omitempty"`
	IdempotencyKey      string                       `json:"idempotency_key,omitempty"`
}
type ProviderDependencyEvidence struct {
	Name                string `json:"name"`
	ProviderID          string `json:"provider_id"`
	ContractVersion     string `json:"contract_version"`
	DeploymentID        string `json:"deployment_id"`
	ProviderKind        string `json:"provider_kind"`
	IntegrityDigest     string `json:"integrity_digest"`
	ConfigurationDigest string `json:"configuration_digest,omitempty"`
}
type ResultRow struct {
	EventID         string            `json:"event_id"`
	Version         string            `json:"version"`
	RunID           string            `json:"run_id"`
	InstructionID   string            `json:"instruction_id"`
	Target          string            `json:"target"`
	Kind            string            `json:"kind"`
	Seq             int               `json:"seq"`
	RecordedAt      time.Time         `json:"recorded_at"`
	Message         *string           `json:"message,omitempty"`
	Final           *FinalResult      `json:"final,omitempty"`
	Error           *ResultError      `json:"error,omitempty"`
	NativeSessionID *string           `json:"native_session_id,omitempty"`
	Provider        *ProviderEvidence `json:"provider,omitempty"`
}
type LogEntry struct {
	Validation *ValidationRow
	Result     *ResultRow
	Policy     *workersafety.PolicyDecision
}

func ValidationEntry(r ValidationRow) LogEntry           { return LogEntry{Validation: &r} }
func ResultEntry(r ResultRow) LogEntry                   { return LogEntry{Result: &r} }
func PolicyEntry(r workersafety.PolicyDecision) LogEntry { return LogEntry{Policy: &r} }

type LogData struct {
	Validations []ValidationRow
	Results     []ResultRow
	Policies    []workersafety.PolicyDecision
}
type RunProjection struct {
	Version         string    `json:"version"`
	RunID           string    `json:"run_id"`
	InstructionID   string    `json:"instruction_id"`
	Target          string    `json:"target"`
	Status          string    `json:"status"`
	CWD             string    `json:"cwd"`
	StartedAt       time.Time `json:"started_at"`
	LastEventAt     time.Time `json:"last_event_at"`
	FinalPath       string    `json:"final_path,omitempty"`
	NativeSessionID string    `json:"native_session_id,omitempty"`
	Error           string    `json:"error,omitempty"`
	LastKind        string    `json:"last_kind"`
}

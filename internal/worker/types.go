// Package worker implements the durable JSONL worker core. It deliberately
// contains no concrete target adapter and imports no hq compiler package.
package worker

import (
	"encoding/json"
	"time"
)

const (
	InstructionVersionV1 = "instruction.v1"
	ResultVersionV1      = "result.v1"
	SessionVersionV1     = "session.v1"
	PlanVersionV1        = "worker.plan.v1"
	DecisionVersionV1    = "worker.decision.v1"
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
	DecisionAccepted  = "accepted"
	DecisionBlocked   = "blocked"
	DecisionDuplicate = "duplicate"
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
	Version         string         `json:"version"`
	Source          SourceRef      `json:"source"`
	InstructionID   string         `json:"instruction_id,omitempty"`
	Target          string         `json:"target,omitempty"`
	Op              string         `json:"op,omitempty"`
	ExpectedAdapter string         `json:"expected_adapter,omitempty"`
	Payload         PayloadSummary `json:"payload"`
	Decision        string         `json:"decision"`
	Validation      []Diagnostic   `json:"validation_errors,omitempty"`
	Policy          PolicyDecision `json:"policy"`
}

// DecisionRow records worker-owned pre-dispatch evidence. It is intentionally
// separate from result.v1: malformed rows and duplicate reads have no valid
// adapter run identity and must not be represented by fake result rows.
type DecisionRow struct {
	DecisionID      string          `json:"decision_id"`
	Version         string          `json:"version"`
	Decision        string          `json:"decision"`
	RecordedAt      time.Time       `json:"recorded_at"`
	Source          SourceRef       `json:"source"`
	InstructionID   string          `json:"instruction_id,omitempty"`
	RunID           string          `json:"run_id,omitempty"`
	Target          string          `json:"target,omitempty"`
	ExpectedAdapter string          `json:"expected_adapter,omitempty"`
	Code            string          `json:"code"`
	Message         string          `json:"message"`
	Diagnostics     []Diagnostic    `json:"diagnostics,omitempty"`
	Policy          *PolicyDecision `json:"policy,omitempty"`
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

type ResultRow struct {
	EventID         string       `json:"event_id"`
	Version         string       `json:"version"`
	RunID           string       `json:"run_id"`
	InstructionID   string       `json:"instruction_id"`
	Target          string       `json:"target"`
	Kind            string       `json:"kind"`
	Seq             int          `json:"seq"`
	RecordedAt      time.Time    `json:"recorded_at"`
	Message         *string      `json:"message,omitempty"`
	Final           *FinalResult `json:"final,omitempty"`
	Error           *ResultError `json:"error,omitempty"`
	NativeSessionID *string      `json:"native_session_id,omitempty"`
}

type LogEntry struct {
	Decision *DecisionRow
	Result   *ResultRow
}

func DecisionEntry(row DecisionRow) LogEntry { return LogEntry{Decision: &row} }
func ResultEntry(row ResultRow) LogEntry     { return LogEntry{Result: &row} }

type LogData struct {
	Decisions []DecisionRow
	Results   []ResultRow
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

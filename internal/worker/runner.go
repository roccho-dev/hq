package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hq/internal/worker/adapter"
	"hq/internal/worker/directexec"
	"hq/internal/workersafety"
)

type EntryAppender interface {
	Append(LogEntry) error
}

type Runner struct {
	Engine    Engine
	Registry  *adapter.Registry
	Approvals ApprovalStore
	Now       func() time.Time
}

func NewRunner(workspaceRoot string, registry *adapter.Registry, approvals ApprovalStore) Runner {
	if registry == nil || len(registry.Snapshot().Targets) == 0 {
		registry = defaultRuntimeRegistry()
	}
	if approvals.byInstruction == nil {
		approvals = EmptyApprovalStore()
	}
	return Runner{
		Engine: NewEngine(workspaceRoot), Registry: registry, Approvals: approvals, Now: time.Now,
	}
}

func defaultRuntimeRegistry() *adapter.Registry {
	registry, err := adapter.NewRegistry(directexec.Registration())
	if err != nil {
		panic("construct direct executable registry: " + err.Error())
	}
	return registry
}

// Process is the single executable worker sequence. Validation and both policy
// layers complete before registry lookup; every lifecycle row is redacted and
// durably appended before the next side effect begins.
func (r Runner) Process(ctx context.Context, rows []ReadRow, prior LogData, replay bool, sink EntryAppender) ([]LogEntry, int, error) {
	if sink == nil {
		return nil, 0, errors.New("event sink is required")
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.Engine.Now == nil {
		r.Engine.Now = r.Now
	}
	if r.Engine.Contract.Version == "" {
		r.Engine.Contract = DefaultContract()
	}
	if r.Registry == nil || len(r.Registry.Snapshot().Targets) == 0 {
		r.Registry = defaultRuntimeRegistry()
	}
	if r.Approvals.byInstruction == nil {
		r.Approvals = EmptyApprovalStore()
	}

	validation := r.Engine.Contract.ValidateRows(rows)
	priorRuns := latestRuns(prior.Results)
	var emitted []LogEntry
	unsuccessful := 0

	appendEntry := func(entry LogEntry) error {
		redacted, err := RedactLogEntry(entry)
		if err != nil {
			return err
		}
		if err := sink.Append(redacted); err != nil {
			return err
		}
		emitted = append(emitted, redacted)
		return nil
	}

	for index, row := range rows {
		if len(validation[index]) != 0 {
			if err := appendEntry(ValidationEntry(makeValidation(row, validation[index][0]))); err != nil {
				return emitted, unsuccessful, err
			}
			unsuccessful++
			continue
		}

		priorRun, exists := priorRuns[row.Instruction.ID]
		if exists && !replay && priorRun.Status != StatusQueued {
			entry := ValidationEntry(makeValidation(row, Diagnostic{
				Code: "duplicate_id", Field: "id",
				Message: "instruction already has running or terminal durable evidence; use --replay to create a new run",
			}))
			if err := appendEntry(entry); err != nil {
				return emitted, unsuccessful, err
			}
			unsuccessful++
			continue
		}

		clock := newMonotonicClock(r.Now)
		runID := ""
		seq := 0
		newRun := true
		if exists && !replay && priorRun.Status == StatusQueued {
			runID = priorRun.RunID
			seq = priorRun.NextSeq
			newRun = false
		} else {
			runID = makeRunID(row, clock())
		}

		digest, err := InstructionDigest(row.Instruction)
		if err != nil {
			return emitted, unsuccessful, fmt.Errorf("digest instruction %q: %w", row.Instruction.ID, err)
		}
		baseDecision := r.Engine.Policy.Evaluate(row.Instruction)
		finalDecision := workersafety.EvaluatePolicy(workersafety.PolicyRequest{
			InstructionID:     row.Instruction.ID,
			RunID:             runID,
			Target:            row.Instruction.Target,
			Operation:         row.Instruction.Op,
			InstructionDigest: digest,
			Risk:              workersafety.RiskDangerous,
			Policy:            workersafety.ExecutionPolicy{RequireApproval: true},
			Approval:          r.Approvals.ApprovalFor(row.Instruction.ID),
		})
		policyMessage := "dispatch denied by exact digest-bound approval policy"
		if !baseDecision.Allowed {
			finalDecision.Status = workersafety.PolicyBlocked
			finalDecision.Reason = baseDecision.Code
			finalDecision.MayDispatch = false
			finalDecision.ApprovedBy = ""
			policyMessage = baseDecision.Message
		}
		if err := appendEntry(PolicyEntry(finalDecision)); err != nil {
			return emitted, unsuccessful, err
		}

		if newRun {
			accepted := ResultRow{
				EventID: makeEventID(runID, seq), Version: ResultVersionV1, RunID: runID,
				InstructionID: row.Instruction.ID, Target: row.Instruction.Target,
				Kind: ResultAccepted, Seq: seq, RecordedAt: clock(),
			}
			if err := appendEntry(ResultEntry(accepted)); err != nil {
				return emitted, unsuccessful, err
			}
			seq++
		}

		if !finalDecision.MayDispatch {
			blocked := blockedResult(runID, row.Instruction, seq, clock(), finalDecision.Reason, policyMessage, false)
			if err := appendEntry(ResultEntry(blocked)); err != nil {
				return emitted, unsuccessful, err
			}
			unsuccessful++
			continue
		}

		resolved, resolveErr := r.Registry.Resolve(row.Instruction.Target)
		request := adapter.Request{
			RunID: runID, InstructionID: row.Instruction.ID, Target: row.Instruction.Target,
			Operation: row.Instruction.Op, Payload: row.Instruction.Payload, CWD: baseDecision.EffectiveCWD,
		}
		if resolveErr != nil {
			result, mapErr := ResultForAdapterError(request, AdapterEnvelope{EventID: makeEventID(runID, seq), Seq: seq, RecordedAt: clock()}, nil, resolveErr)
			if mapErr != nil {
				return emitted, unsuccessful, mapErr
			}
			if err := appendEntry(ResultEntry(result)); err != nil {
				return emitted, unsuccessful, err
			}
			unsuccessful++
			continue
		}

		started := ResultRow{
			EventID: makeEventID(runID, seq), Version: ResultVersionV1, RunID: runID,
			InstructionID: row.Instruction.ID, Target: row.Instruction.Target,
			Kind: ResultStarted, Seq: seq, RecordedAt: clock(),
		}
		if err := appendEntry(ResultEntry(started)); err != nil {
			return emitted, unsuccessful, err
		}
		seq++

		runContext := ctx
		cancel := func() {}
		if baseDecision.TimeoutSeconds > 0 {
			runContext, cancel = context.WithTimeout(ctx, time.Duration(baseDecision.TimeoutSeconds)*time.Second)
		}
		emitErr := error(nil)
		completion, adapterErr := resolved.Run(runContext, request, func(output adapter.Output) error {
			result, err := ResultForAdapterOutput(request, AdapterEnvelope{EventID: makeEventID(runID, seq), Seq: seq, RecordedAt: clock()}, output)
			if err != nil {
				emitErr = err
				return err
			}
			if err := appendEntry(ResultEntry(result)); err != nil {
				emitErr = err
				return err
			}
			seq++
			return nil
		})
		cancel()
		if emitErr != nil {
			return emitted, unsuccessful, emitErr
		}

		var terminal ResultRow
		if adapterErr != nil {
			terminal, err = ResultForAdapterError(request, AdapterEnvelope{EventID: makeEventID(runID, seq), Seq: seq, RecordedAt: clock()}, completion.NativeSessionID, adapterErr)
		} else {
			terminal, err = ResultForAdapterCompletion(request, AdapterEnvelope{EventID: makeEventID(runID, seq), Seq: seq, RecordedAt: clock()}, completion)
			if err != nil {
				terminal, err = ResultForAdapterError(request, AdapterEnvelope{EventID: makeEventID(runID, seq), Seq: seq, RecordedAt: clock()}, completion.NativeSessionID,
					&adapter.FailureError{Class: adapter.FailureFailed, Code: "protocol_error", Message: err.Error()})
			}
		}
		if err != nil {
			return emitted, unsuccessful, err
		}
		if err := appendEntry(ResultEntry(terminal)); err != nil {
			return emitted, unsuccessful, err
		}
		if terminal.Kind != ResultCompleted {
			unsuccessful++
		}
	}
	return emitted, unsuccessful, nil
}

func blockedResult(runID string, instruction Instruction, seq int, at time.Time, code, message string, retryable bool) ResultRow {
	return ResultRow{
		EventID: makeEventID(runID, seq), Version: ResultVersionV1, RunID: runID,
		InstructionID: instruction.ID, Target: instruction.Target, Kind: ResultBlocked,
		Seq: seq, RecordedAt: at, Error: &ResultError{Code: code, Message: message, Retryable: &retryable},
	}
}

func newMonotonicClock(now func() time.Time) func() time.Time {
	var last time.Time
	return func() time.Time {
		current := now().UTC()
		if !last.IsZero() && !current.After(last) {
			current = last.Add(time.Nanosecond)
		}
		last = current
		return current
	}
}

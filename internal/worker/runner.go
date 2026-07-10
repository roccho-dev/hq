package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hq/internal/worker/adapter"
	"hq/internal/workersafety"
)

type EntryAppender interface { Append(LogEntry) error }
type Runner struct { Engine Engine; Registry *adapter.Registry; Approvals ApprovalStore; Now func() time.Time }

func NewRunner(workspaceRoot string, registry *adapter.Registry, approvals ApprovalStore) Runner {
	if registry == nil { registry, _ = adapter.NewRegistry() }
	if approvals.byInstruction == nil { approvals = EmptyApprovalStore() }
	return Runner{Engine: NewEngine(workspaceRoot), Registry: registry, Approvals: approvals, Now: time.Now}
}

// Process is the single executable worker sequence. Validation and policy finish
// before registry lookup. A started result is synced before adapter invocation.
// Running evidence is never blindly replayed: only an exact verified provider
// idempotency contract may resume it with the same key.
func (r Runner) Process(ctx context.Context, rows []ReadRow, prior LogData, replay bool, sink EntryAppender) ([]LogEntry, int, error) {
	if sink == nil { return nil, 0, errors.New("event sink is required") }
	if r.Now == nil { r.Now = time.Now }
	if r.Engine.Now == nil { r.Engine.Now = r.Now }
	if r.Engine.Contract.Version == "" { r.Engine.Contract = DefaultContract() }
	if r.Registry == nil { r.Registry, _ = adapter.NewRegistry() }
	if r.Approvals.byInstruction == nil { r.Approvals = EmptyApprovalStore() }
	validation := r.Engine.Contract.ValidateRows(rows)
	priorRuns := latestRuns(prior.Results)
	var emitted []LogEntry
	unsuccessful := 0
	appendEntry := func(entry LogEntry) error {
		redacted, err := RedactLogEntry(entry); if err != nil { return err }
		if err := sink.Append(redacted); err != nil { return err }
		emitted = append(emitted, redacted); return nil
	}
	for index, row := range rows {
		if len(validation[index]) != 0 {
			if err := appendEntry(ValidationEntry(makeValidation(row, validation[index][0]))); err != nil { return emitted, unsuccessful, err }
			unsuccessful++; continue
		}
		priorRun, exists := priorRuns[row.Instruction.ID]
		if exists && !replay && priorRun.Status != StatusQueued && priorRun.Status != StatusRunning {
			entry := ValidationEntry(makeValidation(row, Diagnostic{Code: "duplicate_id", Field: "id", Message: "instruction already has terminal or invalid durable evidence; use --replay to create a new run"}))
			if err := appendEntry(entry); err != nil { return emitted, unsuccessful, err }
			unsuccessful++; continue
		}
		clock := newMonotonicClock(r.Now)
		runID, seq, newRun, recovering := "", 0, true, false
		if exists && !replay {
			runID, seq, newRun = priorRun.RunID, priorRun.NextSeq, false
			recovering = priorRun.Status == StatusRunning
		} else { runID = makeRunID(row, clock()) }
		digest, err := InstructionDigest(row.Instruction)
		if err != nil { return emitted, unsuccessful, fmt.Errorf("digest instruction %q: %w", row.Instruction.ID, err) }
		baseDecision := r.Engine.Policy.Evaluate(row.Instruction)
		finalDecision := workersafety.EvaluatePolicy(workersafety.PolicyRequest{
			InstructionID: row.Instruction.ID, RunID: runID, Target: row.Instruction.Target, Operation: row.Instruction.Op,
			InstructionDigest: digest, Risk: workersafety.RiskDangerous, Policy: workersafety.ExecutionPolicy{RequireApproval: true},
			Approval: r.Approvals.ApprovalFor(row.Instruction.ID),
		})
		policyMessage := "dispatch denied by exact digest-bound approval policy"
		if !baseDecision.Allowed { finalDecision.Status = workersafety.PolicyBlocked; finalDecision.Reason = baseDecision.Code; finalDecision.MayDispatch = false; finalDecision.ApprovedBy = ""; policyMessage = baseDecision.Message }
		if err := appendEntry(PolicyEntry(finalDecision)); err != nil { return emitted, unsuccessful, err }
		if newRun {
			accepted := ResultRow{EventID: makeEventID(runID, seq), Version: ResultVersionV1, RunID: runID, InstructionID: row.Instruction.ID, Target: row.Instruction.Target, Kind: ResultAccepted, Seq: seq, RecordedAt: clock()}
			if err := appendEntry(ResultEntry(accepted)); err != nil { return emitted, unsuccessful, err }
			seq++
		}
		priorProvider := priorStartedProvider(prior.Results, runID)
		if recovering && (priorProvider == nil || priorProvider.IdempotencyContract == "" || priorProvider.IdempotencyKey == "") {
			if err := appendEntry(ResultEntry(reconcileResult(runID, row.Instruction, seq, clock(), "provider effect may have occurred before durable terminal evidence"))); err != nil { return emitted, unsuccessful, err }
			unsuccessful++; continue
		}
		if !finalDecision.MayDispatch {
			blocked := blockedResult(runID, row.Instruction, seq, clock(), finalDecision.Reason, policyMessage, false)
			if err := appendEntry(ResultEntry(blocked)); err != nil { return emitted, unsuccessful, err }
			unsuccessful++; continue
		}
		registration, resolveErr := r.Registry.ResolveRegistration(row.Instruction.Target)
		request := adapter.Request{RunID: runID, InstructionID: row.Instruction.ID, Target: row.Instruction.Target, Operation: row.Instruction.Op, Payload: row.Instruction.Payload, CWD: baseDecision.EffectiveCWD}
		if recovering {
			if resolveErr != nil || registration.Provider == nil || !providerMatches(*priorProvider, *registration.Provider) {
				if err := appendEntry(ResultEntry(reconcileResult(runID, row.Instruction, seq, clock(), "verified idempotent provider binding is unavailable or changed"))); err != nil { return emitted, unsuccessful, err }
				unsuccessful++; continue
			}
			request.IdempotencyKey = priorProvider.IdempotencyKey
		} else if resolveErr == nil && registration.Provider != nil && registration.Provider.IdempotencyContract != "" {
			request.IdempotencyKey = "hq-idem-" + runID
		}
		if resolveErr != nil {
			result, mapErr := ResultForAdapterError(request, AdapterEnvelope{EventID: makeEventID(runID, seq), Seq: seq, RecordedAt: clock()}, nil, resolveErr)
			if mapErr != nil { return emitted, unsuccessful, mapErr }
			if err := appendEntry(ResultEntry(result)); err != nil { return emitted, unsuccessful, err }
			unsuccessful++; continue
		}
		if !recovering {
			started := ResultRow{EventID: makeEventID(runID, seq), Version: ResultVersionV1, RunID: runID, InstructionID: row.Instruction.ID, Target: row.Instruction.Target, Kind: ResultStarted, Seq: seq, RecordedAt: clock()}
			if registration.Provider != nil { started.Provider = providerEvidence(*registration.Provider, request.IdempotencyKey) }
			if err := appendEntry(ResultEntry(started)); err != nil { return emitted, unsuccessful, err }
			seq++
		}
		runContext := ctx; cancel := func() {}
		if baseDecision.TimeoutSeconds > 0 { runContext, cancel = context.WithTimeout(ctx, time.Duration(baseDecision.TimeoutSeconds)*time.Second) }
		emitErr := error(nil)
		completion, adapterErr := registration.Adapter.Run(runContext, request, func(output adapter.Output) error {
			result, err := ResultForAdapterOutput(request, AdapterEnvelope{EventID: makeEventID(runID, seq), Seq: seq, RecordedAt: clock()}, output)
			if err != nil { emitErr = err; return err }
			if err := appendEntry(ResultEntry(result)); err != nil { emitErr = err; return err }
			seq++; return nil
		})
		cancel(); if emitErr != nil { return emitted, unsuccessful, emitErr }
		var terminal ResultRow
		if adapterErr != nil { terminal, err = ResultForAdapterError(request, AdapterEnvelope{EventID: makeEventID(runID, seq), Seq: seq, RecordedAt: clock()}, completion.NativeSessionID, adapterErr) } else {
			terminal, err = ResultForAdapterCompletion(request, AdapterEnvelope{EventID: makeEventID(runID, seq), Seq: seq, RecordedAt: clock()}, completion)
			if err != nil { terminal, err = ResultForAdapterError(request, AdapterEnvelope{EventID: makeEventID(runID, seq), Seq: seq, RecordedAt: clock()}, completion.NativeSessionID, &adapter.FailureError{Class: adapter.FailureFailed, Code: "protocol_error", Message: err.Error()}) }
		}
		if err != nil { return emitted, unsuccessful, err }
		if err := appendEntry(ResultEntry(terminal)); err != nil { return emitted, unsuccessful, err }
		if terminal.Kind != ResultCompleted { unsuccessful++ }
	}
	return emitted, unsuccessful, nil
}

func providerEvidence(descriptor adapter.ProviderDescriptor, key string) *ProviderEvidence {
	return &ProviderEvidence{CapabilityID: descriptor.CapabilityID, ProviderID: descriptor.ProviderID, ContractVersion: descriptor.ContractVersion, DeploymentID: descriptor.DeploymentID, ProviderKind: descriptor.ProviderKind, IntegrityDigest: descriptor.IntegrityDigest, IdempotencyContract: descriptor.IdempotencyContract, IdempotencyKey: key}
}
func providerMatches(prior ProviderEvidence, current adapter.ProviderDescriptor) bool {
	candidate := ProviderEvidence{CapabilityID: current.CapabilityID, ProviderID: current.ProviderID, ContractVersion: current.ContractVersion, DeploymentID: current.DeploymentID, ProviderKind: current.ProviderKind, IntegrityDigest: current.IntegrityDigest, IdempotencyContract: current.IdempotencyContract}
	return prior.SameProvider(candidate) && prior.IdempotencyContract != "" && prior.IdempotencyKey != ""
}
func priorStartedProvider(results []ResultRow, runID string) *ProviderEvidence {
	var selected *ProviderEvidence; best := -1
	for _, row := range results { if row.RunID == runID && row.Kind == ResultStarted && row.Provider != nil && row.Seq > best { copy := *row.Provider; selected = &copy; best = row.Seq } }
	return selected
}
func reconcileResult(runID string, instruction Instruction, seq int, at time.Time, message string) ResultRow { return blockedResult(runID, instruction, seq, at, "reconcile_required", message, false) }
func blockedResult(runID string, instruction Instruction, seq int, at time.Time, code, message string, retryable bool) ResultRow { return ResultRow{EventID: makeEventID(runID, seq), Version: ResultVersionV1, RunID: runID, InstructionID: instruction.ID, Target: instruction.Target, Kind: ResultBlocked, Seq: seq, RecordedAt: at, Error: &ResultError{Code: code, Message: message, Retryable: &retryable}} }
func newMonotonicClock(now func() time.Time) func() time.Time { var last time.Time; return func() time.Time { current := now().UTC(); if !last.IsZero() && !current.After(last) { current = last.Add(time.Nanosecond) }; last = current; return current } }

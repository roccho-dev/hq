package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hq/internal/worker/adapter"
	"hq/internal/workersafety"
	"strings"
	"time"
)

type EntryAppender interface{ Append(LogEntry) error }
type Runner struct {
	Engine    Engine
	Registry  *adapter.Registry
	Views     adapter.RunViewGateway
	Approvals ApprovalStore
	Now       func() time.Time
}

func NewRunner(root string, registry *adapter.Registry, approvals ApprovalStore) Runner {
	if registry == nil {
		registry, _ = adapter.NewRegistry()
	}
	if approvals.byInstruction == nil {
		approvals = EmptyApprovalStore()
	}
	return Runner{Engine: NewEngine(root), Registry: registry, Approvals: approvals, Now: time.Now}
}
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
	if r.Registry == nil {
		r.Registry, _ = adapter.NewRegistry()
	}
	if r.Approvals.byInstruction == nil {
		r.Approvals = EmptyApprovalStore()
	}
	validation := r.Engine.Contract.ValidateRows(rows)
	priorRuns := latestRuns(prior.Results)
	var emitted []LogEntry
	unsuccessful := 0
	appendEntry := func(e LogEntry) error {
		x, err := RedactLogEntry(e)
		if err != nil {
			return err
		}
		if err := sink.Append(x); err != nil {
			return err
		}
		emitted = append(emitted, x)
		return nil
	}
	for i, row := range rows {
		if len(validation[i]) != 0 {
			if err := appendEntry(ValidationEntry(makeValidation(row, validation[i][0]))); err != nil {
				return emitted, unsuccessful, err
			}
			unsuccessful++
			continue
		}
		pr, exists := priorRuns[row.Instruction.ID]
		if exists && !replay && pr.Status != StatusQueued && pr.Status != StatusRunning {
			e := ValidationEntry(makeValidation(row, Diagnostic{Code: "duplicate_id", Field: "id", Message: "instruction already has terminal or invalid durable evidence; use --replay to create a new run"}))
			if err := appendEntry(e); err != nil {
				return emitted, unsuccessful, err
			}
			unsuccessful++
			continue
		}
		clock := newMonotonicClock(r.Now)
		runID, seq, newRun, recovering := "", 0, true, false
		if exists && !replay {
			runID, seq, newRun = pr.RunID, pr.NextSeq, false
			recovering = pr.Status == StatusRunning
		} else {
			runID = makeRunID(row, clock())
		}
		digest, err := InstructionDigest(row.Instruction)
		if err != nil {
			return emitted, unsuccessful, fmt.Errorf("digest instruction %q: %w", row.Instruction.ID, err)
		}
		base := r.Engine.Policy.Evaluate(row.Instruction)
		decision := workersafety.EvaluatePolicy(workersafety.PolicyRequest{InstructionID: row.Instruction.ID, RunID: runID, Target: row.Instruction.Target, Operation: row.Instruction.Op, InstructionDigest: digest, Risk: workersafety.RiskDangerous, Policy: workersafety.ExecutionPolicy{RequireApproval: true}, Approval: r.Approvals.ApprovalFor(row.Instruction.ID)})
		policyMessage := "dispatch denied by exact digest-bound approval policy"
		if !base.Allowed {
			decision.Status = workersafety.PolicyBlocked
			decision.Reason = base.Code
			decision.MayDispatch = false
			decision.ApprovedBy = ""
			policyMessage = base.Message
		}
		held := !recovering && !decision.MayDispatch && decision.Status == workersafety.PolicyApprovalRequired && isVerifiedResourceInvocation(row.Instruction)
		if !held || !matchingPolicyRecorded(prior.Policies, decision) {
			if err := appendEntry(PolicyEntry(decision)); err != nil {
				return emitted, unsuccessful, err
			}
		}
		if newRun {
			accepted := ResultRow{EventID: makeEventID(runID, seq), Version: ResultVersionV1, RunID: runID, InstructionID: row.Instruction.ID, Target: row.Instruction.Target, Kind: ResultAccepted, Seq: seq, RecordedAt: clock()}
			if err := appendEntry(ResultEntry(accepted)); err != nil {
				return emitted, unsuccessful, err
			}
			seq++
		}
		priorProvider := priorStartedProvider(prior.Results, runID)
		if recovering && (priorProvider == nil || priorProvider.IdempotencyContract == "" || priorProvider.IdempotencyKey == "") {
			if err := appendEntry(ResultEntry(reconcileResult(runID, row.Instruction, seq, clock(), "provider effect may have occurred before durable terminal evidence"))); err != nil {
				return emitted, unsuccessful, err
			}
			unsuccessful++
			continue
		}
		if held {
			continue
		}
		if !decision.MayDispatch {
			if err := appendEntry(ResultEntry(blockedResult(runID, row.Instruction, seq, clock(), decision.Reason, policyMessage, false))); err != nil {
				return emitted, unsuccessful, err
			}
			unsuccessful++
			continue
		}
		registration, resolveErr := r.Registry.ResolveRegistration(row.Instruction.Target)
		request := adapter.Request{RunID: runID, InstructionID: row.Instruction.ID, Target: row.Instruction.Target, Operation: row.Instruction.Op, Payload: row.Instruction.Payload, CWD: base.EffectiveCWD}
		prepared := adapter.Prepared{}
		if resolveErr == nil {
			prepared, resolveErr = registration.Prepare(ctx, request)
		}
		if recovering {
			if resolveErr != nil || prepared.Provider == nil || !providerMatches(*priorProvider, *prepared.Provider) {
				if err := appendEntry(ResultEntry(reconcileResult(runID, row.Instruction, seq, clock(), "verified idempotent provider binding is unavailable or changed"))); err != nil {
					return emitted, unsuccessful, err
				}
				unsuccessful++
				continue
			}
			request.IdempotencyKey = priorProvider.IdempotencyKey
		} else if resolveErr == nil && prepared.Provider != nil && prepared.Provider.IdempotencyContract != "" {
			request.IdempotencyKey = "hq-idem-" + runID
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
		var runView *adapter.RunView
		if !recovering && prepared.RunViewRequired {
			if r.Views == nil {
				if err := appendEntry(ResultEntry(blockedResult(runID, row.Instruction, seq, clock(), "view_unavailable", "required run view gateway is unavailable", false))); err != nil {
					return emitted, unsuccessful, err
				}
				unsuccessful++
				continue
			}
			runView, resolveErr = r.Views.Open(ctx, request)
			if resolveErr != nil || runView == nil {
				message := "required run view could not be opened"
				if resolveErr != nil && strings.TrimSpace(resolveErr.Error()) != "" {
					message = resolveErr.Error()
				}
				if err := appendEntry(ResultEntry(blockedResult(runID, row.Instruction, seq, clock(), "view_unavailable", message, false))); err != nil {
					return emitted, unsuccessful, err
				}
				unsuccessful++
				continue
			}
			if err := runView.Validate(); err != nil {
				if appendErr := appendEntry(ResultEntry(blockedResult(runID, row.Instruction, seq, clock(), "view_unavailable", err.Error(), false))); appendErr != nil {
					return emitted, unsuccessful, appendErr
				}
				unsuccessful++
				continue
			}
		}
		if !recovering {
			started := ResultRow{EventID: makeEventID(runID, seq), Version: ResultVersionV1, RunID: runID, InstructionID: row.Instruction.ID, Target: row.Instruction.Target, Kind: ResultStarted, Seq: seq, RecordedAt: clock()}
			if prepared.Provider != nil {
				started.Provider = providerEvidence(*prepared.Provider, request.IdempotencyKey)
			}
			if runView != nil {
				started.View = &RunViewEvidence{
					Version: RunViewVersionV1, Policy: runView.Policy,
					Provider:        *providerEvidence(runView.Provider, ""),
					NativeSessionID: runView.NativeSessionID,
				}
			}
			if err := appendEntry(ResultEntry(started)); err != nil {
				return emitted, unsuccessful, err
			}
			seq++
		}
		runCtx := ctx
		cancel := func() {}
		if base.TimeoutSeconds > 0 {
			runCtx, cancel = context.WithTimeout(ctx, time.Duration(base.TimeoutSeconds)*time.Second)
		}
		emitErr := error(nil)
		completion, adapterErr := prepared.Adapter.Run(runCtx, request, func(output adapter.Output) error {
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
				terminal, err = ResultForAdapterError(request, AdapterEnvelope{EventID: makeEventID(runID, seq), Seq: seq, RecordedAt: clock()}, completion.NativeSessionID, &adapter.FailureError{Class: adapter.FailureFailed, Code: "protocol_error", Message: err.Error()})
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

func isVerifiedResourceInvocation(instruction Instruction) bool {
	if instruction.Target != "local-tool" {
		return false
	}
	var payload struct {
		ActionID      *string  `json:"action_id"`
		PolicyVersion *string  `json:"policy_version"`
		Argv          []string `json:"argv"`
	}
	if json.Unmarshal(instruction.Payload, &payload) != nil {
		return false
	}
	return payload.ActionID == nil && payload.PolicyVersion != nil && payload.Argv != nil
}

func matchingPolicyRecorded(policies []workersafety.PolicyDecision, decision workersafety.PolicyDecision) bool {
	for index := len(policies) - 1; index >= 0; index-- {
		prior := policies[index]
		if prior.InstructionID != decision.InstructionID || prior.RunID != decision.RunID {
			continue
		}
		return prior.InstructionDigest == decision.InstructionDigest && prior.Status == decision.Status && prior.Reason == decision.Reason && prior.MayDispatch == decision.MayDispatch
	}
	return false
}

func providerEvidence(descriptor adapter.ProviderDescriptor, key string) *ProviderEvidence {
	dependencies := make([]ProviderDependencyEvidence, 0, len(descriptor.Dependencies))
	for _, dependency := range descriptor.Dependencies {
		dependencies = append(dependencies, ProviderDependencyEvidence{
			Name: dependency.Name, ProviderID: dependency.ProviderID, ContractVersion: dependency.ContractVersion,
			DeploymentID: dependency.DeploymentID, ProviderKind: dependency.ProviderKind,
			IntegrityDigest: dependency.IntegrityDigest, ConfigurationDigest: dependency.ConfigurationDigest,
		})
	}
	return &ProviderEvidence{
		CapabilityID: descriptor.CapabilityID, ProviderID: descriptor.ProviderID, ContractVersion: descriptor.ContractVersion,
		DeploymentID: descriptor.DeploymentID, ProviderKind: descriptor.ProviderKind, IntegrityDigest: descriptor.IntegrityDigest,
		ConfigurationDigest: descriptor.ConfigurationDigest, Dependencies: dependencies,
		IdempotencyContract: descriptor.IdempotencyContract, IdempotencyKey: key,
	}
}
func providerMatches(prior ProviderEvidence, descriptor adapter.ProviderDescriptor) bool {
	current := providerEvidence(descriptor, "")
	return prior.SameProvider(*current) && prior.IdempotencyContract != "" && prior.IdempotencyKey != ""
}
func priorStartedProvider(results []ResultRow, runID string) *ProviderEvidence {
	var selected *ProviderEvidence
	best := -1
	for _, row := range results {
		if row.RunID == runID && row.Kind == ResultStarted && row.Provider != nil && row.Seq > best {
			copy := *row.Provider
			copy.Dependencies = append([]ProviderDependencyEvidence(nil), row.Provider.Dependencies...)
			selected = &copy
			best = row.Seq
		}
	}
	return selected
}
func reconcileResult(runID string, i Instruction, seq int, at time.Time, message string) ResultRow {
	return blockedResult(runID, i, seq, at, "reconcile_required", message, false)
}
func blockedResult(runID string, i Instruction, seq int, at time.Time, code, message string, retryable bool) ResultRow {
	return ResultRow{EventID: makeEventID(runID, seq), Version: ResultVersionV1, RunID: runID, InstructionID: i.ID, Target: i.Target, Kind: ResultBlocked, Seq: seq, RecordedAt: at, Error: &ResultError{Code: code, Message: message, Retryable: &retryable}}
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

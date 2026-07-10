package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
)

type Engine struct {
	Contract Contract
	Policy   Policy
	Now      func() time.Time
}

func NewEngine(workspaceRoot string) Engine {
	return Engine{Contract: DefaultContract(), Policy: DefaultPolicy(workspaceRoot), Now: time.Now}
}

func (e Engine) Plan(row ReadRow, validation []Diagnostic) PlanRow {
	plan := PlanRow{
		Version:         PlanVersionV1,
		Source:          row.Source,
		InstructionID:   row.Instruction.ID,
		Target:          row.Instruction.Target,
		Op:              row.Instruction.Op,
		ExpectedAdapter: row.Instruction.Target,
		Payload:         SummarizePayload(row.Instruction.Payload),
		Validation:      validation,
		Decision:        PlanBlocked,
		Policy:          PolicyDecision{Allowed: false, Code: "not_evaluated", Message: "validation must pass before policy"},
	}
	if len(validation) != 0 {
		return plan
	}
	plan.Policy = e.Policy.Evaluate(row.Instruction)
	if plan.Policy.Allowed {
		plan.Decision = PlanAccepted
	}
	return plan
}

func (e Engine) DryRun(rows []ReadRow, w io.Writer) (blocked int, err error) {
	validation := e.Contract.ValidateRows(rows)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for i, row := range rows {
		plan := e.Plan(row, validation[i])
		if plan.Decision != PlanAccepted {
			blocked++
		}
		if err := enc.Encode(plan); err != nil {
			return blocked, err
		}
	}
	return blocked, nil
}

// EvaluateNormal records validation.v1 and result.v1 rows but never calls a
// target adapter. Concrete dispatch belongs to the adapter lane. Every valid
// row therefore reaches a durable accepted->blocked run until a registry is
// wired in a later slice.
func (e Engine) EvaluateNormal(rows []ReadRow, prior LogData, replay bool) []LogEntry {
	now := e.Now
	if now == nil {
		now = time.Now
	}
	validation := e.Contract.ValidateRows(rows)
	priorRuns := latestRuns(prior.Results)
	entries := make([]LogEntry, 0, len(rows)*2)
	for i, row := range rows {
		at := now().UTC()
		if len(validation[i]) != 0 {
			entries = append(entries, ValidationEntry(makeValidation(row, validation[i][0])))
			continue
		}
		if priorRun, exists := priorRuns[row.Instruction.ID]; exists && !replay {
			if priorRun.Status == StatusQueued {
				decision := e.Policy.Evaluate(row.Instruction)
				entries = append(entries, ResultEntry(makeBlockedResult(priorRun.RunID, row.Instruction, priorRun.NextSeq, decision, at)))
				continue
			}
			entries = append(entries, ValidationEntry(makeValidation(row, Diagnostic{
				Code:    "duplicate_id",
				Field:   "id",
				Message: "instruction already has durable run evidence; use --replay to create a new run",
			})))
			continue
		}

		runID := makeRunID(row, at)
		decision := e.Policy.Evaluate(row.Instruction)
		entries = append(entries, ResultEntry(ResultRow{
			EventID:       makeEventID(runID, 0),
			Version:       ResultVersionV1,
			RunID:         runID,
			InstructionID: row.Instruction.ID,
			Target:        row.Instruction.Target,
			Kind:          ResultAccepted,
			Seq:           0,
			RecordedAt:    at,
		}))
		entries = append(entries, ResultEntry(makeBlockedResult(runID, row.Instruction, 1, decision, at.Add(time.Nanosecond))))
	}
	return entries
}

func makeValidation(row ReadRow, diagnostic Diagnostic) ValidationRow {
	retryable := false
	validation := ValidationRow{
		Version:    ValidationVersionV1,
		SourceLine: row.Source.Line,
		Status:     StatusBlocked,
		Error:      ResultError{Code: diagnostic.Code, Message: diagnostic.Message, Retryable: &retryable},
	}
	if row.Instruction.ID != "" {
		validation.InstructionID = row.Instruction.ID
	}
	if _, ok := canonicalTargets[row.Instruction.Target]; ok {
		validation.Target = row.Instruction.Target
	}
	return validation
}

func makeBlockedResult(runID string, instruction Instruction, seq int, policy PolicyDecision, at time.Time) ResultRow {
	code := policy.Code
	message := policy.Message
	retryable := false
	if policy.Allowed {
		code = "adapter_unavailable"
		message = "validated instruction has no installed concrete adapter"
		retryable = true
	}
	return ResultRow{
		EventID:       makeEventID(runID, seq),
		Version:       ResultVersionV1,
		RunID:         runID,
		InstructionID: instruction.ID,
		Target:        instruction.Target,
		Kind:          ResultBlocked,
		Seq:           seq,
		RecordedAt:    at,
		Error:         &ResultError{Code: code, Message: message, Retryable: &retryable},
	}
}

type runEvidence struct {
	RunID   string
	Status  string
	NextSeq int
	At      time.Time
}

func latestRuns(results []ResultRow) map[string]runEvidence {
	byRun := map[string][]ResultRow{}
	for _, row := range results {
		byRun[row.RunID] = append(byRun[row.RunID], row)
	}
	out := map[string]runEvidence{}
	for _, rows := range byRun {
		sort.Slice(rows, func(i, j int) bool { return rows[i].Seq < rows[j].Seq })
		status, _, diags := projectRunState(rows)
		if len(diags) != 0 {
			status = "invalid"
		}
		last := rows[len(rows)-1]
		evidence := runEvidence{RunID: last.RunID, Status: status, NextSeq: last.Seq + 1, At: last.RecordedAt}
		current, ok := out[last.InstructionID]
		if !ok || evidence.At.After(current.At) {
			out[last.InstructionID] = evidence
		}
	}
	return out
}

func makeRunID(row ReadRow, at time.Time) string {
	seed := fmt.Sprintf("%s\x00%d\x00%s\x00%d", row.Source.Path, row.Source.Line, row.Instruction.ID, at.UnixNano())
	h := sha256.Sum256([]byte(seed))
	return "run-" + hex.EncodeToString(h[:8])
}

func makeEventID(runID string, seq int) string {
	return fmt.Sprintf("evt-%s-%06d", runID, seq)
}

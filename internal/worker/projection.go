package worker

import (
	"encoding/json"
	"fmt"
	"sort"
)

var terminalStatuses = map[string]struct{}{
	StatusCompleted: {}, StatusFailed: {}, StatusBlocked: {}, StatusTimeout: {}, StatusCancelled: {},
}

func Project(instructions []Instruction, results []ResultRow) ([]RunProjection, []Diagnostic) {
	byInstruction := map[string]Instruction{}
	for _, instruction := range instructions {
		byInstruction[instruction.ID] = instruction
	}
	byRun := map[string][]ResultRow{}
	for _, row := range results {
		byRun[row.RunID] = append(byRun[row.RunID], row)
	}
	runIDs := make([]string, 0, len(byRun))
	for runID := range byRun {
		runIDs = append(runIDs, runID)
	}
	sort.Strings(runIDs)
	var out []RunProjection
	var diagnostics []Diagnostic
	for _, runID := range runIDs {
		rows := byRun[runID]
		sort.Slice(rows, func(i, j int) bool { return rows[i].Seq < rows[j].Seq })
		status, nativeID, runDiags := projectRunState(rows)
		if len(runDiags) != 0 {
			diagnostics = append(diagnostics, runDiags...)
			continue
		}
		first := rows[0]
		instruction, hasInstruction := byInstruction[first.InstructionID]
		cwd := "."
		if hasInstruction {
			cwd = instructionCWD(instruction)
			if instruction.Target != first.Target {
				diagnostics = append(diagnostics, Diagnostic{Code: "target_mismatch", Field: runID, Message: "result target differs from instruction target"})
				continue
			}
		} else if instructions != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: "unknown_instruction", Field: runID, Message: "result run references an unknown instruction"})
			continue
		}
		last := rows[len(rows)-1]
		projection := RunProjection{
			Version:         SessionVersionV1,
			RunID:           runID,
			InstructionID:   first.InstructionID,
			Target:          first.Target,
			Status:          status,
			CWD:             cwd,
			StartedAt:       first.RecordedAt,
			LastEventAt:     last.RecordedAt,
			NativeSessionID: nativeID,
			LastKind:        last.Kind,
		}
		if last.Final != nil {
			projection.FinalPath = last.Final.Path
		}
		if last.Error != nil {
			projection.Error = last.Error.Message
		}
		out = append(out, projection)
	}
	return out, diagnostics
}

func projectRunState(rows []ResultRow) (status, nativeSessionID string, diagnostics []Diagnostic) {
	if len(rows) == 0 {
		return "", "", []Diagnostic{{Code: "empty_run", Message: "run has no result rows"}}
	}
	first := rows[0]
	current := ""
	terminal := false
	for index, row := range rows {
		field := row.RunID
		if err := row.Validate(); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: "invalid_result", Field: field, Message: err.Error()})
			continue
		}
		if row.RunID != first.RunID || row.InstructionID != first.InstructionID || row.Target != first.Target {
			diagnostics = append(diagnostics, Diagnostic{Code: "identity_drift", Field: field, Message: "run_id, instruction_id, and target must remain stable"})
			continue
		}
		if row.Seq != index {
			diagnostics = append(diagnostics, Diagnostic{Code: "non_contiguous_seq", Field: field, Message: fmt.Sprintf("expected seq %d, got %d", index, row.Seq)})
			continue
		}
		if terminal {
			diagnostics = append(diagnostics, Diagnostic{Code: "event_after_terminal", Field: field, Message: "no result event may follow a terminal result"})
			continue
		}
		next, changes, ok := resultStatus(row.Kind, current, index)
		if !ok {
			diagnostics = append(diagnostics, Diagnostic{Code: "invalid_transition", Field: field, Message: fmt.Sprintf("%s cannot follow status %s", row.Kind, current)})
			continue
		}
		if changes {
			current = next
		}
		if _, ok := terminalStatuses[current]; ok {
			terminal = true
		}
		if row.NativeSessionID != nil {
			if nativeSessionID != "" && nativeSessionID != *row.NativeSessionID {
				diagnostics = append(diagnostics, Diagnostic{Code: "native_session_changed", Field: field, Message: "native_session_id cannot change inside a run"})
				continue
			}
			nativeSessionID = *row.NativeSessionID
		}
	}
	if len(diagnostics) != 0 {
		return "", "", diagnostics
	}
	return current, nativeSessionID, nil
}

func resultStatus(kind, current string, index int) (next string, changes, ok bool) {
	switch kind {
	case ResultAccepted:
		return StatusQueued, true, index == 0 && current == ""
	case ResultStarted:
		return StatusRunning, true, current == StatusQueued
	case ResultStdout, ResultStderr:
		return current, false, current == StatusRunning
	case ResultCompleted:
		return StatusCompleted, true, current == StatusRunning
	case ResultFailed:
		return StatusFailed, true, current == StatusRunning
	case ResultBlocked:
		return StatusBlocked, true, current == StatusQueued || current == StatusRunning
	case ResultTimeout:
		return StatusTimeout, true, current == StatusRunning
	case ResultCancelled:
		return StatusCancelled, true, current == StatusQueued || current == StatusRunning
	default:
		return "", false, false
	}
}

func instructionCWD(instruction Instruction) string {
	var payload map[string]any
	if json.Unmarshal(instruction.Payload, &payload) == nil {
		if cwd, ok := payload["cwd"].(string); ok && cwd != "" {
			return cwd
		}
	}
	return "."
}

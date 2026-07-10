package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	LedgerVersionV1    = "worker.ledger.v1"
	RunDetailVersionV1 = "worker.run-detail.v1"
)

// LedgerRow is a transient read model built from canonical instruction.v1 and
// result.v1 evidence. It is not a durable authority and must never be used to
// change run state.
type LedgerRow struct {
	Version         string       `json:"version"`
	RunID           string       `json:"run_id"`
	InstructionID   string       `json:"instruction_id"`
	Target          string       `json:"target"`
	Op              string       `json:"op"`
	Status          string       `json:"status"`
	CWD             string       `json:"cwd"`
	StartedAt       time.Time    `json:"started_at"`
	LastEventAt     time.Time    `json:"last_event_at"`
	NativeSessionID string       `json:"native_session_id,omitempty"`
	LastKind        string       `json:"last_kind"`
	Summary         string       `json:"summary,omitempty"`
	FinalPath       string       `json:"final_path,omitempty"`
	Error           *ResultError `json:"error,omitempty"`
}

// InstructionSummary exposes enough instruction context to understand a run
// without returning the full target payload by default.
type InstructionSummary struct {
	ID        string   `json:"id"`
	Target    string   `json:"target"`
	Op        string   `json:"op"`
	CWD       string   `json:"cwd"`
	CreatedAt string   `json:"created_at"`
	Summary   string   `json:"summary"`
	Reason    string   `json:"reason,omitempty"`
	ReplyTo   string   `json:"reply_to,omitempty"`
	Labels    []string `json:"labels,omitempty"`
}

// RunDetail is a transient, rebuildable read model for one run. Error is an
// in-memory alias of Run.Error for text rendering and is excluded from JSON so
// the wire representation has one structured error field.
type RunDetail struct {
	Version     string             `json:"version"`
	Run         LedgerRow          `json:"run"`
	Instruction InstructionSummary `json:"instruction"`
	Events      []ResultRow        `json:"events"`
	Final       *FinalResult       `json:"final,omitempty"`
	Error       *ResultError       `json:"-"`
	AttachHint  string             `json:"attach_hint,omitempty"`
}

// ObservationError is a machine-readable failure returned by observation
// operations. The CLI serializes Code and Message without inventing run state.
type ObservationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ObservationError) Error() string { return e.Message }

// LoadInstructionsForObservation reads the durable instruction stream, keeps
// later valid rows visible after malformed rows, and returns line-scoped
// diagnostics for every rejected row.
func LoadInstructionsForObservation(path string) ([]Instruction, []Diagnostic, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	rows, err := ReadInstructions(path, f)
	if err != nil {
		return nil, nil, err
	}
	validation := DefaultContract().ValidateRows(rows)
	instructions := make([]Instruction, 0, len(rows))
	var diagnostics []Diagnostic
	for index, row := range rows {
		diags := validation[index]
		if len(diags) == 0 {
			instructions = append(instructions, row.Instruction)
			continue
		}
		for _, diagnostic := range diags {
			field := fmt.Sprintf("line %d", row.Source.Line)
			if diagnostic.Field != "" {
				field += ":" + diagnostic.Field
			}
			diagnostic.Field = field
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	return instructions, diagnostics, nil
}

// BuildLedger produces the one cross-target run list. Canonical instruction
// and result rows remain authority; the returned rows are disposable views.
func BuildLedger(instructions []Instruction, results []ResultRow) ([]LedgerRow, []Diagnostic) {
	projection, diagnostics := Project(instructions, results)
	byInstruction := make(map[string]Instruction, len(instructions))
	for _, instruction := range instructions {
		byInstruction[instruction.ID] = instruction
	}
	byRun := make(map[string][]ResultRow)
	for _, row := range results {
		byRun[row.RunID] = append(byRun[row.RunID], row)
	}

	ledger := make([]LedgerRow, 0, len(projection))
	for _, session := range projection {
		instruction, ok := byInstruction[session.InstructionID]
		if !ok {
			diagnostics = append(diagnostics, Diagnostic{
				Code:    "unknown_instruction",
				Field:   session.RunID,
				Message: "ledger row requires linked durable instruction evidence",
			})
			continue
		}
		events := append([]ResultRow(nil), byRun[session.RunID]...)
		sort.Slice(events, func(i, j int) bool { return events[i].Seq < events[j].Seq })
		row := LedgerRow{
			Version:         LedgerVersionV1,
			RunID:           session.RunID,
			InstructionID:   session.InstructionID,
			Target:          session.Target,
			Op:              instruction.Op,
			Status:          session.Status,
			CWD:             session.CWD,
			StartedAt:       session.StartedAt,
			LastEventAt:     session.LastEventAt,
			NativeSessionID: session.NativeSessionID,
			LastKind:        session.LastKind,
			Summary:         summarizeRun(instruction, events),
			FinalPath:       session.FinalPath,
		}
		for index := len(events) - 1; index >= 0; index-- {
			if events[index].Error != nil {
				copyValue := *events[index].Error
				row.Error = &copyValue
				break
			}
		}
		ledger = append(ledger, row)
	}

	sort.SliceStable(ledger, func(i, j int) bool {
		if ledger[i].LastEventAt.Equal(ledger[j].LastEventAt) {
			return ledger[i].RunID < ledger[j].RunID
		}
		return ledger[i].LastEventAt.After(ledger[j].LastEventAt)
	})
	return ledger, diagnostics
}

// BuildRunDetail reconstructs one run from durable rows alone.
func BuildRunDetail(runID string, instructions []Instruction, results []ResultRow) (RunDetail, []Diagnostic, error) {
	if strings.TrimSpace(runID) == "" {
		return RunDetail{}, nil, &ObservationError{Code: "run_id_required", Message: "run id is required"}
	}
	ledger, diagnostics := BuildLedger(instructions, results)
	var selected *LedgerRow
	for index := range ledger {
		if ledger[index].RunID == runID {
			copyValue := ledger[index]
			selected = &copyValue
			break
		}
	}
	if selected == nil {
		hasRunEvidence := false
		for _, row := range results {
			if row.RunID == runID {
				hasRunEvidence = true
				break
			}
		}
		if hasRunEvidence {
			message := fmt.Sprintf("run %q has invalid or unlinked durable evidence", runID)
			for _, diagnostic := range diagnostics {
				if diagnostic.Field == runID {
					message = diagnostic.Message
					break
				}
			}
			return RunDetail{}, diagnostics, &ObservationError{Code: "invalid_run_evidence", Message: message}
		}
		return RunDetail{}, diagnostics, &ObservationError{
			Code:    "run_not_found",
			Message: fmt.Sprintf("run %q was not found in durable evidence", runID),
		}
	}

	byInstruction := make(map[string]Instruction, len(instructions))
	for _, instruction := range instructions {
		byInstruction[instruction.ID] = instruction
	}
	instruction := byInstruction[selected.InstructionID]
	events := make([]ResultRow, 0)
	for _, row := range results {
		if row.RunID == runID {
			events = append(events, row)
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Seq < events[j].Seq })
	detail := RunDetail{
		Version:     RunDetailVersionV1,
		Run:         *selected,
		Instruction: summarizeInstruction(instruction),
		Events:      events,
		Error:       selected.Error,
	}
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Final != nil {
			copyValue := *events[index].Final
			detail.Final = &copyValue
			break
		}
	}
	if selected.NativeSessionID != "" {
		detail.AttachHint = fmt.Sprintf(
			"native session %s is available; use the %s adapter's documented read or attach surface",
			selected.NativeSessionID,
			selected.Target,
		)
	}
	return detail, diagnostics, nil
}

// FollowRun emits current and newly appended result rows for one known run. It
// uses a bounded rescan rather than filesystem notifications so a lost
// notification cannot permanently hide a durable event. Rows already emitted
// must remain byte-equivalent canonical JSON throughout the follow operation.
func FollowRun(ctx context.Context, eventPath, runID string, follow bool, pollInterval time.Duration, emit func(ResultRow) error) error {
	if strings.TrimSpace(runID) == "" {
		return &ObservationError{Code: "run_id_required", Message: "run id is required"}
	}
	if emit == nil {
		return &ObservationError{Code: "emit_required", Message: "tail emitter is required"}
	}
	if pollInterval <= 0 {
		pollInterval = 250 * time.Millisecond
	}

	seenRows := make([]string, 0)
	firstScan := true
	for {
		data, err := LoadEventFile(eventPath)
		if err != nil {
			return err
		}
		events := make([]ResultRow, 0)
		for _, row := range data.Results {
			if row.RunID == runID {
				events = append(events, row)
			}
		}
		sort.Slice(events, func(i, j int) bool { return events[i].Seq < events[j].Seq })
		if firstScan && len(events) == 0 {
			return &ObservationError{
				Code:    "run_not_found",
				Message: fmt.Sprintf("run %q was not found in durable evidence", runID),
			}
		}
		firstScan = false

		if len(events) < len(seenRows) {
			return &ObservationError{
				Code:    "run_evidence_truncated",
				Message: fmt.Sprintf("run %q durable evidence shrank from %d rows to %d", runID, len(seenRows), len(events)),
			}
		}
		if len(events) != 0 {
			if _, _, diagnostics := projectRunState(events); len(diagnostics) != 0 {
				return &ObservationError{Code: "invalid_run_evidence", Message: diagnostics[0].Message}
			}
			for index, row := range events {
				encoded, err := json.Marshal(row)
				if err != nil {
					return err
				}
				canonical := string(encoded)
				if index < len(seenRows) {
					if seenRows[index] != canonical {
						return &ObservationError{
							Code:    "run_evidence_changed",
							Message: fmt.Sprintf("run %q durable evidence changed at seq %d", runID, row.Seq),
						}
					}
					continue
				}
				if row.Seq != len(seenRows) {
					return &ObservationError{
						Code:    "non_contiguous_seq",
						Message: fmt.Sprintf("run %q expected seq %d, got %d", runID, len(seenRows), row.Seq),
					}
				}
				if err := emit(row); err != nil {
					return err
				}
				seenRows = append(seenRows, canonical)
			}
			if isTerminalResultKind(events[len(events)-1].Kind) {
				return nil
			}
		}
		if !follow {
			return nil
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func summarizeInstruction(instruction Instruction) InstructionSummary {
	summary := InstructionSummary{
		ID:        instruction.ID,
		Target:    instruction.Target,
		Op:        instruction.Op,
		CWD:       instructionCWD(instruction),
		CreatedAt: instruction.CreatedAt,
		Summary:   instructionPayloadSummary(instruction),
		Labels:    append([]string(nil), instruction.Labels...),
	}
	if instruction.Reason != nil {
		summary.Reason = *instruction.Reason
	}
	if instruction.ReplyTo != nil {
		summary.ReplyTo = *instruction.ReplyTo
	}
	return summary
}

func summarizeRun(instruction Instruction, events []ResultRow) string {
	for index := len(events) - 1; index >= 0; index-- {
		row := events[index]
		if row.Final != nil && strings.TrimSpace(row.Final.Text) != "" {
			return compactSummary(row.Final.Text, 160)
		}
		if (row.Kind == ResultStdout || row.Kind == ResultStderr) && row.Message != nil && strings.TrimSpace(*row.Message) != "" {
			return compactSummary(*row.Message, 160)
		}
	}
	return instructionPayloadSummary(instruction)
}

func instructionPayloadSummary(instruction Instruction) string {
	var payload struct {
		Prompt string   `json:"prompt"`
		Argv   []string `json:"argv"`
	}
	if json.Unmarshal(instruction.Payload, &payload) == nil {
		if strings.TrimSpace(payload.Prompt) != "" {
			return compactSummary(payload.Prompt, 160)
		}
		if len(payload.Argv) != 0 {
			return compactSummary(strings.Join(payload.Argv, " "), 160)
		}
	}
	if instruction.Reason != nil {
		return compactSummary(*instruction.Reason, 160)
	}
	return instruction.Op + " " + instruction.Target
}

func compactSummary(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

func isTerminalResultKind(kind string) bool {
	switch kind {
	case ResultCompleted, ResultFailed, ResultBlocked, ResultTimeout, ResultCancelled:
		return true
	default:
		return false
	}
}

// EncodeJSONLine writes one stable JSON object followed by a newline.
func EncodeJSONLine(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

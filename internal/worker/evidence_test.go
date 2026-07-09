package worker

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEventLogRejectsDuplicateEvidenceIDs(t *testing.T) {
	result := `{"event_id":"evt-1","version":"result.v1","run_id":"run-1","instruction_id":"ins-1","target":"sh","kind":"accepted","seq":0,"recorded_at":"2026-07-10T00:00:00Z"}`
	if _, err := LoadEventLog(strings.NewReader(result + "\n" + result)); err == nil || !strings.Contains(err.Error(), "duplicate event_id") {
		t.Fatalf("duplicate event_id was not rejected: %v", err)
	}
	decision := `{"decision_id":"decision-1","version":"worker.decision.v1","decision":"blocked","recorded_at":"2026-07-10T00:00:00Z","source":{"path":"queue.jsonl","line":1,"raw":"{}"},"code":"malformed_json","message":"bad row"}`
	if _, err := LoadEventLog(strings.NewReader(decision + "\n" + decision)); err == nil || !strings.Contains(err.Error(), "duplicate decision_id") {
		t.Fatalf("duplicate decision_id was not rejected: %v", err)
	}
}

func TestProjectionRejectsEventAfterTerminalAndNativeSessionDrift(t *testing.T) {
	at := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	retryable := false
	a, b := "session-a", "session-b"

	afterTerminal := []ResultRow{
		{EventID: "e0", Version: ResultVersionV1, RunID: "r-terminal", InstructionID: "i-terminal", Target: "sh", Kind: ResultAccepted, Seq: 0, RecordedAt: at},
		{EventID: "e1", Version: ResultVersionV1, RunID: "r-terminal", InstructionID: "i-terminal", Target: "sh", Kind: ResultBlocked, Seq: 1, RecordedAt: at.Add(time.Second), Error: &ResultError{Code: "policy", Message: "blocked", Retryable: &retryable}},
		{EventID: "e2", Version: ResultVersionV1, RunID: "r-terminal", InstructionID: "i-terminal", Target: "sh", Kind: ResultBlocked, Seq: 2, RecordedAt: at.Add(2 * time.Second), Error: &ResultError{Code: "policy", Message: "blocked again", Retryable: &retryable}},
	}
	if projection, diagnostics := Project(nil, afterTerminal); len(projection) != 0 || !hasDiagnostic(diagnostics, "event_after_terminal") {
		t.Fatalf("projection=%+v diagnostics=%+v", projection, diagnostics)
	}

	message := "working"
	drift := []ResultRow{
		{EventID: "d0", Version: ResultVersionV1, RunID: "r-drift", InstructionID: "i-drift", Target: "codex", Kind: ResultAccepted, Seq: 0, RecordedAt: at},
		{EventID: "d1", Version: ResultVersionV1, RunID: "r-drift", InstructionID: "i-drift", Target: "codex", Kind: ResultStarted, Seq: 1, RecordedAt: at.Add(time.Second), NativeSessionID: &a},
		{EventID: "d2", Version: ResultVersionV1, RunID: "r-drift", InstructionID: "i-drift", Target: "codex", Kind: ResultStdout, Seq: 2, RecordedAt: at.Add(2 * time.Second), NativeSessionID: &b, Message: &message},
	}
	if projection, diagnostics := Project(nil, drift); len(projection) != 0 || !hasDiagnostic(diagnostics, "native_session_changed") {
		t.Fatalf("projection=%+v diagnostics=%+v", projection, diagnostics)
	}
}

func TestPolicyOperationDenyHook(t *testing.T) {
	row := Instruction{
		ID:        "ins-1",
		Version:   InstructionVersionV1,
		Op:        "run",
		Target:    "sh",
		Payload:   json.RawMessage(`{"argv":["true"],"cwd":"."}`),
		CreatedAt: "2026-07-10T00:00:00Z",
	}
	policy := DefaultPolicy(t.TempDir())
	policy.DeniedOps["run"] = struct{}{}
	if decision := policy.Evaluate(row); decision.Allowed || decision.Code != "op_denied" {
		t.Fatalf("decision=%+v", decision)
	}
}

func hasDiagnostic(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

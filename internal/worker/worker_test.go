package worker

import (
	"bufio"
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const validInstruction = `{"id":"ins-sh-001","version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["printf","hello\n"],"cwd":"work"},"created_at":"2026-07-10T00:00:00Z"}`

func TestReadInstructionsPreservesRawLaterRowsAndLineErrors(t *testing.T) {
	input := "\n" + validInstruction + "\n# not a comment\n" + strings.Replace(validInstruction, "ins-sh-001", "ins-sh-002", 1) + "\n"
	rows, err := ReadInstructions("queue.jsonl", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Source.Line != 2 || rows[1].Source.Line != 3 || rows[2].Source.Line != 4 {
		t.Fatalf("rows=%+v", rows)
	}
	if rows[1].ParseError == nil || rows[1].ParseError.Code != "malformed_json" || rows[1].Source.Raw != "# not a comment" {
		t.Fatalf("line-scoped error/raw missing: %+v", rows[1])
	}
	if rows[2].Instruction.ID != "ins-sh-002" {
		t.Fatalf("later row was hidden: %+v", rows[2])
	}
}

func TestReaderRejectsNonObjectRows(t *testing.T) {
	rows, err := ReadInstructions("queue.jsonl", strings.NewReader(`[1,2,3]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ParseError == nil || rows[0].ParseError.Code != "invalid_row" {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestInstructionContractMatchesCanonicalV1(t *testing.T) {
	fixture, err := os.ReadFile("testdata/valid.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ReadInstructions("testdata/valid.jsonl", bytes.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if diags := DefaultContract().Validate(row); len(diags) != 0 {
			t.Fatalf("row=%+v diagnostics=%+v", row, diags)
		}
		seen[row.Instruction.Target] = true
	}
	for _, target := range []string{"sh", "herdr", "codex", "claude"} {
		if !seen[target] {
			t.Fatalf("missing target %s", target)
		}
	}
}

func TestInstructionContractRejectsDriftPayloadAndDuplicateIDs(t *testing.T) {
	bad := strings.Replace(validInstruction, `"version":"instruction.v1"`, `"version":"v1","hidden":true`, 1)
	rows, _ := ReadInstructions("queue.jsonl", strings.NewReader(bad+"\n"+validInstruction+"\n"+validInstruction))
	validation := DefaultContract().ValidateRows(rows)
	codes := diagnosticCodes(validation[0])
	if !codes["unknown_version"] || !codes["unknown_field"] || !diagnosticCodes(validation[2])["duplicate_id"] {
		t.Fatalf("validation=%+v", validation)
	}
	for _, raw := range []string{
		`{"id":"i","version":"instruction.v1","op":"run","target":"sh","payload":{"command":"not-an-argv"},"created_at":"2026-07-10T00:00:00Z"}`,
		`{"id":"i","version":"instruction.v1","op":"run","target":"codex","payload":{"prompt":"","env":{"X":"1"}},"created_at":"2026-07-10T00:00:00Z"}`,
		`{"id":"i","version":"instruction.v1","op":"other","target":"sh","payload":{"argv":["true"]},"created_at":"2026-07-10T00:00:00+09:00"}`,
	} {
		candidate, _ := ReadInstructions("q", strings.NewReader(raw))
		if len(DefaultContract().Validate(candidate[0])) == 0 {
			t.Fatalf("expected rejection: %s", raw)
		}
	}
}

func TestDryRunIsMachineReadableAndContinuesAfterInvalidRow(t *testing.T) {
	rows, _ := ReadInstructions("queue.jsonl", strings.NewReader(`{bad`+"\n"+validInstruction))
	var out bytes.Buffer
	blocked, err := NewEngine(t.TempDir()).DryRun(rows, &out)
	if err != nil || blocked != 1 || len(strings.Split(strings.TrimSpace(out.String()), "\n")) != 2 {
		t.Fatalf("blocked=%d err=%v output=%q", blocked, err, out.String())
	}
	var plans []PlanRow
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var plan PlanRow
		if err := json.Unmarshal([]byte(line), &plan); err != nil {
			t.Fatal(err)
		}
		plans = append(plans, plan)
	}
	if plans[0].Decision != PlanBlocked || plans[1].Decision != PlanAccepted || !plans[1].Policy.Allowed {
		t.Fatalf("plans=%+v", plans)
	}
}

func TestPolicyBoundsWorkspaceTimeoutEnvironmentAndHooks(t *testing.T) {
	rows, _ := ReadInstructions("queue.jsonl", strings.NewReader(validInstruction))
	root := t.TempDir()
	policy := DefaultPolicy(root)
	if got := policy.Evaluate(rows[0].Instruction); !got.Allowed || got.TimeoutSeconds != 600 {
		t.Fatalf("default=%+v", got)
	}
	outside := strings.Replace(validInstruction, `"cwd":"work"`, `"cwd":"../outside"`, 1)
	outsideRows, _ := ReadInstructions("q", strings.NewReader(outside))
	if got := policy.Evaluate(outsideRows[0].Instruction); got.Code != "cwd_outside_workspace" {
		t.Fatalf("cwd=%+v", got)
	}
	policy.DefaultTimeout = 2 * time.Hour
	if got := policy.Evaluate(rows[0].Instruction); got.Code != "timeout_exceeds_max" {
		t.Fatalf("timeout=%+v", got)
	}
	policy = DefaultPolicy(root)
	policy.Environment["SECRET_TOKEN"] = "redacted"
	if got := policy.Evaluate(rows[0].Instruction); got.Code != "env_not_allowed" {
		t.Fatalf("env=%+v", got)
	}
	policy = DefaultPolicy(root)
	policy.DeniedTargets["sh"] = struct{}{}
	if got := policy.Evaluate(rows[0].Instruction); got.Code != "target_denied" {
		t.Fatalf("target=%+v", got)
	}
}

func TestEventLogAppendsTypedValidationAndResultRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log := NewEventLog(path)
	at := time.Date(2026, 7, 10, 1, 2, 3, 0, time.UTC)
	retryable := false
	validation := ValidationRow{Version: ValidationVersionV1, SourceLine: 1, Status: StatusBlocked, Error: ResultError{Code: "malformed_json", Message: "bad row", Retryable: &retryable}}
	result := ResultRow{EventID: "evt-1", Version: ResultVersionV1, RunID: "r", InstructionID: "i", Target: "sh", Kind: ResultAccepted, Seq: 0, RecordedAt: at}
	for _, entry := range []LogEntry{ValidationEntry(validation), ResultEntry(result)} {
		if err := log.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadEventLog(bytes.NewReader(b))
	if err != nil || len(loaded.Validations) != 1 || len(loaded.Results) != 1 {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}

func TestEventLogRejectsUnknownFieldsAndVersions(t *testing.T) {
	for _, raw := range []string{
		`{"version":"result.v1","event_id":"e","run_id":"r","instruction_id":"i","target":"sh","kind":"accepted","seq":0,"recorded_at":"2026-07-10T00:00:00Z","hidden":true}`,
		`{"version":"mystery.v1"}`,
	} {
		if _, err := LoadEventLog(strings.NewReader(raw)); err == nil {
			t.Fatalf("expected rejection: %s", raw)
		}
	}
}

func TestProjectionCoversAllSevenStatuses(t *testing.T) {
	at := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	mk := func(run, id, kind string, seq int) ResultRow {
		row := ResultRow{EventID: run + "-" + kind, Version: ResultVersionV1, RunID: run, InstructionID: id, Target: "sh", Kind: kind, Seq: seq, RecordedAt: at.Add(time.Duration(seq) * time.Second)}
		if kind == ResultCompleted {
			row.Final = &FinalResult{Path: "out/" + run + ".txt"}
		}
		if kind == ResultFailed || kind == ResultBlocked || kind == ResultTimeout || kind == ResultCancelled {
			retryable := true
			row.Error = &ResultError{Code: kind, Message: kind, Retryable: &retryable}
		}
		return row
	}
	runs := [][]ResultRow{
		{mk("queued", "i-queued", ResultAccepted, 0)},
		{mk("running", "i-running", ResultAccepted, 0), mk("running", "i-running", ResultStarted, 1)},
		{mk("completed", "i-completed", ResultAccepted, 0), mk("completed", "i-completed", ResultStarted, 1), mk("completed", "i-completed", ResultCompleted, 2)},
		{mk("failed", "i-failed", ResultAccepted, 0), mk("failed", "i-failed", ResultStarted, 1), mk("failed", "i-failed", ResultFailed, 2)},
		{mk("blocked", "i-blocked", ResultAccepted, 0), mk("blocked", "i-blocked", ResultBlocked, 1)},
		{mk("timeout", "i-timeout", ResultAccepted, 0), mk("timeout", "i-timeout", ResultStarted, 1), mk("timeout", "i-timeout", ResultTimeout, 2)},
		{mk("cancelled", "i-cancelled", ResultAccepted, 0), mk("cancelled", "i-cancelled", ResultCancelled, 1)},
	}
	var results []ResultRow
	var instructions []Instruction
	for _, run := range runs {
		results = append(results, run...)
		instructions = append(instructions, Instruction{ID: run[0].InstructionID, Target: "sh", Payload: json.RawMessage(`{"argv":["true"],"cwd":"."}`)})
	}
	projection, diagnostics := Project(instructions, results)
	if len(diagnostics) != 0 || len(projection) != 7 {
		t.Fatalf("projection=%+v diagnostics=%+v", projection, diagnostics)
	}
	seen := map[string]bool{}
	for _, row := range projection {
		seen[row.Status] = true
	}
	for _, status := range []string{StatusQueued, StatusRunning, StatusCompleted, StatusFailed, StatusBlocked, StatusTimeout, StatusCancelled} {
		if !seen[status] {
			t.Fatalf("missing status %s", status)
		}
	}
}

func TestNormalModeValidationIdempotencyReplayAndResume(t *testing.T) {
	rows, _ := ReadInstructions("queue.jsonl", strings.NewReader(validInstruction))
	at := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	engine := NewEngine(t.TempDir())
	engine.Now = func() time.Time { return at }
	entries := engine.EvaluateNormal(rows, LogData{}, false)
	if len(entries) != 2 || entries[0].Result.Kind != ResultAccepted || entries[1].Result.Kind != ResultBlocked || entries[1].Result.Error.Code != "adapter_unavailable" {
		t.Fatalf("entries=%+v", entries)
	}
	started := LogData{Results: []ResultRow{
		{EventID: "e0", Version: ResultVersionV1, RunID: "old", InstructionID: "ins-sh-001", Target: "sh", Kind: ResultAccepted, Seq: 0, RecordedAt: at.Add(-2 * time.Second)},
		{EventID: "e1", Version: ResultVersionV1, RunID: "old", InstructionID: "ins-sh-001", Target: "sh", Kind: ResultStarted, Seq: 1, RecordedAt: at.Add(-time.Second)},
	}}
	skipped := engine.EvaluateNormal(rows, started, false)
	if len(skipped) != 1 || skipped[0].Validation == nil || skipped[0].Validation.Error.Code != "duplicate_id" {
		t.Fatalf("skipped=%+v", skipped)
	}
	replayed := engine.EvaluateNormal(rows, started, true)
	if len(replayed) != 2 || replayed[0].Result.RunID == "old" {
		t.Fatalf("replayed=%+v", replayed)
	}
	queued := LogData{Results: []ResultRow{{EventID: "q0", Version: ResultVersionV1, RunID: "queued-old", InstructionID: "ins-sh-001", Target: "sh", Kind: ResultAccepted, Seq: 0, RecordedAt: at.Add(-time.Second)}}}
	resumed := engine.EvaluateNormal(rows, queued, false)
	if len(resumed) != 1 || resumed[0].Result.RunID != "queued-old" || resumed[0].Result.Seq != 1 || resumed[0].Result.Kind != ResultBlocked {
		t.Fatalf("resumed=%+v", resumed)
	}
}

func TestInvalidRowsProduceValidationWithoutRun(t *testing.T) {
	rows, _ := ReadInstructions("queue.jsonl", strings.NewReader(`{bad`+"\n"+validInstruction+"\n"+validInstruction))
	entries := NewEngine(t.TempDir()).EvaluateNormal(rows, LogData{}, false)
	validations, results := 0, 0
	for _, entry := range entries {
		if entry.Validation != nil {
			validations++
		}
		if entry.Result != nil {
			results++
		}
	}
	if validations != 2 || results != 2 || entries[0].Validation.Error.Code != "malformed_json" || entries[len(entries)-1].Validation.Error.Code != "duplicate_id" {
		t.Fatalf("entries=%+v", entries)
	}
}

func TestWorkerCoreDoesNotImportCompilerOrCommandPackages(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			if strings.Contains(path, "/internal/hq") || strings.Contains(path, "/cmd/hq") {
				t.Fatalf("%s imports forbidden compiler package %s", file, path)
			}
		}
	}
}

func TestProjectionOrderingAndContractFixtures(t *testing.T) {
	root := filepath.Join("..", "..", "spec", "fixtures")
	exampleFile, err := os.Open(filepath.Join(root, "instruction.examples.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer exampleFile.Close()
	type example struct {
		Row Instruction `json:"row"`
	}
	var instructions []Instruction
	scanner := bufio.NewScanner(exampleFile)
	line := 0
	for scanner.Scan() {
		line++
		var fixture example
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatal(err)
		}
		row := ReadRow{Source: SourceRef{Path: "instruction.examples.jsonl", Line: line, Raw: string(scanner.Bytes())}, Instruction: fixture.Row}
		if diagnostics := DefaultContract().Validate(row); len(diagnostics) != 0 {
			t.Fatalf("line=%d diagnostics=%+v", line, diagnostics)
		}
		instructions = append(instructions, fixture.Row)
	}
	resultFile, err := os.Open(filepath.Join(root, "result.runs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer resultFile.Close()
	var results []ResultRow
	scanner = bufio.NewScanner(resultFile)
	for scanner.Scan() {
		var row ResultRow
		if err := decodeStrict(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if err := row.Validate(); err != nil {
			t.Fatal(err)
		}
		results = append(results, row)
	}
	projected, diagnostics := Project(instructions, results)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
	type expectedSession struct {
		Version         string    `json:"version"`
		RunID           string    `json:"run_id"`
		InstructionID   string    `json:"instruction_id"`
		Target          string    `json:"target"`
		Status          string    `json:"status"`
		CWD             string    `json:"cwd"`
		StartedAt       time.Time `json:"started_at"`
		LastEventAt     time.Time `json:"last_event_at"`
		NativeSessionID string    `json:"native_session_id,omitempty"`
		FinalPath       string    `json:"final_path,omitempty"`
	}
	expectedFile, err := os.Open(filepath.Join(root, "session.index.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer expectedFile.Close()
	var expected []expectedSession
	scanner = bufio.NewScanner(expectedFile)
	for scanner.Scan() {
		var row expectedSession
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		expected = append(expected, row)
	}
	if len(projected) != len(expected) {
		t.Fatalf("projected=%d expected=%d", len(projected), len(expected))
	}
	byRun := map[string]RunProjection{}
	for _, row := range projected {
		byRun[row.RunID] = row
	}
	for _, want := range expected {
		got := byRun[want.RunID]
		if got.Version != want.Version || got.InstructionID != want.InstructionID || got.Target != want.Target || got.Status != want.Status || got.CWD != want.CWD || !got.StartedAt.Equal(want.StartedAt) || !got.LastEventAt.Equal(want.LastEventAt) || got.NativeSessionID != want.NativeSessionID || got.FinalPath != want.FinalPath {
			t.Fatalf("run=%s got=%+v want=%+v", want.RunID, got, want)
		}
	}
	ids := make([]string, 0, len(projected))
	for _, row := range projected {
		ids = append(ids, row.RunID)
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("run order=%v", ids)
	}
}

func diagnosticCodes(diagnostics []Diagnostic) map[string]bool {
	out := map[string]bool{}
	for _, diagnostic := range diagnostics {
		out[diagnostic.Code] = true
	}
	return out
}

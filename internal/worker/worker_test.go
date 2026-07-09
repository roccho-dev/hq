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

func TestInstructionContractRejectsDriftAndDuplicateIDs(t *testing.T) {
	bad := strings.Replace(validInstruction, `"version":"instruction.v1"`, `"version":"v1","hidden":true`, 1)
	rows, _ := ReadInstructions("queue.jsonl", strings.NewReader(bad+"\n"+validInstruction+"\n"+validInstruction))
	validation := DefaultContract().ValidateRows(rows)
	codes0 := diagnosticCodes(validation[0])
	if !codes0["unknown_version"] || !codes0["unknown_field"] {
		t.Fatalf("first diagnostics=%+v", validation[0])
	}
	if !diagnosticCodes(validation[2])["duplicate_id"] {
		t.Fatalf("duplicate diagnostics=%+v", validation[2])
	}
}

func TestTargetPayloadsFailClosed(t *testing.T) {
	cases := []string{
		`{"id":"i","version":"instruction.v1","op":"run","target":"sh","payload":{"command":"rm -rf /"},"created_at":"2026-07-10T00:00:00Z"}`,
		`{"id":"i","version":"instruction.v1","op":"run","target":"codex","payload":{"prompt":"","env":{"X":"1"}},"created_at":"2026-07-10T00:00:00Z"}`,
		`{"id":"i","version":"instruction.v1","op":"other","target":"sh","payload":{"argv":["true"]},"created_at":"2026-07-10T00:00:00+09:00"}`,
	}
	for _, raw := range cases {
		rows, _ := ReadInstructions("q", strings.NewReader(raw))
		if len(DefaultContract().Validate(rows[0])) == 0 {
			t.Fatalf("expected rejection: %s", raw)
		}
	}
}

func TestDryRunIsMachineReadableAndSideEffectFree(t *testing.T) {
	rows, _ := ReadInstructions("queue.jsonl", strings.NewReader(validInstruction))
	engine := NewEngine(t.TempDir())
	var out bytes.Buffer
	blocked, err := engine.DryRun(rows, &out)
	if err != nil || blocked != 0 {
		t.Fatalf("blocked=%d err=%v output=%s", blocked, err, out.String())
	}
	var plan PlanRow
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Version != PlanVersionV1 || plan.Decision != DecisionAccepted || !plan.Policy.Allowed || plan.ExpectedAdapter != "sh" {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestDryRunReportsInvalidRowsAndContinues(t *testing.T) {
	input := `{bad` + "\n" + validInstruction
	rows, _ := ReadInstructions("queue.jsonl", strings.NewReader(input))
	var out bytes.Buffer
	blocked, err := NewEngine(t.TempDir()).DryRun(rows, &out)
	if err != nil || blocked != 1 || len(strings.Split(strings.TrimSpace(out.String()), "\n")) != 2 {
		t.Fatalf("blocked=%d err=%v output=%q", blocked, err, out.String())
	}
}

func TestPolicyBoundsWorkspaceTimeoutEnvironmentAndHooks(t *testing.T) {
	rows, _ := ReadInstructions("queue.jsonl", strings.NewReader(validInstruction))
	root := t.TempDir()
	policy := DefaultPolicy(root)
	if got := policy.Evaluate(rows[0].Instruction); !got.Allowed || got.TimeoutSeconds != 600 {
		t.Fatalf("default decision=%+v", got)
	}
	outside := strings.Replace(validInstruction, `"cwd":"work"`, `"cwd":"../outside"`, 1)
	outsideRows, _ := ReadInstructions("q", strings.NewReader(outside))
	if got := policy.Evaluate(outsideRows[0].Instruction); got.Code != "cwd_outside_workspace" {
		t.Fatalf("cwd decision=%+v", got)
	}
	policy.DefaultTimeout = 2 * time.Hour
	if got := policy.Evaluate(rows[0].Instruction); got.Code != "timeout_exceeds_max" {
		t.Fatalf("timeout decision=%+v", got)
	}
	policy = DefaultPolicy(root)
	policy.Environment["SECRET_TOKEN"] = "redacted"
	if got := policy.Evaluate(rows[0].Instruction); got.Code != "env_not_allowed" {
		t.Fatalf("env decision=%+v", got)
	}
	policy = DefaultPolicy(root)
	policy.DeniedTargets["sh"] = struct{}{}
	if got := policy.Evaluate(rows[0].Instruction); got.Code != "target_denied" {
		t.Fatalf("target decision=%+v", got)
	}
}

func TestEventLogAppendsTypedDecisionAndResultRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log := NewEventLog(path)
	at := time.Date(2026, 7, 10, 1, 2, 3, 0, time.UTC)
	decision := DecisionRow{DecisionID: "decision-1", Version: DecisionVersionV1, Decision: DecisionAccepted, RecordedAt: at, Source: SourceRef{Path: "q", Line: 1, Raw: validInstruction}, InstructionID: "i", Target: "sh", Code: "allowed", Message: "allowed"}
	result := ResultRow{EventID: "evt-1", Version: ResultVersionV1, RunID: "r", InstructionID: "i", Target: "sh", Kind: ResultAccepted, Seq: 0, RecordedAt: at}
	for _, entry := range []LogEntry{DecisionEntry(decision), ResultEntry(result)} {
		if err := log.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(b, []byte("\n")) != 2 {
		t.Fatalf("rows=%q", b)
	}
	loaded, err := LoadEventLog(bytes.NewReader(b))
	if err != nil || len(loaded.Decisions) != 1 || len(loaded.Results) != 1 {
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
	type scenario struct {
		status string
		rows   []ResultRow
	}
	mk := func(run, id, kind string, seq int) ResultRow {
		row := ResultRow{EventID: run + "-" + kind, Version: ResultVersionV1, RunID: run, InstructionID: id, Target: "sh", Kind: kind, Seq: seq, RecordedAt: at.Add(time.Duration(seq) * time.Second)}
		if kind == ResultCompleted {
			row.Final = &FinalResult{Path: "out/" + run + ".txt"}
		}
		if kind == ResultFailed || kind == ResultBlocked || kind == ResultTimeout || kind == ResultCancelled {
			retry := true
			row.Error = &ResultError{Code: kind, Message: kind, Retryable: &retry}
		}
		return row
	}
	scenarios := []scenario{
		{StatusQueued, []ResultRow{mk("queued", "i-queued", ResultAccepted, 0)}},
		{StatusRunning, []ResultRow{mk("running", "i-running", ResultAccepted, 0), mk("running", "i-running", ResultStarted, 1)}},
		{StatusCompleted, []ResultRow{mk("completed", "i-completed", ResultAccepted, 0), mk("completed", "i-completed", ResultStarted, 1), mk("completed", "i-completed", ResultCompleted, 2)}},
		{StatusFailed, []ResultRow{mk("failed", "i-failed", ResultAccepted, 0), mk("failed", "i-failed", ResultStarted, 1), mk("failed", "i-failed", ResultFailed, 2)}},
		{StatusBlocked, []ResultRow{mk("blocked", "i-blocked", ResultAccepted, 0), mk("blocked", "i-blocked", ResultBlocked, 1)}},
		{StatusTimeout, []ResultRow{mk("timeout", "i-timeout", ResultAccepted, 0), mk("timeout", "i-timeout", ResultStarted, 1), mk("timeout", "i-timeout", ResultTimeout, 2)}},
		{StatusCancelled, []ResultRow{mk("cancelled", "i-cancelled", ResultAccepted, 0), mk("cancelled", "i-cancelled", ResultCancelled, 1)}},
	}
	var results []ResultRow
	var instructions []Instruction
	for _, scenario := range scenarios {
		results = append(results, scenario.rows...)
		id := scenario.rows[0].InstructionID
		instructions = append(instructions, Instruction{ID: id, Target: "sh", Payload: json.RawMessage(`{"argv":["true"],"cwd":"."}`)})
	}
	projection, diags := Project(instructions, results)
	if len(diags) != 0 || len(projection) != len(scenarios) {
		t.Fatalf("projection=%+v diagnostics=%+v", projection, diags)
	}
	got := map[string]bool{}
	for _, row := range projection {
		got[row.Status] = true
	}
	for _, scenario := range scenarios {
		if !got[scenario.status] {
			t.Fatalf("missing status %s", scenario.status)
		}
	}
}

func TestProjectionRejectsGapsTransitionsAndSessionDrift(t *testing.T) {
	at := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	a, b := "a", "b"
	rows := []ResultRow{
		{EventID: "e0", Version: ResultVersionV1, RunID: "r", InstructionID: "i", Target: "sh", Kind: ResultAccepted, Seq: 0, RecordedAt: at},
		{EventID: "e2", Version: ResultVersionV1, RunID: "r", InstructionID: "i", Target: "sh", Kind: ResultStarted, Seq: 2, RecordedAt: at.Add(time.Second), NativeSessionID: &a},
		{EventID: "e3", Version: ResultVersionV1, RunID: "r", InstructionID: "i", Target: "sh", Kind: ResultStdout, Seq: 3, RecordedAt: at.Add(2 * time.Second), Message: ptr("x"), NativeSessionID: &b},
	}
	if projection, diags := Project(nil, rows); len(projection) != 0 || len(diags) == 0 {
		t.Fatalf("projection=%+v diagnostics=%+v", projection, diags)
	}
}

func TestNormalModeWritesDurableBlockedRunWithoutStartingAdapter(t *testing.T) {
	rows, _ := ReadInstructions("queue.jsonl", strings.NewReader(validInstruction))
	engine := NewEngine(t.TempDir())
	engine.Now = func() time.Time { return time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC) }
	entries := engine.EvaluateNormal(rows, LogData{}, false)
	if len(entries) != 3 || entries[0].Decision == nil || entries[1].Result.Kind != ResultAccepted || entries[2].Result.Kind != ResultBlocked || entries[2].Result.Error.Code != "adapter_unavailable" {
		t.Fatalf("entries=%+v", entries)
	}
	projection, diags := Project([]Instruction{rows[0].Instruction}, []ResultRow{*entries[1].Result, *entries[2].Result})
	if len(diags) != 0 || len(projection) != 1 || projection[0].Status != StatusBlocked {
		t.Fatalf("projection=%+v diagnostics=%+v", projection, diags)
	}
}

func TestInvalidAndDuplicateRowsProduceDecisionEvidenceOnly(t *testing.T) {
	input := `{bad` + "\n" + validInstruction + "\n" + validInstruction
	rows, _ := ReadInstructions("queue.jsonl", strings.NewReader(input))
	engine := NewEngine(t.TempDir())
	engine.Now = func() time.Time { return time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC) }
	entries := engine.EvaluateNormal(rows, LogData{}, false)
	decisions := 0
	results := 0
	for _, entry := range entries {
		if entry.Decision != nil {
			decisions++
		}
		if entry.Result != nil {
			results++
		}
	}
	if decisions != 3 || results != 2 || entries[0].Decision.Source.Raw != `{bad` || entries[len(entries)-1].Decision.Code != "duplicate_id" {
		t.Fatalf("entries=%+v", entries)
	}
}

func TestIdempotencySkipsStartedRunAndReplayCreatesNewRun(t *testing.T) {
	rows, _ := ReadInstructions("queue.jsonl", strings.NewReader(validInstruction))
	at := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	prior := LogData{Results: []ResultRow{
		{EventID: "e0", Version: ResultVersionV1, RunID: "old", InstructionID: "ins-sh-001", Target: "sh", Kind: ResultAccepted, Seq: 0, RecordedAt: at.Add(-2 * time.Second)},
		{EventID: "e1", Version: ResultVersionV1, RunID: "old", InstructionID: "ins-sh-001", Target: "sh", Kind: ResultStarted, Seq: 1, RecordedAt: at.Add(-time.Second)},
	}}
	engine := NewEngine(t.TempDir())
	engine.Now = func() time.Time { return at }
	skipped := engine.EvaluateNormal(rows, prior, false)
	if len(skipped) != 1 || skipped[0].Decision == nil || skipped[0].Decision.Decision != DecisionDuplicate {
		t.Fatalf("skipped=%+v", skipped)
	}
	replayed := engine.EvaluateNormal(rows, prior, true)
	if len(replayed) != 3 || replayed[1].Result.RunID == "old" {
		t.Fatalf("replayed=%+v", replayed)
	}
}

func TestRestartResumesQueuedRunWithoutSecondAcceptedEvent(t *testing.T) {
	rows, _ := ReadInstructions("queue.jsonl", strings.NewReader(validInstruction))
	at := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	prior := LogData{Results: []ResultRow{{EventID: "e0", Version: ResultVersionV1, RunID: "old", InstructionID: "ins-sh-001", Target: "sh", Kind: ResultAccepted, Seq: 0, RecordedAt: at.Add(-time.Second)}}}
	engine := NewEngine(t.TempDir())
	engine.Now = func() time.Time { return at }
	entries := engine.EvaluateNormal(rows, prior, false)
	if len(entries) != 2 || entries[1].Result == nil || entries[1].Result.RunID != "old" || entries[1].Result.Seq != 1 || entries[1].Result.Kind != ResultBlocked {
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

func diagnosticCodes(diags []Diagnostic) map[string]bool {
	out := map[string]bool{}
	for _, d := range diags {
		out[d.Code] = true
	}
	return out
}

func ptr(value string) *string { return &value }

func TestProjectionOrderingIsDeterministic(t *testing.T) {
	at := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	rows := []ResultRow{
		{EventID: "b", Version: ResultVersionV1, RunID: "b", InstructionID: "ib", Target: "sh", Kind: ResultAccepted, Seq: 0, RecordedAt: at},
		{EventID: "a", Version: ResultVersionV1, RunID: "a", InstructionID: "ia", Target: "sh", Kind: ResultAccepted, Seq: 0, RecordedAt: at},
	}
	projection, diags := Project(nil, rows)
	if len(diags) != 0 {
		t.Fatal(diags)
	}
	ids := []string{projection[0].RunID, projection[1].RunID}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("ids=%v", ids)
	}
}

func TestGoWorkerConsumesContractLaneFixturesExactly(t *testing.T) {
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
		if diags := DefaultContract().Validate(row); len(diags) != 0 {
			t.Fatalf("line=%d diagnostics=%+v", line, diags)
		}
		instructions = append(instructions, fixture.Row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
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
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}

	projected, diags := Project(instructions, results)
	if len(diags) != 0 {
		t.Fatalf("projection diagnostics=%+v", diags)
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
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(projected) != len(expected) {
		t.Fatalf("projected=%d expected=%d", len(projected), len(expected))
	}
	byRun := map[string]RunProjection{}
	for _, row := range projected {
		byRun[row.RunID] = row
	}
	for _, want := range expected {
		got, ok := byRun[want.RunID]
		if !ok || got.Version != want.Version || got.InstructionID != want.InstructionID || got.Target != want.Target || got.Status != want.Status || got.CWD != want.CWD || !got.StartedAt.Equal(want.StartedAt) || !got.LastEventAt.Equal(want.LastEventAt) || got.NativeSessionID != want.NativeSessionID || got.FinalPath != want.FinalPath {
			t.Fatalf("run=%s got=%+v want=%+v", want.RunID, got, want)
		}
	}
}

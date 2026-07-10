package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hq/internal/worker/adapter"
	"hq/internal/workersafety"
)

type runnerFakeAdapter struct {
	calls       int
	seenPayload string
}

func (f *runnerFakeAdapter) Run(_ context.Context, request adapter.Request, emit adapter.Emit) (adapter.Completion, error) {
	f.calls++
	f.seenPayload = string(request.Payload)
	if err := emit(adapter.Output{Kind: adapter.OutputStdout, Message: "token=sk-" + strings.Repeat("a", 24)}); err != nil {
		return adapter.Completion{}, err
	}
	return adapter.Completion{FinalText: "done with ghp_" + strings.Repeat("b", 32)}, nil
}

type memoryAppender struct{ entries []LogEntry }

func (m *memoryAppender) Append(entry LogEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}

func approvedRunnerFixture(t *testing.T, instruction Instruction) ApprovalStore {
	t.Helper()
	digest, err := InstructionDigest(instruction)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(ApprovalRecord{
		Version: ApprovalVersionV1, InstructionID: instruction.ID, Approved: true,
		ApprovedBy: "owner", InstructionDigest: digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := LoadApprovals(bytes.NewReader(append(encoded, '\n')))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func runnerRow(t *testing.T, secret string) ReadRow {
	t.Helper()
	raw := `{"id":"ins-runner-001","version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["printf","` + secret + `"],"cwd":"."},"created_at":"2026-07-10T06:00:00Z"}`
	rows, err := ReadInstructions("queue.jsonl", strings.NewReader(raw))
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	return rows[0]
}

func TestRunnerComposesApprovalDispatchRedactionAndProjection(t *testing.T) {
	secret := "sk-" + strings.Repeat("z", 24)
	row := runnerRow(t, secret)
	fake := &runnerFakeAdapter{}
	registry, err := adapter.NewRegistry(adapter.Registration{Target: "sh", Adapter: fake})
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(t.TempDir(), registry, approvedRunnerFixture(t, row.Instruction))
	at := time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC)
	runner.Now = func() time.Time { return at }
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log := NewEventLog(path)
	emitted, unsuccessful, err := runner.Process(context.Background(), []ReadRow{row}, LogData{}, false, log)
	if err != nil {
		t.Fatal(err)
	}
	if unsuccessful != 0 || fake.calls != 1 {
		t.Fatalf("unsuccessful=%d calls=%d", unsuccessful, fake.calls)
	}
	if !strings.Contains(fake.seenPayload, secret) {
		t.Fatalf("execution payload was redacted before dispatch: %s", fake.seenPayload)
	}
	if len(emitted) != 5 || emitted[0].Policy == nil || emitted[1].Result.Kind != ResultAccepted || emitted[2].Result.Kind != ResultStarted || emitted[3].Result.Kind != ResultStdout || emitted[4].Result.Kind != ResultCompleted {
		t.Fatalf("unexpected lifecycle: %+v", emitted)
	}
	durable, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(durable)
	for _, forbidden := range []string{secret, "sk-" + strings.Repeat("a", 24), "ghp_" + strings.Repeat("b", 32)} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("secret survived durable append: %q in %s", forbidden, text)
		}
	}
	loaded, err := LoadEventFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Policies) != 1 || len(loaded.Results) != 4 || loaded.Results[len(loaded.Results)-1].Kind != ResultCompleted {
		t.Fatalf("loaded=%+v", loaded)
	}
	projection, diagnostics := Project([]Instruction{row.Instruction}, loaded.Results)
	if len(diagnostics) != 0 || len(projection) != 1 || projection[0].Status != StatusCompleted {
		t.Fatalf("projection=%+v diagnostics=%+v", projection, diagnostics)
	}
}

func TestRunnerMissingOrStaleApprovalNeverReachesAdapter(t *testing.T) {
	row := runnerRow(t, "plain")
	for _, test := range []struct {
		name  string
		store ApprovalStore
		code  string
	}{
		{name: "missing", store: EmptyApprovalStore(), code: "approval_required"},
		{name: "stale", store: func() ApprovalStore {
			record := `{"version":"worker.approval.v1","instruction_id":"ins-runner-001","approved":true,"approved_by":"owner","instruction_digest":"sha256:` + strings.Repeat("0", 64) + `"}`
			store, err := LoadApprovals(strings.NewReader(record))
			if err != nil {
				t.Fatal(err)
			}
			return store
		}(), code: "approval_digest_mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &runnerFakeAdapter{}
			registry, err := adapter.NewRegistry(adapter.Registration{Target: "sh", Adapter: fake})
			if err != nil {
				t.Fatal(err)
			}
			runner := NewRunner(t.TempDir(), registry, test.store)
			sink := &memoryAppender{}
			emitted, unsuccessful, err := runner.Process(context.Background(), []ReadRow{row}, LogData{}, false, sink)
			if err != nil {
				t.Fatal(err)
			}
			if fake.calls != 0 || unsuccessful != 1 {
				t.Fatalf("calls=%d unsuccessful=%d", fake.calls, unsuccessful)
			}
			if len(emitted) != 3 || emitted[0].Policy == nil || emitted[2].Result == nil || emitted[2].Result.Error.Code != test.code {
				t.Fatalf("emitted=%+v", emitted)
			}
		})
	}
}

func TestEventLogDirectAppendCannotBypassRedaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log := NewEventLog(path)
	message := "Bearer " + strings.Repeat("x", 24)
	row := ResultRow{
		EventID: "evt-direct-0", Version: ResultVersionV1, RunID: "run-direct", InstructionID: "ins-direct",
		Target: "sh", Kind: ResultStdout, Seq: 0, RecordedAt: time.Now().UTC(), Message: &message,
	}
	if err := log.Append(ResultEntry(row)); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), strings.Repeat("x", 24)) {
		t.Fatalf("direct append leaked secret: %s", content)
	}
}

func TestRunnerComposesWorkspaceBoundsIntoFinalMayDispatch(t *testing.T) {
	raw := `{"id":"ins-outside-001","version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["true"],"cwd":"../outside"},"created_at":"2026-07-10T06:00:00Z"}`
	rows, err := ReadInstructions("queue.jsonl", strings.NewReader(raw))
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	fake := &runnerFakeAdapter{}
	registry, err := adapter.NewRegistry(adapter.Registration{Target: "sh", Adapter: fake})
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(t.TempDir(), registry, approvedRunnerFixture(t, rows[0].Instruction))
	sink := &memoryAppender{}
	emitted, unsuccessful, err := runner.Process(context.Background(), rows, LogData{}, false, sink)
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 0 || unsuccessful != 1 {
		t.Fatalf("calls=%d unsuccessful=%d", fake.calls, unsuccessful)
	}
	if len(emitted) != 3 || emitted[0].Policy == nil {
		t.Fatalf("emitted=%+v", emitted)
	}
	if emitted[0].Policy.MayDispatch || emitted[0].Policy.Status != workersafety.PolicyBlocked || emitted[0].Policy.Reason != "cwd_outside_workspace" {
		t.Fatalf("final policy was not composed: %+v", emitted[0].Policy)
	}
	if emitted[2].Result == nil || emitted[2].Result.Error.Code != "cwd_outside_workspace" {
		t.Fatalf("result=%+v", emitted[2])
	}
}

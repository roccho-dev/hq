package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"hq/internal/worker/resultv1"
)

type fakeAdapter struct{ calls *int }

func (f fakeAdapter) Run(_ context.Context, _ Request, emit Emit) (Completion, error) {
	(*f.calls)++
	if err := emit(Output{Kind: OutputStdout, Message: "output"}); err != nil {
		return Completion{}, err
	}
	return Completion{FinalText: "done"}, nil
}

func TestRegistryFailsClosedAndSnapshotIsDeterministic(t *testing.T) {
	calls := 0
	registry, err := NewRegistry(
		Registration{Target: "sh", Adapter: fakeAdapter{calls: &calls}},
		Registration{Target: "herdr", Adapter: fakeAdapter{calls: &calls}},
		Registration{Target: "codex", Adapter: fakeAdapter{calls: &calls}},
		Registration{Target: "claude", Adapter: fakeAdapter{calls: &calls}},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"sh", "herdr", "codex", "claude"} {
		if _, err := registry.Resolve(target); err != nil {
			t.Fatalf("resolve %s: %v", target, err)
		}
	}
	for _, target := range []string{"shell", "SH", " sh", "unknown"} {
		if _, err := registry.Resolve(target); err == nil {
			t.Fatalf("target %q unexpectedly resolved", target)
		}
	}
	empty, err := NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := empty.Resolve("sh"); err == nil || !strings.Contains(err.Error(), "no adapter registered") {
		t.Fatalf("expected adapter unavailable, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("Resolve executed an adapter; calls=%d", calls)
	}
	want := RegistrySnapshot{Version: RegistryVersion, Targets: []string{"claude", "codex", "herdr", "sh"}}
	if got := registry.Snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot=%#v want=%#v", got, want)
	}
}

func TestRegistryRejectsDuplicateNilAndNonCanonicalRegistration(t *testing.T) {
	calls := 0
	if _, err := NewRegistry(
		Registration{Target: "sh", Adapter: fakeAdapter{calls: &calls}},
		Registration{Target: "sh", Adapter: fakeAdapter{calls: &calls}},
	); err == nil {
		t.Fatal("duplicate registration must fail")
	}
	if _, err := NewRegistry(Registration{Target: "shell", Adapter: fakeAdapter{calls: &calls}}); err == nil {
		t.Fatal("non-canonical target must fail")
	}
	var nilAdapter *fakeAdapter
	if _, err := NewRegistry(Registration{Target: "sh", Adapter: nilAdapter}); err == nil {
		t.Fatal("typed nil adapter must fail")
	}
}

func TestMapperMatchesCanonicalResultFixtureExactly(t *testing.T) {
	canonical := loadCanonicalRows(t)
	herdrSession := "herdr-session-001"
	codexSession := "codex-session-timeout-001"
	claudeSession := "claude-session-running-001"
	retryable := true

	cases := []struct {
		eventID string
		got     func() (resultv1.Row, error)
	}{
		{"evt-run-sh-001-02", func() (resultv1.Row, error) {
			return ResultForOutput(req("run-sh-001", "ins-sh-001", "sh"), env("evt-run-sh-001-02", 2, "2026-07-10T05:00:02Z"), Output{Kind: OutputStdout, Message: "hello\n"})
		}},
		{"evt-run-sh-001-03", func() (resultv1.Row, error) {
			return ResultForCompletion(req("run-sh-001", "ins-sh-001", "sh"), env("evt-run-sh-001-03", 3, "2026-07-10T05:00:03Z"), Completion{FinalText: "hello", FinalPath: "artifacts/run-sh-001/final.txt"})
		}},
		{"evt-run-herdr-001-02", func() (resultv1.Row, error) {
			return ResultForOutput(req("run-herdr-001", "ins-herdr-001", "herdr"), env("evt-run-herdr-001-02", 2, "2026-07-10T05:01:02Z"), Output{Kind: OutputStderr, Message: "adapter exited with status 1", NativeSessionID: &herdrSession})
		}},
		{"evt-run-herdr-001-03", func() (resultv1.Row, error) {
			return ResultForError(req("run-herdr-001", "ins-herdr-001", "herdr"), env("evt-run-herdr-001-03", 3, "2026-07-10T05:01:03Z"), &herdrSession, &FailureError{Kind: resultv1.KindFailed, Detail: resultv1.Error{Code: "adapter_exit", Message: "adapter exited with status 1", Retryable: &retryable}})
		}},
		{"evt-run-claude-001-01", func() (resultv1.Row, error) {
			return ResultForError(req("run-claude-001", "ins-claude-001", "claude"), env("evt-run-claude-001-01", 1, "2026-07-10T05:02:01Z"), nil, NewBlockedError("policy_blocked", "approval is required"))
		}},
		{"evt-run-claude-running-001-02", func() (resultv1.Row, error) {
			return ResultForOutput(req("run-claude-running-001", "ins-claude-running-001", "claude"), env("evt-run-claude-running-001-02", 2, "2026-07-10T05:06:02Z"), Output{Kind: OutputStdout, Message: "working", NativeSessionID: &claudeSession})
		}},
		{"evt-run-codex-timeout-001-02", func() (resultv1.Row, error) {
			return ResultForError(req("run-codex-timeout-001", "ins-codex-timeout-001", "codex"), env("evt-run-codex-timeout-001-02", 2, "2026-07-10T05:08:01Z"), &codexSession, context.DeadlineExceeded)
		}},
		{"evt-run-codex-001-01", func() (resultv1.Row, error) {
			return ResultForError(req("run-codex-001", "ins-codex-001", "codex"), env("evt-run-codex-001-01", 1, "2026-07-10T05:04:01Z"), nil, &FailureError{Kind: resultv1.KindCancelled, Detail: resultv1.Error{Code: "cancel_requested", Message: "run cancelled before start", Retryable: &retryable}})
		}},
	}

	seen := map[string]bool{}
	for _, test := range cases {
		got, err := test.got()
		if err != nil {
			t.Fatalf("%s: %v", test.eventID, err)
		}
		want := canonical[test.eventID]
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s\n got: %#v\nwant: %#v", test.eventID, got, want)
		}
		seen[got.Target] = true
	}
	for _, target := range []string{"sh", "herdr", "codex", "claude"} {
		if !seen[target] {
			t.Fatalf("no exact canonical mapping proof for %s", target)
		}
	}
}

func TestNoParallelPersistedAdapterEventContract(t *testing.T) {
	paths := []string{"contract.go", "registry.go", "contract_test.go", filepath.Join("..", "..", "..", "docs", "architecture", "worker-adapter-contract.md")}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) && strings.Contains(path, "docs") {
				continue
			}
			t.Fatal(err)
		}
		for _, forbidden := range []string{"adapter." + "event.v1", "type " + "Event struct", "artifact_" + "paths"} {
			if strings.Contains(string(data), forbidden) {
				t.Fatalf("%s contains obsolete persisted contract token %q", path, forbidden)
			}
		}
	}
}

func TestMissingFinalFailsInsteadOfCreatingFalseCompletedRow(t *testing.T) {
	_, err := ResultForCompletion(req("run-1", "ins-1", "sh"), env("evt-1", 1, "2026-07-10T05:00:00Z"), Completion{})
	if err == nil || !strings.Contains(err.Error(), "requires final") {
		t.Fatalf("got %v", err)
	}
}

func TestInvalidStructuredFailureBecomesCanonicalProtocolError(t *testing.T) {
	row, err := ResultForError(req("run-1", "ins-1", "sh"), env("evt-1", 1, "2026-07-10T05:00:00Z"), nil, &FailureError{Kind: resultv1.KindStdout})
	if err != nil {
		t.Fatal(err)
	}
	if row.Kind != resultv1.KindFailed || row.Error == nil || row.Error.Code != "protocol_error" {
		t.Fatalf("row=%#v", row)
	}
}

func TestUnknownTargetDoesNotBecomeResultV1(t *testing.T) {
	_, err := ResultForError(req("run-1", "ins-1", "unknown"), env("evt-1", 1, "2026-07-10T05:00:00Z"), nil, errors.New("boom"))
	if err == nil || !strings.Contains(err.Error(), "not part of instruction/result v1") {
		t.Fatalf("got %v", err)
	}
}

func loadCanonicalRows(t *testing.T) map[string]resultv1.Row {
	t.Helper()
	path := filepath.Join("..", "..", "..", "spec", "fixtures", "result.runs.jsonl")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows := map[string]resultv1.Row{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		row, err := resultv1.DecodeStrict(scanner.Bytes())
		if err != nil {
			t.Fatalf("canonical fixture: %v", err)
		}
		rows[row.EventID] = row
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

func req(runID, instructionID, target string) Request {
	return Request{RunID: runID, InstructionID: instructionID, Target: target, Operation: "run", Payload: json.RawMessage(`{}`)}
}

func env(eventID string, seq int, recordedAt string) Envelope {
	parsed, err := time.Parse(time.RFC3339, recordedAt)
	if err != nil {
		panic(err)
	}
	return Envelope{EventID: eventID, Seq: seq, RecordedAt: parsed}
}

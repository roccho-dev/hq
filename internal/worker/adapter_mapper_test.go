package worker

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

	"hq/internal/worker/adapter"
)

func TestAdapterMapperMatchesCanonicalResultV1FixturesForAllTargets(t *testing.T) {
	path := filepath.Join("..", "..", "spec", "fixtures", "adapter", "result-v1.jsonl")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	expected := map[string]ResultRow{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var row ResultRow
		if err := decodeStrict(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if err := row.Validate(); err != nil {
			t.Fatal(err)
		}
		expected[row.Target] = row
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}

	herdrSession := "herdr-session-001"
	codexSession := "codex-session-timeout-001"
	claudeSession := "claude-session-running-001"
	retryable := true

	cases := map[string]func() (ResultRow, error){
		"sh": func() (ResultRow, error) {
			return ResultForAdapterOutput(
				request("run-sh-001", "ins-sh-001", "sh"),
				envelope("evt-run-sh-001-02", 2, "2026-07-10T05:00:02Z"),
				adapter.Output{Kind: adapter.OutputStdout, Message: "hello\n"},
			)
		},
		"herdr": func() (ResultRow, error) {
			return ResultForAdapterError(
				request("run-herdr-001", "ins-herdr-001", "herdr"),
				envelope("evt-run-herdr-001-03", 3, "2026-07-10T05:01:03Z"),
				&herdrSession,
				&adapter.FailureError{Class: adapter.FailureFailed, Code: "adapter_exit", Message: "adapter exited with status 1", Retryable: retryable},
			)
		},
		"codex": func() (ResultRow, error) {
			return ResultForAdapterError(
				request("run-codex-timeout-001", "ins-codex-timeout-001", "codex"),
				envelope("evt-run-codex-timeout-001-02", 2, "2026-07-10T05:08:01Z"),
				&codexSession,
				context.DeadlineExceeded,
			)
		},
		"claude": func() (ResultRow, error) {
			return ResultForAdapterOutput(
				request("run-claude-running-001", "ins-claude-running-001", "claude"),
				envelope("evt-run-claude-running-001-02", 2, "2026-07-10T05:06:02Z"),
				adapter.Output{Kind: adapter.OutputStdout, Message: "working", NativeSessionID: &claudeSession},
			)
		},
	}

	if len(expected) != len(cases) {
		t.Fatalf("fixture targets=%d mapper targets=%d", len(expected), len(cases))
	}
	for target, build := range cases {
		got, err := build()
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if !reflect.DeepEqual(got, expected[target]) {
			t.Fatalf("%s mapper drift\n got: %#v\nwant: %#v", target, got, expected[target])
		}
	}
}

func TestAdapterCompletionMapsFinalIntoCanonicalShape(t *testing.T) {
	session := "native-1"
	row, err := ResultForAdapterCompletion(
		request("run-1", "ins-1", "herdr"),
		envelope("evt-1", 2, "2026-07-10T00:00:02Z"),
		adapter.Completion{FinalText: "answer", FinalPath: "outputs/run-1/final.json", NativeSessionID: &session},
	)
	if err != nil {
		t.Fatal(err)
	}
	if row.Kind != ResultCompleted || row.Final == nil || row.Final.Text != "answer" || row.Final.Path != "outputs/run-1/final.json" {
		t.Fatalf("completion mapping drifted: %#v", row)
	}
}

func TestAdapterErrorMappingUsesCanonicalTerminalKinds(t *testing.T) {
	cases := []struct {
		name string
		err  error
		kind string
		code string
	}{
		{name: "cancelled", err: context.Canceled, kind: ResultCancelled, code: "cancel_requested"},
		{name: "unavailable", err: &adapter.AdapterUnavailableError{Target: "sh"}, kind: ResultBlocked, code: "adapter_unavailable"},
		{name: "blocked", err: adapter.NewBlockedError("approval_required", "approval required"), kind: ResultBlocked, code: "approval_required"},
		{name: "generic", err: errors.New("boom"), kind: ResultFailed, code: "adapter_error"},
		{name: "invalid structured", err: &adapter.FailureError{Class: "completed"}, kind: ResultFailed, code: "protocol_error"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			row, err := ResultForAdapterError(request("run-1", "ins-1", "sh"), envelope("evt-1", 1, "2026-07-10T00:00:01Z"), nil, test.err)
			if err != nil {
				t.Fatal(err)
			}
			if row.Kind != test.kind || row.Error == nil || row.Error.Code != test.code {
				t.Fatalf("mapping drifted: %#v", row)
			}
		})
	}
}

func TestParallelAdapterEventContractIsAbsent(t *testing.T) {
	paths := []string{
		filepath.Join("adapter", "contract.go"),
		filepath.Join("adapter", "registry.go"),
		"adapter_mapper.go",
		filepath.Join("..", "..", "docs", "architecture", "worker-adapter-contract.md"),
		filepath.Join("..", "..", "spec", "fixtures", "adapter", "result-v1.jsonl"),
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "adapter.event.v1") {
			t.Fatalf("parallel persisted contract remains in %s", path)
		}
	}
}

func request(runID, instructionID, target string) adapter.Request {
	return adapter.Request{RunID: runID, InstructionID: instructionID, Target: target, Operation: "run", Payload: json.RawMessage(`{}`)}
}

func envelope(eventID string, seq int, recordedAt string) AdapterEnvelope {
	value, err := time.Parse(time.RFC3339, recordedAt)
	if err != nil {
		panic(err)
	}
	return AdapterEnvelope{EventID: eventID, Seq: seq, RecordedAt: value}
}

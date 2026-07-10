package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hq/internal/worker"
)

func TestListEmptyEvidenceContract(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "missing-instructions.jsonl")
	events := filepath.Join(root, "missing-events.jsonl")

	var jsonOut, jsonErr bytes.Buffer
	if code := run([]string{"list", "--input", input, "--events", events, "--json"}, &jsonOut, &jsonErr); code != 0 {
		t.Fatalf("json list code=%d stdout=%q stderr=%q", code, jsonOut.String(), jsonErr.String())
	}
	if jsonOut.Len() != 0 || jsonErr.Len() != 0 {
		t.Fatalf("json empty contract stdout=%q stderr=%q", jsonOut.String(), jsonErr.String())
	}

	var textOut, textErr bytes.Buffer
	if code := run([]string{"list", "--input", input, "--events", events}, &textOut, &textErr); code != 0 {
		t.Fatalf("text list code=%d stdout=%q stderr=%q", code, textOut.String(), textErr.String())
	}
	if textOut.String() != "no runs\n" || textErr.Len() != 0 {
		t.Fatalf("text empty contract stdout=%q stderr=%q", textOut.String(), textErr.String())
	}
}

func TestObservationCommandsAreReadOnlyAndLimitIsDeterministic(t *testing.T) {
	input, events, newestRun := writeTwoRunObservationFixture(t)
	beforeInput, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents, err := os.ReadFile(events)
	if err != nil {
		t.Fatal(err)
	}

	var listOut, listErr bytes.Buffer
	if code := run([]string{"list", "--input", input, "--events", events, "--limit", "1", "--json"}, &listOut, &listErr); code != 0 {
		t.Fatalf("list code=%d stdout=%q stderr=%q", code, listOut.String(), listErr.String())
	}
	lines := strings.Split(strings.TrimSpace(listOut.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("limited rows=%d output=%q", len(lines), listOut.String())
	}
	var ledger worker.LedgerRow
	if err := json.Unmarshal([]byte(lines[0]), &ledger); err != nil {
		t.Fatal(err)
	}
	if ledger.RunID != newestRun {
		t.Fatalf("limited newest run=%q want=%q", ledger.RunID, newestRun)
	}

	var textOut, textErr bytes.Buffer
	if code := run([]string{"list", "--input", input, "--events", events, "--limit", "1"}, &textOut, &textErr); code != 0 {
		t.Fatalf("text list code=%d stdout=%q stderr=%q", code, textOut.String(), textErr.String())
	}
	if !strings.Contains(textOut.String(), newestRun) {
		t.Fatalf("text list does not match JSON selection: %q", textOut.String())
	}

	var showOut, showErr bytes.Buffer
	if code := run([]string{"show", "--input", input, "--events", events, "--run", newestRun, "--json"}, &showOut, &showErr); code != 0 {
		t.Fatalf("show code=%d stdout=%q stderr=%q", code, showOut.String(), showErr.String())
	}
	var detail map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(showOut.Bytes()), &detail); err != nil {
		t.Fatal(err)
	}
	if _, duplicated := detail["error"]; duplicated {
		t.Fatalf("run error is duplicated at detail top level: %+v", detail)
	}

	var tailOut, tailErr bytes.Buffer
	if code := run([]string{"tail", "--events", events, "--run", newestRun, "--follow=false", "--json"}, &tailOut, &tailErr); code != 0 {
		t.Fatalf("tail code=%d stdout=%q stderr=%q", code, tailOut.String(), tailErr.String())
	}

	afterInput, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	afterEvents, err := os.ReadFile(events)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeInput, afterInput) || !bytes.Equal(beforeEvents, afterEvents) {
		t.Fatal("observation command changed canonical evidence")
	}
}

func TestObservationArgumentFailuresAreTyped(t *testing.T) {
	cases := [][]string{
		{"list", "--limit", "-1"},
		{"show"},
		{"tail", "--run", "r", "--poll", "0s"},
		{"tail", "--run", "r", "--timeout", "-1s"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 1 {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
		var failure struct {
			Version string `json:"version"`
			Code    string `json:"code"`
		}
		if err := json.Unmarshal(bytes.TrimSpace(stderr.Bytes()), &failure); err != nil {
			t.Fatalf("args=%v stderr=%q err=%v", args, stderr.String(), err)
		}
		if failure.Version != "worker.error.v1" || failure.Code != "invalid_arguments" {
			t.Fatalf("args=%v failure=%+v", args, failure)
		}
	}
}

func TestTailTimeoutDoesNotFabricateTerminalEvidence(t *testing.T) {
	root := t.TempDir()
	events := filepath.Join(root, "events.jsonl")
	at := time.Date(2026, 7, 10, 7, 0, 0, 0, time.UTC)
	log := worker.NewEventLog(events)
	for _, row := range []worker.ResultRow{
		{EventID: "e0", Version: worker.ResultVersionV1, RunID: "run-running", InstructionID: "ins-running", Target: "codex", Kind: worker.ResultAccepted, Seq: 0, RecordedAt: at},
		{EventID: "e1", Version: worker.ResultVersionV1, RunID: "run-running", InstructionID: "ins-running", Target: "codex", Kind: worker.ResultStarted, Seq: 1, RecordedAt: at.Add(time.Second)},
	} {
		if err := log.Append(worker.ResultEntry(row)); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.ReadFile(events)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"tail", "--events", events, "--run", "run-running", "--json",
		"--poll", "1ms", "--timeout", "20ms",
	}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if lines := strings.Split(strings.TrimSpace(stdout.String()), "\n"); len(lines) != 2 {
		t.Fatalf("tail should emit existing rows before timeout: %q", stdout.String())
	}
	var failure struct {
		Version string `json:"version"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stderr.Bytes()), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Version != "worker.error.v1" || failure.Code != "tail_timeout" {
		t.Fatalf("failure=%+v", failure)
	}
	after, err := os.ReadFile(events)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("tail timeout changed canonical evidence")
	}
}

func TestShowDistinguishesInvalidRunEvidenceFromMissingRun(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "instructions.jsonl")
	events := filepath.Join(root, "events.jsonl")
	instruction := `{"id":"ins-invalid","version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["true"],"cwd":"."},"created_at":"2026-07-10T00:00:00Z"}` + "\n"
	if err := os.WriteFile(input, []byte(instruction), 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	log := worker.NewEventLog(events)
	if err := log.Append(worker.ResultEntry(worker.ResultRow{
		EventID: "invalid-accepted", Version: worker.ResultVersionV1, RunID: "run-invalid", InstructionID: "ins-invalid",
		Target: "sh", Kind: worker.ResultAccepted, Seq: 0, RecordedAt: at,
	})); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(worker.ResultEntry(worker.ResultRow{
		EventID: "invalid-completed", Version: worker.ResultVersionV1, RunID: "run-invalid", InstructionID: "ins-invalid",
		Target: "sh", Kind: worker.ResultCompleted, Seq: 2, RecordedAt: at.Add(time.Second), Final: &worker.FinalResult{Text: "must not project"},
	})); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"show", "--input", input, "--events", events, "--run", "run-invalid", "--json"}, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var failure struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stderr.Bytes()), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Code != "invalid_run_evidence" {
		t.Fatalf("failure=%+v", failure)
	}
}

func writeTwoRunObservationFixture(t *testing.T) (input, events, newestRun string) {
	t.Helper()
	root := t.TempDir()
	input = filepath.Join(root, "instructions.jsonl")
	events = filepath.Join(root, "events.jsonl")
	instructions := strings.Join([]string{
		`{"id":"ins-old","version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["printf","old"],"cwd":"."},"created_at":"2026-07-10T00:00:00Z"}`,
		`{"id":"ins-new","version":"instruction.v1","op":"run","target":"claude","payload":{"prompt":"Inspect the newest run.","cwd":"."},"created_at":"2026-07-10T00:01:00Z"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(input, []byte(instructions), 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	log := worker.NewEventLog(events)
	retryable := false
	rows := []worker.ResultRow{
		{EventID: "old-0", Version: worker.ResultVersionV1, RunID: "run-old", InstructionID: "ins-old", Target: "sh", Kind: worker.ResultAccepted, Seq: 0, RecordedAt: at},
		{EventID: "old-1", Version: worker.ResultVersionV1, RunID: "run-old", InstructionID: "ins-old", Target: "sh", Kind: worker.ResultCompleted, Seq: 1, RecordedAt: at.Add(time.Second), Final: &worker.FinalResult{Text: "old"}},
		{EventID: "new-0", Version: worker.ResultVersionV1, RunID: "run-new", InstructionID: "ins-new", Target: "claude", Kind: worker.ResultAccepted, Seq: 0, RecordedAt: at.Add(2 * time.Second)},
		{EventID: "new-1", Version: worker.ResultVersionV1, RunID: "run-new", InstructionID: "ins-new", Target: "claude", Kind: worker.ResultBlocked, Seq: 1, RecordedAt: at.Add(3 * time.Second), Error: &worker.ResultError{Code: "approval_required", Message: "approval is required", Retryable: &retryable}},
	}
	for _, row := range rows {
		if err := log.Append(worker.ResultEntry(row)); err != nil {
			t.Fatal(err)
		}
	}
	return input, events, "run-new"
}

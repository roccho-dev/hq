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

func TestListShowAndTailCommandsReadCanonicalEvidence(t *testing.T) {
	input, events := writeObservationFixture(t)

	var listOut, listErr bytes.Buffer
	if code := run([]string{"list", "--input", input, "--events", events, "--json"}, &listOut, &listErr); code != 0 {
		t.Fatalf("list code=%d stdout=%q stderr=%q", code, listOut.String(), listErr.String())
	}
	var ledger worker.LedgerRow
	if err := json.Unmarshal(bytes.TrimSpace(listOut.Bytes()), &ledger); err != nil {
		t.Fatal(err)
	}
	if ledger.RunID != "run-1" || ledger.Status != worker.StatusCompleted || ledger.FinalPath != "out/run-1/final.txt" {
		t.Fatalf("ledger=%+v", ledger)
	}

	var showOut, showErr bytes.Buffer
	if code := run([]string{"show", "--input", input, "--events", events, "--run", "run-1", "--json"}, &showOut, &showErr); code != 0 {
		t.Fatalf("show code=%d stdout=%q stderr=%q", code, showOut.String(), showErr.String())
	}
	var detail worker.RunDetail
	if err := json.Unmarshal(bytes.TrimSpace(showOut.Bytes()), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Final == nil || detail.Final.Text != "done" || len(detail.Events) != 3 {
		t.Fatalf("detail=%+v", detail)
	}

	var tailOut, tailErr bytes.Buffer
	if code := run([]string{"tail", "--events", events, "--run", "run-1", "--follow=false", "--json"}, &tailOut, &tailErr); code != 0 {
		t.Fatalf("tail code=%d stdout=%q stderr=%q", code, tailOut.String(), tailErr.String())
	}
	if lines := strings.Split(strings.TrimSpace(tailOut.String()), "\n"); len(lines) != 3 {
		t.Fatalf("tail lines=%d output=%q", len(lines), tailOut.String())
	}
}

func TestShowMissingRunReturnsStructuredError(t *testing.T) {
	input, events := writeObservationFixture(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"show", "--input", input, "--events", events, "--run", "missing", "--json"}, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var failure struct {
		Version string `json:"version"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stderr.Bytes()), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Version != "worker.error.v1" || failure.Code != "run_not_found" {
		t.Fatalf("failure=%+v", failure)
	}
}

func TestViewReconstructsFinalWithoutRawStreamOrMutation(t *testing.T) {
	root := t.TempDir()
	events := filepath.Join(root, "events.jsonl")
	at := time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC)
	raw := "claude-raw-stream-must-not-appear"
	log := worker.NewEventLog(events)
	for _, row := range []worker.ResultRow{
		{EventID: "view-e0", Version: worker.ResultVersionV1, RunID: "run-view", InstructionID: "ins-view", Target: "local-tool", Kind: worker.ResultAccepted, Seq: 0, RecordedAt: at},
		{EventID: "view-e1", Version: worker.ResultVersionV1, RunID: "run-view", InstructionID: "ins-view", Target: "local-tool", Kind: worker.ResultStarted, Seq: 1, RecordedAt: at.Add(time.Second)},
		{EventID: "view-e2", Version: worker.ResultVersionV1, RunID: "run-view", InstructionID: "ins-view", Target: "local-tool", Kind: worker.ResultStdout, Seq: 2, RecordedAt: at.Add(2 * time.Second), Message: &raw},
		{EventID: "view-e3", Version: worker.ResultVersionV1, RunID: "run-view", InstructionID: "ins-view", Target: "local-tool", Kind: worker.ResultCompleted, Seq: 3, RecordedAt: at.Add(3 * time.Second), Final: &worker.FinalResult{Text: "compact Claude final"}},
	} {
		if err := log.Append(worker.ResultEntry(row)); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.ReadFile(events)
	if err != nil {
		t.Fatal(err)
	}

	var firstOut, firstErr bytes.Buffer
	args := []string{"view", "--events", events, "--run", "run-view", "--follow=false"}
	if code := run(args, &firstOut, &firstErr); code != 0 {
		t.Fatalf("first view code=%d stdout=%q stderr=%q", code, firstOut.String(), firstErr.String())
	}
	var replayOut, replayErr bytes.Buffer
	if code := run(args, &replayOut, &replayErr); code != 0 {
		t.Fatalf("replay view code=%d stdout=%q stderr=%q", code, replayOut.String(), replayErr.String())
	}
	if firstOut.String() != replayOut.String() || strings.Contains(firstOut.String(), raw) ||
		!strings.Contains(firstOut.String(), "queued") || !strings.Contains(firstOut.String(), "running") ||
		!strings.Contains(firstOut.String(), "completed") || !strings.Contains(firstOut.String(), "compact Claude final") {
		t.Fatalf("first=%q replay=%q", firstOut.String(), replayOut.String())
	}
	after, err := os.ReadFile(events)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("view changed canonical evidence")
	}
}

func writeObservationFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	input := filepath.Join(root, "instructions.jsonl")
	events := filepath.Join(root, "events.jsonl")
	instruction := `{"id":"ins-1","version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["printf","done"],"cwd":"."},"created_at":"2026-07-10T00:00:00Z"}` + "\n"
	if err := os.WriteFile(input, []byte(instruction), 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 7, 10, 1, 0, 0, 0, time.UTC)
	log := worker.NewEventLog(events)
	for _, row := range []worker.ResultRow{
		{EventID: "e0", Version: worker.ResultVersionV1, RunID: "run-1", InstructionID: "ins-1", Target: "sh", Kind: worker.ResultAccepted, Seq: 0, RecordedAt: at},
		{EventID: "e1", Version: worker.ResultVersionV1, RunID: "run-1", InstructionID: "ins-1", Target: "sh", Kind: worker.ResultStarted, Seq: 1, RecordedAt: at.Add(time.Second)},
		{EventID: "e2", Version: worker.ResultVersionV1, RunID: "run-1", InstructionID: "ins-1", Target: "sh", Kind: worker.ResultCompleted, Seq: 2, RecordedAt: at.Add(2 * time.Second), Final: &worker.FinalResult{Text: "done", Path: "out/run-1/final.txt"}},
	} {
		if err := log.Append(worker.ResultEntry(row)); err != nil {
			t.Fatal(err)
		}
	}
	return input, events
}

package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildLedgerListsMixedTargetsNewestFirst(t *testing.T) {
	at := time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC)
	native := "codex-session-1"
	retryable := true
	instructions := []Instruction{
		{ID: "ins-sh", Version: InstructionVersionV1, Op: "run", Target: "sh", Payload: []byte(`{"argv":["printf","hello"],"cwd":"work"}`), CreatedAt: at.Add(-time.Minute).Format(time.RFC3339)},
		{ID: "ins-codex", Version: InstructionVersionV1, Op: "run", Target: "codex", Payload: []byte(`{"prompt":"Inspect the durable run evidence.","cwd":"."}`), CreatedAt: at.Format(time.RFC3339)},
	}
	results := []ResultRow{
		{EventID: "sh-0", Version: ResultVersionV1, RunID: "run-sh", InstructionID: "ins-sh", Target: "sh", Kind: ResultAccepted, Seq: 0, RecordedAt: at},
		{EventID: "sh-1", Version: ResultVersionV1, RunID: "run-sh", InstructionID: "ins-sh", Target: "sh", Kind: ResultStarted, Seq: 1, RecordedAt: at.Add(time.Second)},
		{EventID: "sh-2", Version: ResultVersionV1, RunID: "run-sh", InstructionID: "ins-sh", Target: "sh", Kind: ResultCompleted, Seq: 2, RecordedAt: at.Add(2 * time.Second), Final: &FinalResult{Text: "hello", Path: "out/run-sh/final.txt"}},
		{EventID: "codex-0", Version: ResultVersionV1, RunID: "run-codex", InstructionID: "ins-codex", Target: "codex", Kind: ResultAccepted, Seq: 0, RecordedAt: at.Add(3 * time.Second)},
		{EventID: "codex-1", Version: ResultVersionV1, RunID: "run-codex", InstructionID: "ins-codex", Target: "codex", Kind: ResultStarted, Seq: 1, RecordedAt: at.Add(4 * time.Second), NativeSessionID: &native},
		{EventID: "codex-2", Version: ResultVersionV1, RunID: "run-codex", InstructionID: "ins-codex", Target: "codex", Kind: ResultFailed, Seq: 2, RecordedAt: at.Add(5 * time.Second), NativeSessionID: &native, Error: &ResultError{Code: "adapter_exit", Message: "adapter exited", Retryable: &retryable}},
	}
	ledger, diagnostics := BuildLedger(instructions, results)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
	if len(ledger) != 2 || ledger[0].RunID != "run-codex" || ledger[1].RunID != "run-sh" {
		t.Fatalf("ledger=%+v", ledger)
	}
	if ledger[0].Op != "run" || ledger[0].Status != StatusFailed || ledger[0].NativeSessionID != native || ledger[0].Error == nil {
		t.Fatalf("codex row=%+v", ledger[0])
	}
	if ledger[1].CWD != "work" || ledger[1].FinalPath != "out/run-sh/final.txt" || ledger[1].Summary != "hello" {
		t.Fatalf("sh row=%+v", ledger[1])
	}
}

func TestBuildRunDetailExplainsOneRunFromDurableRows(t *testing.T) {
	at := time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC)
	native := "claude-session-1"
	instruction := Instruction{ID: "ins-1", Version: InstructionVersionV1, Op: "run", Target: "claude", Payload: []byte(`{"prompt":"Summarize the final answer from durable evidence.","cwd":"."}`), CreatedAt: at.Format(time.RFC3339)}
	results := []ResultRow{
		{EventID: "e0", Version: ResultVersionV1, RunID: "run-1", InstructionID: "ins-1", Target: "claude", Kind: ResultAccepted, Seq: 0, RecordedAt: at},
		{EventID: "e1", Version: ResultVersionV1, RunID: "run-1", InstructionID: "ins-1", Target: "claude", Kind: ResultStarted, Seq: 1, RecordedAt: at.Add(time.Second), NativeSessionID: &native},
		{EventID: "e2", Version: ResultVersionV1, RunID: "run-1", InstructionID: "ins-1", Target: "claude", Kind: ResultCompleted, Seq: 2, RecordedAt: at.Add(2 * time.Second), NativeSessionID: &native, Final: &FinalResult{Text: "final answer", Path: "out/run-1/final.txt"}},
	}
	detail, diagnostics, err := BuildRunDetail("run-1", []Instruction{instruction}, results)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("detail=%+v diagnostics=%+v err=%v", detail, diagnostics, err)
	}
	if detail.Instruction.Summary != "Summarize the final answer from durable evidence." || detail.Final == nil || detail.Final.Text != "final answer" {
		t.Fatalf("detail=%+v", detail)
	}
	if !strings.Contains(detail.AttachHint, native) || len(detail.Events) != 3 {
		t.Fatalf("detail=%+v", detail)
	}
}

func TestLoadInstructionsForObservationKeepsLaterValidRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instructions.jsonl")
	valid := `{"id":"ins-1","version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["true"],"cwd":"."},"created_at":"2026-07-10T00:00:00Z"}`
	if err := os.WriteFile(path, []byte("{bad\n"+valid+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	instructions, diagnostics, err := LoadInstructionsForObservation(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(instructions) != 1 || instructions[0].ID != "ins-1" || len(diagnostics) == 0 {
		t.Fatalf("instructions=%+v diagnostics=%+v", instructions, diagnostics)
	}
	if !strings.Contains(diagnostics[0].Field, "line 1") {
		t.Fatalf("diagnostic=%+v", diagnostics[0])
	}
}

func TestFollowRunEmitsAppendedRowsThroughTerminalState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log := NewEventLog(path)
	at := time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC)
	for _, row := range []ResultRow{
		{EventID: "e0", Version: ResultVersionV1, RunID: "run-1", InstructionID: "ins-1", Target: "sh", Kind: ResultAccepted, Seq: 0, RecordedAt: at},
		{EventID: "e1", Version: ResultVersionV1, RunID: "run-1", InstructionID: "ins-1", Target: "sh", Kind: ResultStarted, Seq: 1, RecordedAt: at.Add(time.Second)},
	} {
		if err := log.Append(ResultEntry(row)); err != nil {
			t.Fatal(err)
		}
	}
	go func() {
		time.Sleep(25 * time.Millisecond)
		message := "working"
		_ = log.Append(ResultEntry(ResultRow{EventID: "e2", Version: ResultVersionV1, RunID: "run-1", InstructionID: "ins-1", Target: "sh", Kind: ResultStdout, Seq: 2, RecordedAt: at.Add(2 * time.Second), Message: &message}))
		_ = log.Append(ResultEntry(ResultRow{EventID: "e3", Version: ResultVersionV1, RunID: "run-1", InstructionID: "ins-1", Target: "sh", Kind: ResultCompleted, Seq: 3, RecordedAt: at.Add(3 * time.Second), Final: &FinalResult{Text: "done"}}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var kinds []string
	err := FollowRun(ctx, path, "run-1", true, 5*time.Millisecond, func(row ResultRow) error {
		kinds = append(kinds, row.Kind)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{ResultAccepted, ResultStarted, ResultStdout, ResultCompleted}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds=%v want=%v", kinds, want)
	}
}

func TestFollowRunMissingIDFailsWithStructuredError(t *testing.T) {
	err := FollowRun(context.Background(), filepath.Join(t.TempDir(), "missing.jsonl"), "missing", false, time.Millisecond, func(ResultRow) error { return nil })
	var observationError *ObservationError
	if !errors.As(err, &observationError) || observationError.Code != "run_not_found" {
		t.Fatalf("err=%T %v", err, err)
	}
}

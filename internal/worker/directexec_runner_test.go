package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"hq/internal/worker/adapter"
	"hq/internal/worker/directexec"
)

func buildDirectexecHelper(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	target := filepath.Join(t.TempDir(), "hq-worker-fixture"+suffix)
	cmd := exec.Command("go", "build", "-o", target, "./cmd/hq-worker-fixture")
	cmd.Dir = repoRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, output)
	}
	return target
}

func directexecRegistry(t *testing.T) *adapter.Registry {
	t.Helper()
	registry, err := adapter.NewRegistry(directexec.Registration())
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func directexecRow(t *testing.T, id string, argv []string) ReadRow {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"argv": argv, "cwd": "."})
	if err != nil {
		t.Fatal(err)
	}
	instruction := Instruction{
		ID: id, Version: InstructionVersionV1, Op: "run", Target: "sh", Payload: payload,
		CreatedAt: "2026-07-10T07:00:00Z",
	}
	encoded, err := json.Marshal(instruction)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ReadInstructions("queue.jsonl", strings.NewReader(string(encoded)))
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	return rows[0]
}

func resultKinds(entries []LogEntry) []string {
	var kinds []string
	for _, entry := range entries {
		if entry.Result != nil {
			kinds = append(kinds, entry.Result.Kind)
		}
	}
	return kinds
}

func TestRunnerExecutesApprovedDirectProcessOnce(t *testing.T) {
	helper := buildDirectexecHelper(t)
	workspace := t.TempDir()
	sentinel := filepath.Join(workspace, "sentinel.txt")
	row := directexecRow(t, "ins-direct-runner-001", []string{
		helper, "--stdout", "hello", "--stderr", "warning", "--sentinel", sentinel,
	})
	runner := NewRunner(workspace, directexecRegistry(t), approvedRunnerFixture(t, row.Instruction))
	sink := &memoryAppender{}
	emitted, unsuccessful, err := runner.Process(context.Background(), []ReadRow{row}, LogData{}, false, sink)
	if err != nil {
		t.Fatal(err)
	}
	if unsuccessful != 0 {
		t.Fatalf("unsuccessful=%d", unsuccessful)
	}
	want := []string{ResultAccepted, ResultStarted, ResultStdout, ResultStderr, ResultCompleted}
	got := resultKinds(emitted)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("kinds=%v want=%v entries=%+v", got, want, emitted)
	}
	if emitted[0].Policy == nil || !emitted[0].Policy.MayDispatch {
		t.Fatalf("policy=%+v", emitted[0].Policy)
	}
	started, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if string(started) != "started\n" {
		t.Fatalf("sentinel=%q", started)
	}

	prior := LogData{}
	for _, entry := range emitted {
		if entry.Result != nil {
			prior.Results = append(prior.Results, *entry.Result)
		}
	}
	second, unsuccessful, err := runner.Process(context.Background(), []ReadRow{row}, prior, false, &memoryAppender{})
	if err != nil {
		t.Fatal(err)
	}
	if unsuccessful != 1 || len(second) != 1 || second[0].Validation == nil || second[0].Validation.Error.Code != "duplicate_id" {
		t.Fatalf("second=%+v unsuccessful=%d", second, unsuccessful)
	}
	started, err = os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if string(started) != "started\n" {
		t.Fatalf("duplicate process start: %q", started)
	}
}

func TestRunnerMissingApprovalDoesNotStartDirectProcess(t *testing.T) {
	helper := buildDirectexecHelper(t)
	workspace := t.TempDir()
	sentinel := filepath.Join(workspace, "sentinel.txt")
	row := directexecRow(t, "ins-direct-blocked-001", []string{helper, "--sentinel", sentinel})
	runner := NewRunner(workspace, directexecRegistry(t), EmptyApprovalStore())
	emitted, unsuccessful, err := runner.Process(context.Background(), []ReadRow{row}, LogData{}, false, &memoryAppender{})
	if err != nil {
		t.Fatal(err)
	}
	if unsuccessful != 1 || len(emitted) != 3 || emitted[2].Result == nil || emitted[2].Result.Error.Code != "approval_required" {
		t.Fatalf("emitted=%+v unsuccessful=%d", emitted, unsuccessful)
	}
	if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("blocked process started: %v", err)
	}
}

func TestRunnerMapsDirectProcessNonZeroExit(t *testing.T) {
	helper := buildDirectexecHelper(t)
	workspace := t.TempDir()
	row := directexecRow(t, "ins-direct-exit-001", []string{helper, "--exit-code", "9"})
	runner := NewRunner(workspace, directexecRegistry(t), approvedRunnerFixture(t, row.Instruction))
	emitted, unsuccessful, err := runner.Process(context.Background(), []ReadRow{row}, LogData{}, false, &memoryAppender{})
	if err != nil {
		t.Fatal(err)
	}
	if unsuccessful != 1 || emitted[len(emitted)-1].Result == nil || emitted[len(emitted)-1].Result.Kind != ResultFailed || emitted[len(emitted)-1].Result.Error.Code != "process_exit_nonzero" {
		t.Fatalf("emitted=%+v unsuccessful=%d", emitted, unsuccessful)
	}
}

func TestRunnerMapsDirectProcessDeadlineWithPartialOutput(t *testing.T) {
	helper := buildDirectexecHelper(t)
	workspace := t.TempDir()
	row := directexecRow(t, "ins-direct-timeout-001", []string{
		helper, "--stdout", "partial-out", "--stderr", "partial-err", "--sleep", "5s",
	})
	runner := NewRunner(workspace, directexecRegistry(t), approvedRunnerFixture(t, row.Instruction))
	runner.Engine.Policy.DefaultTimeout = 150 * time.Millisecond
	runner.Engine.Policy.MaxTimeout = time.Second
	started := time.Now()
	emitted, unsuccessful, err := runner.Process(context.Background(), []ReadRow{row}, LogData{}, false, &memoryAppender{})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("worker stayed blocked: %s", elapsed)
	}
	if unsuccessful != 1 {
		t.Fatalf("unsuccessful=%d", unsuccessful)
	}
	want := []string{ResultAccepted, ResultStarted, ResultStdout, ResultStderr, ResultTimeout}
	if got := resultKinds(emitted); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("kinds=%v want=%v entries=%+v", got, want, emitted)
	}
	terminal := emitted[len(emitted)-1].Result
	if terminal.Error == nil || terminal.Error.Code != "deadline_exceeded" {
		t.Fatalf("terminal=%+v", terminal)
	}
}

func TestRunnerMapsExplicitDirectProcessCancellation(t *testing.T) {
	helper := buildDirectexecHelper(t)
	workspace := t.TempDir()
	row := directexecRow(t, "ins-direct-cancel-001", []string{helper, "--sleep", "5s"})
	runner := NewRunner(workspace, directexecRegistry(t), approvedRunnerFixture(t, row.Instruction))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	emitted, unsuccessful, err := runner.Process(ctx, []ReadRow{row}, LogData{}, false, &memoryAppender{})
	if err != nil {
		t.Fatal(err)
	}
	if unsuccessful != 1 || emitted[len(emitted)-1].Result == nil || emitted[len(emitted)-1].Result.Kind != ResultCancelled || emitted[len(emitted)-1].Result.Error.Code != "cancel_requested" {
		t.Fatalf("emitted=%+v unsuccessful=%d", emitted, unsuccessful)
	}
}

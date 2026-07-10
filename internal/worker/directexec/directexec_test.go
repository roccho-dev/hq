package directexec

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"hq/internal/worker/adapter"
)

func buildHelper(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	packageDir := filepath.Dir(file)
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	target := filepath.Join(t.TempDir(), "hq-worker-fixture"+suffix)
	cmd := exec.Command("go", "build", "-o", target, "../../../cmd/hq-worker-fixture")
	cmd.Dir = packageDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, output)
	}
	return target
}

func directRequest(t *testing.T, cwd string, argv []string) adapter.Request {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"argv": argv, "cwd": "."})
	if err != nil {
		t.Fatal(err)
	}
	return adapter.Request{
		RunID: "run-direct-001", InstructionID: "ins-direct-001", Target: "sh", Operation: "run",
		Payload: raw, CWD: cwd,
	}
}

func TestRegistrationOwnsOnlyCanonicalShTarget(t *testing.T) {
	registration := Registration()
	if registration.Target != "sh" || registration.Adapter == nil || registration.Provider != nil {
		t.Fatalf("registration=%+v", registration)
	}
}

func TestDirectExecutablePreservesExactArgumentsOutputAndEmptyEnvironment(t *testing.T) {
	helper := buildHelper(t)
	cwd := t.TempDir()
	argsFile := filepath.Join(cwd, "args.json")
	envFile := filepath.Join(cwd, "env.txt")
	literal := []string{";", "|", ">", "*", "$HOME", "%PATH%", `"quoted"`, "space value"}
	argv := []string{
		helper, "--stdout", "hello", "--stderr", "warning", "--args-file", argsFile,
		"--env-key", "HQ_DIRECTEXEC_AMBIENT", "--env-file", envFile, "--",
	}
	argv = append(argv, literal...)
	t.Setenv("HQ_DIRECTEXEC_AMBIENT", "must-not-survive")

	var output []adapter.Output
	completion, err := (Adapter{}).Run(context.Background(), directRequest(t, cwd, argv), func(value adapter.Output) error {
		output = append(output, value)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if completion.FinalText != "process exited successfully with status 0" {
		t.Fatalf("completion=%+v", completion)
	}
	wantOutput := []adapter.Output{
		{Kind: adapter.OutputStdout, Message: "hello"},
		{Kind: adapter.OutputStderr, Message: "warning"},
	}
	if !reflect.DeepEqual(output, wantOutput) {
		t.Fatalf("output=%+v want=%+v", output, wantOutput)
	}
	encoded, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	var actual []string
	if err := json.Unmarshal(encoded, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, literal) {
		t.Fatalf("args=%q want=%q", actual, literal)
	}
	environment, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(environment) != 0 {
		t.Fatalf("ambient environment survived: %q", environment)
	}
}

func TestDirectExecutableRejectsPATHLookup(t *testing.T) {
	_, err := (Adapter{}).Run(context.Background(), directRequest(t, t.TempDir(), []string{"printf", "hello"}), func(adapter.Output) error {
		return nil
	})
	var failure *adapter.FailureError
	if !errors.As(err, &failure) || failure.Class != adapter.FailureBlocked || failure.Code != "executable_path_required" {
		t.Fatalf("err=%T %v", err, err)
	}
}

func TestDirectExecutableMapsNonZeroExit(t *testing.T) {
	helper := buildHelper(t)
	_, err := (Adapter{}).Run(context.Background(), directRequest(t, t.TempDir(), []string{helper, "--exit-code", "7"}), func(adapter.Output) error {
		return nil
	})
	var failure *adapter.FailureError
	if !errors.As(err, &failure) || failure.Class != adapter.FailureFailed || failure.Code != "process_exit_nonzero" || !strings.Contains(failure.Message, "7") {
		t.Fatalf("err=%T %v", err, err)
	}
}

func TestDirectExecutableDeadlinePreservesPartialOutput(t *testing.T) {
	helper := buildHelper(t)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	var output []adapter.Output
	_, err := (Adapter{}).Run(ctx, directRequest(t, t.TempDir(), []string{
		helper, "--stdout", "partial-out", "--stderr", "partial-err", "--sleep", "5s",
	}), func(value adapter.Output) error {
		output = append(output, value)
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%T %v", err, err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("deadline did not stop direct child: %s", elapsed)
	}
	if len(output) != 2 || output[0].Kind != adapter.OutputStdout || output[0].Message != "partial-out" || output[1].Kind != adapter.OutputStderr || output[1].Message != "partial-err" {
		t.Fatalf("output=%+v", output)
	}
}

func TestDirectExecutableExplicitCancellation(t *testing.T) {
	helper := buildHelper(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	started := time.Now()
	_, err := (Adapter{}).Run(ctx, directRequest(t, t.TempDir(), []string{helper, "--sleep", "5s"}), func(adapter.Output) error {
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%T %v", err, err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("cancellation did not stop direct child: %s", elapsed)
	}
}

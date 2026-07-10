package agentadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"hq/internal/worker/adapter"
)

func TestDirectOSRunnerRejectsBareExecutableName(t *testing.T) {
	_, err := (DirectOSRunner{}).Run(context.Background(), Command{Path: "printf", Args: []string{"hello"}, Dir: t.TempDir()})
	var failure *adapter.FailureError
	if !errors.As(err, &failure) || failure.Class != adapter.FailureBlocked || failure.Code != "executable_path_required" {
		t.Fatalf("error=%T %v", err, err)
	}
}

func TestDirectOSRunnerPreservesLiteralArgumentsAndDropsAmbientEnvironment(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	argsPath := filepath.Join(root, "args.json")
	envPath := filepath.Join(root, "env.txt")
	literal := []string{";", "|", ">", "*", "$HOME", "%PATH%", `"quoted"`, "space value"}
	t.Setenv("HQ_DIRECT_RUNNER_SECRET", "must-not-survive")
	args := []string{"-test.run=TestDirectRunnerHelperProcess", "--", "record", argsPath, envPath}
	args = append(args, literal...)
	result, err := (DirectOSRunner{}).Run(context.Background(), Command{Path: executable, Args: args, Dir: root})
	if err != nil {
		t.Fatalf("run: %v stderr=%s", err, result.Stderr)
	}
	encoded, err := os.ReadFile(argsPath)
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
	environment, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(environment) != 0 {
		t.Fatalf("ambient environment survived: %q", environment)
	}
}

func TestShMapsNonZeroExitToStableFailure(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, err = (Sh{}).Run(context.Background(), req("sh", ShPayload{Argv: []string{executable, "-test.run=TestDirectRunnerHelperProcess", "--", "exit7"}}, t.TempDir()), nil)
	var failure *adapter.FailureError
	if !errors.As(err, &failure) || failure.Class != adapter.FailureFailed || failure.Code != "process_exit_nonzero" || !strings.Contains(failure.Message, "7") {
		t.Fatalf("error=%T %v", err, err)
	}
}

func TestShTimeoutPreservesOutputBeforeTerminalError(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var output []adapter.Output
	_, err = (Sh{}).Run(ctx, req("sh", ShPayload{Argv: []string{executable, "-test.run=TestDirectRunnerHelperProcess", "--", "partial-sleep"}}, t.TempDir()), func(value adapter.Output) error {
		output = append(output, value)
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%T %v", err, err)
	}
	if len(output) != 2 || output[0].Kind != adapter.OutputStderr || output[0].Message != "partial-err" || output[1].Kind != adapter.OutputStdout || output[1].Message != "partial-out" {
		t.Fatalf("output=%+v", output)
	}
}

func TestDirectRunnerHelperProcess(t *testing.T) {
	separator := -1
	for index, value := range os.Args {
		if value == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	switch os.Args[separator+1] {
	case "record":
		if separator+3 >= len(os.Args) {
			os.Exit(125)
		}
		encoded, _ := json.Marshal(os.Args[separator+4:])
		if err := os.WriteFile(os.Args[separator+2], encoded, 0o600); err != nil {
			os.Exit(125)
		}
		if err := os.WriteFile(os.Args[separator+3], []byte(os.Getenv("HQ_DIRECT_RUNNER_SECRET")), 0o600); err != nil {
			os.Exit(125)
		}
	case "exit7":
		os.Exit(7)
	case "partial-sleep":
		_, _ = fmt.Fprint(os.Stdout, "partial-out")
		_, _ = fmt.Fprint(os.Stderr, "partial-err")
		time.Sleep(30 * time.Second)
	}
}

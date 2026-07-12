package directexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRunnerUsesLiteralArgvAndExactEnvironment(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	literal := []string{";", "|", ">", "*", "$HOME", "%PATH%", `"quoted"`, "space value"}
	t.Setenv("HQ_DIRECTEXEC_SECRET", "must-not-survive")
	args := []string{"-test.run=TestDirectExecHelperProcess", "--", "record"}
	args = append(args, literal...)
	result, err := (Runner{}).Run(context.Background(), Command{
		Path: executable, Args: args, Dir: t.TempDir(),
		Env: []string{"HQ_DIRECTEXEC_ALLOWED=visible"}, StdoutLimit: 4096, StderrLimit: 4096,
	})
	if err != nil {
		t.Fatalf("run: %v stderr=%s", err, result.Stderr)
	}
	var actual struct {
		Args    []string `json:"args"`
		Allowed string   `json:"allowed"`
		Secret  string   `json:"secret"`
	}
	if err := json.Unmarshal(result.Stdout, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual.Args, literal) || actual.Allowed != "visible" || actual.Secret != "" {
		t.Fatalf("actual=%+v", actual)
	}
}

func TestRunnerEmptyEnvironmentDoesNotInherit(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HQ_DIRECTEXEC_SECRET", "must-not-survive")
	result, err := (Runner{}).Run(context.Background(), Command{
		Path: executable, Args: []string{"-test.run=TestDirectExecHelperProcess", "--", "record"},
		Dir: t.TempDir(), StdoutLimit: 4096, StderrLimit: 4096,
	})
	if err != nil {
		t.Fatalf("run: %v stderr=%s", err, result.Stderr)
	}
	var actual struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(result.Stdout, &actual); err != nil {
		t.Fatal(err)
	}
	if actual.Secret != "" {
		t.Fatalf("ambient environment survived: %q", actual.Secret)
	}
}

func TestRunnerEnforcesInputAndOutputBounds(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	assertCode := func(t *testing.T, err error, code string) {
		t.Helper()
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != code {
			t.Fatalf("error=%T %v want code=%s", err, err, code)
		}
	}

	_, err = (Runner{}).Run(context.Background(), Command{Path: executable, Dir: t.TempDir(), Stdin: []byte("12345"), StdinLimit: 4})
	assertCode(t, err, "stdin_limit_exceeded")

	result, err := (Runner{}).Run(context.Background(), Command{
		Path: executable, Args: []string{"-test.run=TestDirectExecHelperProcess", "--", "emit-stdout"},
		Dir: t.TempDir(), StdoutLimit: 4, StderrLimit: 16,
	})
	assertCode(t, err, "stdout_limit_exceeded")
	if string(result.Stdout) != "0123" {
		t.Fatalf("stdout=%q", result.Stdout)
	}

	result, err = (Runner{}).Run(context.Background(), Command{
		Path: executable, Args: []string{"-test.run=TestDirectExecHelperProcess", "--", "emit-stderr"},
		Dir: t.TempDir(), StdoutLimit: 16, StderrLimit: 4,
	})
	assertCode(t, err, "stderr_limit_exceeded")
	if string(result.Stderr) != "0123" {
		t.Fatalf("stderr=%q", result.Stderr)
	}
}

func TestRunnerCancellationReturnsBoundedPartialOutput(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, err := (Runner{}).Run(ctx, Command{
		Path: executable, Args: []string{"-test.run=TestDirectExecHelperProcess", "--", "partial-sleep"},
		Dir: t.TempDir(), StdoutLimit: 64, StderrLimit: 64,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%T %v", err, err)
	}
	if string(result.Stdout) != "partial-out" || string(result.Stderr) != "partial-err" {
		t.Fatalf("result=%+v", result)
	}
}

func TestRunnerRejectsBareExecutableAndInvalidLimits(t *testing.T) {
	for _, test := range []struct {
		command Command
		code    string
	}{
		{command: Command{Path: "tool"}, code: "executable_path_required"},
		{command: Command{Path: `./tool`, StdoutLimit: -1}, code: "invalid_output_limit"},
	} {
		_, err := (Runner{}).Run(context.Background(), test.command)
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != test.code {
			t.Fatalf("error=%T %v want code=%s", err, err, test.code)
		}
	}
}

func TestDirectExecHelperProcess(t *testing.T) {
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
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"args": os.Args[separator+2:], "allowed": os.Getenv("HQ_DIRECTEXEC_ALLOWED"), "secret": os.Getenv("HQ_DIRECTEXEC_SECRET"),
		})
	case "emit-stdout":
		_, _ = io.WriteString(os.Stdout, "0123456789")
	case "emit-stderr":
		_, _ = io.WriteString(os.Stderr, "0123456789")
	case "partial-sleep":
		_, _ = fmt.Fprint(os.Stdout, "partial-out")
		_, _ = fmt.Fprint(os.Stderr, "partial-err")
		time.Sleep(30 * time.Second)
	default:
		_, _ = io.WriteString(os.Stderr, strings.Join(os.Args[separator+1:], " "))
		os.Exit(125)
	}
	os.Exit(0)
}

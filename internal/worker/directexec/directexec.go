// Package directexec implements the canonical target=sh adapter as direct
// argv execution. It never evaluates shell language or performs PATH lookup.
package directexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"hq/internal/worker/adapter"
)

// Adapter executes one already-validated sh request as an exact child process.
type Adapter struct{}

type payload struct {
	Argv []string `json:"argv"`
	CWD  string   `json:"cwd,omitempty"`
}

// Registration returns the one exact registry entry owned by this package.
func Registration() adapter.Registration {
	return adapter.Registration{Target: "sh", Adapter: Adapter{}}
}

// Run executes argv[0] with argv[1:] without an intervening shell. The worker
// owns validation, approval, cwd selection, deadline, durable identity, and
// lifecycle mapping; this adapter returns only transient output/completion.
func (Adapter) Run(ctx context.Context, request adapter.Request, emit adapter.Emit) (adapter.Completion, error) {
	if ctx == nil {
		return adapter.Completion{}, protocolFailure(errors.New("context is required"))
	}
	if emit == nil {
		return adapter.Completion{}, protocolFailure(errors.New("output emitter is required"))
	}
	if err := request.Validate(); err != nil {
		return adapter.Completion{}, protocolFailure(err)
	}
	if request.Target != "sh" {
		return adapter.Completion{}, blocked("target_mismatch", "direct executable adapter only accepts target sh")
	}
	if request.Operation != "run" {
		return adapter.Completion{}, blocked("operation_mismatch", "direct executable adapter only accepts operation run")
	}
	if strings.TrimSpace(request.CWD) == "" {
		return adapter.Completion{}, blocked("cwd_required", "worker-approved cwd is required")
	}

	decoded, err := decodePayload(request.Payload)
	if err != nil {
		return adapter.Completion{}, protocolFailure(err)
	}
	executable, err := resolveExecutable(request.CWD, decoded.Argv[0])
	if err != nil {
		return adapter.Completion{}, blocked("executable_path_required", err.Error())
	}

	cmd := exec.CommandContext(ctx, executable, decoded.Argv[1:]...)
	cmd.Dir = request.CWD
	// A direct adapter must not inherit ambient secrets or use inherited PATH as
	// hidden configuration. Windows' os/exec keeps the minimum SYSTEMROOT needed
	// by the platform when an explicit environment is supplied.
	cmd.Env = []string{}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	// Output produced before process termination remains evidence even when the
	// terminal outcome is timeout, cancellation, or a non-zero exit.
	if err := emitBuffer(emit, adapter.OutputStdout, &stdout); err != nil {
		return adapter.Completion{}, err
	}
	if err := emitBuffer(emit, adapter.OutputStderr, &stderr); err != nil {
		return adapter.Completion{}, err
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return adapter.Completion{}, ctxErr
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return adapter.Completion{}, &adapter.FailureError{
				Class:   adapter.FailureFailed,
				Code:    "process_exit_nonzero",
				Message: fmt.Sprintf("process exited with status %d", exitErr.ExitCode()),
			}
		}
		return adapter.Completion{}, &adapter.FailureError{
			Class:   adapter.FailureFailed,
			Code:    "process_start_failed",
			Message: runErr.Error(),
		}
	}

	return adapter.Completion{FinalText: "process exited successfully with status 0"}, nil
}

func decodePayload(raw json.RawMessage) (payload, error) {
	var value payload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return payload{}, fmt.Errorf("decode sh payload: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return payload{}, errors.New("decode sh payload: trailing data")
	}
	if len(value.Argv) == 0 {
		return payload{}, errors.New("argv is required")
	}
	for _, arg := range value.Argv {
		if strings.TrimSpace(arg) == "" {
			return payload{}, errors.New("argv contains an empty value")
		}
	}
	return value, nil
}

func resolveExecutable(cwd, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("argv[0] is empty")
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}
	if !strings.ContainsAny(value, "/\\") {
		return "", errors.New("argv[0] must be an absolute or explicit relative path; PATH lookup is forbidden")
	}
	return filepath.Clean(filepath.Join(cwd, value)), nil
}

func emitBuffer(emit adapter.Emit, kind adapter.OutputKind, buffer *bytes.Buffer) error {
	if buffer.Len() == 0 {
		return nil
	}
	return emit(adapter.Output{Kind: kind, Message: buffer.String()})
}

func protocolFailure(err error) error {
	return &adapter.FailureError{Class: adapter.FailureFailed, Code: "protocol_error", Message: err.Error()}
}

func blocked(code, message string) error {
	return &adapter.FailureError{Class: adapter.FailureBlocked, Code: code, Message: message}
}

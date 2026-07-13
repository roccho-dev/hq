// Package directexec owns the bounded, explicit-path process boundary shared
// by worker adapters. It never invokes a shell, performs PATH lookup, or
// inherits the ambient environment.
package directexec

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type Command struct {
	Path        string
	Args        []string
	Dir         string
	Stdin       []byte
	StdinLimit  int64
	StdoutLimit int64
	StderrLimit int64
	Env         []string
}

type Result struct {
	Stdout []byte
	Stderr []byte
}

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

type Runner struct{}

func (Runner) Run(ctx context.Context, command Command) (Result, error) {
	path := strings.TrimSpace(command.Path)
	if path == "" || (!filepath.IsAbs(path) && !strings.ContainsAny(path, `/\`)) {
		return Result{}, failure("executable_path_required", "executable path must be absolute or explicitly relative; PATH lookup is forbidden")
	}
	if command.StdinLimit < 0 || command.StdoutLimit < 0 || command.StderrLimit < 0 {
		return Result{}, failure("invalid_output_limit", "stdin, stdout, and stderr limits must be non-negative")
	}
	if command.StdinLimit > 0 && int64(len(command.Stdin)) > command.StdinLimit {
		return Result{}, failure("stdin_limit_exceeded", fmt.Sprintf("stdin exceeds %d-byte limit", command.StdinLimit))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Clean(filepath.Join(command.Dir, path))
	}
	cmd := exec.CommandContext(ctx, path, append([]string(nil), command.Args...)...)
	cmd.Dir = command.Dir
	// A nil exec.Cmd.Env inherits the parent. Always allocate, including for
	// the empty environment, so absence means intentionally empty.
	cmd.Env = make([]string, len(command.Env))
	copy(cmd.Env, command.Env)
	if len(command.Stdin) != 0 {
		cmd.Stdin = bytes.NewReader(command.Stdin)
	}
	stdout := newBoundedBuffer(command.StdoutLimit)
	stderr := newBoundedBuffer(command.StderrLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if contextErr := ctx.Err(); contextErr != nil {
		return result, contextErr
	}
	if stdout.Exceeded() {
		return result, failure("stdout_limit_exceeded", fmt.Sprintf("stdout exceeds %d-byte limit", command.StdoutLimit))
	}
	if stderr.Exceeded() {
		return result, failure("stderr_limit_exceeded", fmt.Sprintf("stderr exceeds %d-byte limit", command.StderrLimit))
	}
	return result, runErr
}

func failure(code, message string) error { return &Error{Code: code, Message: message} }

type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int64
	exceeded bool
}

func newBoundedBuffer(limit int64) *boundedBuffer { return &boundedBuffer{limit: limit} }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	length := len(p)
	if b.limit == 0 {
		_, _ = b.buffer.Write(p)
		return length, nil
	}
	remaining := b.limit - int64(b.buffer.Len())
	if remaining <= 0 {
		b.exceeded = b.exceeded || length != 0
		return length, nil
	}
	keep := int64(length)
	if keep > remaining {
		keep = remaining
		b.exceeded = true
	}
	_, _ = b.buffer.Write(p[:int(keep)])
	return length, nil
}

func (b *boundedBuffer) Bytes() []byte  { return append([]byte(nil), b.buffer.Bytes()...) }
func (b *boundedBuffer) Exceeded() bool { return b.exceeded }

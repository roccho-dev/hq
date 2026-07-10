// Package agentadapter implements concrete transient adapters for Herdr,
// Codex, and Claude. Durable identity and lifecycle remain worker-owned.
package agentadapter

import (
	"bytes"
	"context"
	"io"
	"os/exec"
)

type Command struct {
	Path string
	Args []string
	Dir  string
}

type CommandResult struct {
	Stdout []byte
	Stderr []byte
}

type Runner interface {
	Run(context.Context, Command) (CommandResult, error)
}

type RunnerFunc func(context.Context, Command) (CommandResult, error)

func (f RunnerFunc) Run(ctx context.Context, command Command) (CommandResult, error) {
	return f(ctx, command)
}

type OSRunner struct{}

func (OSRunner) Run(ctx context.Context, command Command) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	cmd.Dir = command.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = io.Writer(&stdout)
	cmd.Stderr = io.Writer(&stderr)
	err := cmd.Run()
	return CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
}

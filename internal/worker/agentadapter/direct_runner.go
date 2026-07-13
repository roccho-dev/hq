package agentadapter

import (
	"context"
	"errors"

	"hq/internal/worker/adapter"
	"hq/internal/worker/directexec"
)

// DirectOSRunner executes an already-validated explicit path without a shell,
// PATH lookup, or inherited environment. It is intentionally used only by the
// canonical sh/direct-argv adapter; provider CLIs retain their own environment
// contract through OSRunner.
type DirectOSRunner struct{}

func (DirectOSRunner) Run(ctx context.Context, command Command) (CommandResult, error) {
	result, err := (directexec.Runner{}).Run(ctx, directexec.Command{
		Path: command.Path, Args: command.Args, Dir: command.Dir, Stdin: command.Stdin,
		Env: []string{},
	})
	var directError *directexec.Error
	if errors.As(err, &directError) {
		return CommandResult{Stdout: result.Stdout, Stderr: result.Stderr}, &adapter.FailureError{
			Class: adapter.FailureBlocked, Code: directError.Code, Message: directError.Message,
		}
	}
	return CommandResult{Stdout: result.Stdout, Stderr: result.Stderr}, err
}

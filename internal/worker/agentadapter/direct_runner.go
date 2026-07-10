package agentadapter

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"path/filepath"
)

// DirectOSRunner executes an already-validated explicit path without a shell,
// PATH lookup, or inherited environment. It is intentionally used only by the
// canonical sh/direct-argv adapter; provider CLIs retain their own environment
// contract through OSRunner.
type DirectOSRunner struct{}

func (DirectOSRunner) Run(ctx context.Context, command Command) (CommandResult, error) {
	path := command.Path
	if !filepath.IsAbs(path) {
		path = filepath.Clean(filepath.Join(command.Dir, path))
	}
	cmd := exec.CommandContext(ctx, path, command.Args...)
	cmd.Dir = command.Dir
	cmd.Env = []string{}
	if len(command.Stdin) != 0 {
		cmd.Stdin = bytes.NewReader(command.Stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = io.Writer(&stdout)
	cmd.Stderr = io.Writer(&stderr)
	err := cmd.Run()
	if contextErr := ctx.Err(); contextErr != nil {
		err = contextErr
	}
	return CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
}

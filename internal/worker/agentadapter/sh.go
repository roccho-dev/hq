package agentadapter

import (
	"context"
	"strings"

	"hq/internal/worker/adapter"
)

// Sh executes the canonical target=sh payload as an argv vector. Despite the
// historical target name, it never invokes a shell and never reparses argv.
type Sh struct {
	Runner Runner
}

type ShPayload struct {
	CWD  string   `json:"cwd,omitempty"`
	Argv []string `json:"argv"`
}

func (p ShPayload) validate() error {
	if len(p.Argv) == 0 {
		return blocked("invalid_payload", "sh argv is required")
	}
	for _, value := range p.Argv {
		if strings.TrimSpace(value) == "" {
			return blocked("invalid_payload", "sh argv must contain only non-empty strings")
		}
	}
	return nil
}

func (a Sh) Run(ctx context.Context, request adapter.Request, emit adapter.Emit) (adapter.Completion, error) {
	if err := validateAdapterRequest(request, "sh"); err != nil {
		return adapter.Completion{}, err
	}
	var payload ShPayload
	if err := decodeStrict(request.Payload, &payload); err != nil {
		return adapter.Completion{}, blocked("invalid_payload", err.Error())
	}
	if err := payload.validate(); err != nil {
		return adapter.Completion{}, err
	}
	runner := a.Runner
	if runner == nil {
		runner = OSRunner{}
	}
	result, runErr := runner.Run(ctx, Command{
		Path: payload.Argv[0],
		Args: append([]string(nil), payload.Argv[1:]...),
		Dir:  effectiveDir(request, payload.CWD),
	})
	if err := emitStderr(result, "", emit); err != nil {
		return adapter.Completion{}, err
	}
	if err := emitLines(adapter.OutputStdout, result.Stdout, "", emit); err != nil {
		return adapter.Completion{}, err
	}
	if runErr != nil {
		return adapter.Completion{}, runErr
	}
	finalText := strings.TrimSpace(string(result.Stdout))
	if finalText == "" {
		finalText = "command completed"
	}
	return adapter.Completion{FinalText: finalText}, nil
}

package agentadapter

import (
	"context"
	"fmt"
	"strings"

	"hq/internal/worker/adapter"
)

type Herdr struct {
	Runner Runner
	Path   string
}

type HerdrPayload struct {
	Action      string   `json:"action"`
	CWD         string   `json:"cwd,omitempty"`
	Name        string   `json:"name,omitempty"`
	NoFocus     *bool    `json:"no_focus,omitempty"`
	Split       string   `json:"split,omitempty"`
	CommandArgv []string `json:"command_argv,omitempty"`
	Prompt      string   `json:"prompt,omitempty"`
	Agent       string   `json:"agent,omitempty"`
	Source      string   `json:"source,omitempty"`
	Lines       int      `json:"lines,omitempty"`
	WaitStatus  string   `json:"wait_status,omitempty"`
	TimeoutMS   int      `json:"timeout_ms,omitempty"`
	Takeover    bool     `json:"takeover,omitempty"`
}

func (p HerdrPayload) validate() error {
	switch p.Action {
	case "start":
		if strings.TrimSpace(p.Name) == "" {
			return blocked("invalid_payload", "herdr start requires name")
		}
		if len(p.CommandArgv) == 0 {
			return blocked("invalid_payload", "herdr start requires command_argv")
		}
		for _, value := range p.CommandArgv {
			if strings.TrimSpace(value) == "" {
				return blocked("invalid_payload", "herdr command_argv must contain only non-empty strings")
			}
		}
		if strings.TrimSpace(p.Prompt) == "" {
			return blocked("invalid_payload", "herdr start requires prompt")
		}
		if p.Split != "" && p.Split != "right" && p.Split != "down" {
			return blocked("invalid_payload", "herdr split must be right or down")
		}
	case "read":
		if strings.TrimSpace(p.Agent) == "" {
			return blocked("invalid_payload", "herdr read requires agent")
		}
		if p.Source != "" && p.Source != "visible" && p.Source != "recent" && p.Source != "recent-unwrapped" && p.Source != "detection" {
			return blocked("invalid_payload", "herdr read source is invalid")
		}
		if p.Lines < 0 {
			return blocked("invalid_payload", "herdr lines must be non-negative")
		}
	case "observe":
		if strings.TrimSpace(p.Agent) == "" {
			return blocked("invalid_payload", "herdr observe requires agent")
		}
		switch p.WaitStatus {
		case "", "idle", "working", "blocked", "unknown":
		default:
			return blocked("invalid_payload", "herdr wait_status is invalid")
		}
		if p.TimeoutMS < 0 || p.Lines < 0 {
			return blocked("invalid_payload", "herdr timeout_ms and lines must be non-negative")
		}
	case "attach":
		if strings.TrimSpace(p.Agent) == "" {
			return blocked("invalid_payload", "herdr attach requires agent")
		}
	default:
		return blocked("invalid_payload", "herdr action must be start, read, observe, or attach")
	}
	return nil
}

func (a Herdr) Run(ctx context.Context, request adapter.Request, emit adapter.Emit) (adapter.Completion, error) {
	if err := validateAdapterRequest(request, "herdr"); err != nil {
		return adapter.Completion{}, err
	}
	var payload HerdrPayload
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
	path := a.Path
	if path == "" {
		path = "herdr"
	}
	switch payload.Action {
	case "start":
		return a.start(ctx, runner, path, request, payload, emit)
	case "read":
		return a.read(ctx, runner, path, request, payload, emit)
	case "observe":
		return a.observe(ctx, runner, path, request, payload, emit)
	case "attach":
		command := fmt.Sprintf("herdr agent attach %s", payload.Agent)
		if payload.Takeover {
			command += " --takeover"
		}
		return adapter.Completion{FinalText: "Herdr attach target: " + command, NativeSessionID: stringPointer(payload.Agent)}, nil
	default:
		return adapter.Completion{}, blocked("invalid_payload", "unsupported Herdr action")
	}
}

func (a Herdr) start(ctx context.Context, runner Runner, path string, request adapter.Request, payload HerdrPayload, emit adapter.Emit) (adapter.Completion, error) {
	args := []string{"agent", "start", payload.Name, "--cwd", effectiveDir(request, payload.CWD)}
	split := payload.Split
	if split == "" {
		split = "right"
	}
	args = append(args, "--split", split)
	noFocus := true
	if payload.NoFocus != nil {
		noFocus = *payload.NoFocus
	}
	if noFocus {
		args = append(args, "--no-focus")
	} else {
		args = append(args, "--focus")
	}
	args = append(args, "--")
	args = append(args, payload.CommandArgv...)
	args = append(args, payload.Prompt)
	result, err := runner.Run(ctx, Command{Path: path, Args: args, Dir: effectiveDir(request, payload.CWD)})
	if emitErr := emitStderr(result, payload.Name, emit); emitErr != nil {
		return adapter.Completion{}, emitErr
	}
	if err != nil {
		return adapter.Completion{}, err
	}
	if emitErr := emitLines(adapter.OutputStdout, result.Stdout, payload.Name, emit); emitErr != nil {
		return adapter.Completion{}, emitErr
	}
	return adapter.Completion{FinalText: fmt.Sprintf("Herdr agent %s started", payload.Name), NativeSessionID: stringPointer(payload.Name)}, nil
}

func (a Herdr) read(ctx context.Context, runner Runner, path string, request adapter.Request, payload HerdrPayload, emit adapter.Emit) (adapter.Completion, error) {
	source := payload.Source
	if source == "" {
		source = "recent-unwrapped"
	}
	lines := payload.Lines
	if lines == 0 {
		lines = 100
	}
	result, err := runner.Run(ctx, Command{Path: path, Args: []string{"agent", "read", payload.Agent, "--source", source, "--lines", fmt.Sprintf("%d", lines)}, Dir: effectiveDir(request, payload.CWD)})
	if emitErr := emitStderr(result, payload.Agent, emit); emitErr != nil {
		return adapter.Completion{}, emitErr
	}
	if err != nil {
		return adapter.Completion{}, err
	}
	if emitErr := emitLines(adapter.OutputStdout, result.Stdout, payload.Agent, emit); emitErr != nil {
		return adapter.Completion{}, emitErr
	}
	text := strings.TrimSpace(string(result.Stdout))
	if text == "" {
		text = fmt.Sprintf("Herdr agent %s returned no text", payload.Agent)
	}
	return adapter.Completion{FinalText: text, NativeSessionID: stringPointer(payload.Agent)}, nil
}

func (a Herdr) observe(ctx context.Context, runner Runner, path string, request adapter.Request, payload HerdrPayload, emit adapter.Emit) (adapter.Completion, error) {
	status := payload.WaitStatus
	if status == "" {
		status = "idle"
	}
	timeout := payload.TimeoutMS
	if timeout == 0 {
		timeout = 120000
	}
	waited, err := runner.Run(ctx, Command{Path: path, Args: []string{"agent", "wait", payload.Agent, "--status", status, "--timeout", fmt.Sprintf("%d", timeout)}, Dir: effectiveDir(request, payload.CWD)})
	if emitErr := emitStderr(waited, payload.Agent, emit); emitErr != nil {
		return adapter.Completion{}, emitErr
	}
	if err != nil {
		return adapter.Completion{}, err
	}
	payload.Action = "read"
	if payload.Source == "" {
		payload.Source = "recent-unwrapped"
	}
	return a.read(ctx, runner, path, request, payload, emit)
}

package agentadapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"hq/internal/worker/adapter"
)

type Codex struct {
	Runner Runner
	Path   string
}

type CodexPayload struct {
	Action           string `json:"action"`
	Prompt           string `json:"prompt"`
	CWD              string `json:"cwd,omitempty"`
	SessionID        string `json:"session_id,omitempty"`
	OutputPath       string `json:"output_path,omitempty"`
	Sandbox          string `json:"sandbox,omitempty"`
	SkipGitRepoCheck bool   `json:"skip_git_repo_check,omitempty"`
}

func (p CodexPayload) validate() error {
	if p.Action != "exec" && p.Action != "resume" {
		return blocked("invalid_payload", "codex action must be exec or resume")
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return blocked("invalid_payload", "codex prompt is required")
	}
	if p.Action == "resume" && strings.TrimSpace(p.SessionID) == "" {
		return blocked("invalid_payload", "codex resume requires session_id")
	}
	switch p.Sandbox {
	case "", "read-only", "workspace-write", "danger-full-access":
	default:
		return blocked("invalid_payload", "codex sandbox is invalid")
	}
	return nil
}

func (a Codex) Run(ctx context.Context, request adapter.Request, emit adapter.Emit) (adapter.Completion, error) {
	if err := validateAdapterRequest(request, "codex"); err != nil {
		return adapter.Completion{}, err
	}
	var payload CodexPayload
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
		path = "codex"
	}
	cwd := effectiveDir(request, payload.CWD)
	finalPath, err := resolveOutputPath(cwd, payload.OutputPath, safeFilePart(request.RunID)+"-codex.txt")
	if err != nil {
		return adapter.Completion{}, blocked("invalid_output_path", err.Error())
	}
	args := []string{"exec", "--json", "--color", "never", "--output-last-message", finalPath}
	if payload.Sandbox != "" {
		args = append(args, "--sandbox", payload.Sandbox)
	}
	if payload.SkipGitRepoCheck {
		args = append(args, "--skip-git-repo-check")
	}
	if payload.Action == "resume" {
		args = append(args, "resume", payload.SessionID, "-")
	} else {
		args = append(args, "-")
	}
	result, runErr := runner.Run(ctx, Command{Path: path, Args: args, Dir: cwd, Stdin: []byte(payload.Prompt)})
	if emitErr := emitStderr(result, payload.SessionID, emit); emitErr != nil {
		return adapter.Completion{}, emitErr
	}
	sessionID, parseErr := parseCodexEvents(result.Stdout, payload.SessionID, emit)
	if parseErr != nil {
		return adapter.Completion{}, parseErr
	}
	if runErr != nil {
		return adapter.Completion{}, runErr
	}
	finalText, err := readNonEmpty(finalPath)
	if err != nil {
		return adapter.Completion{}, failure("missing_final_answer", err.Error(), false)
	}
	return adapter.Completion{FinalText: finalText, FinalPath: finalPath, NativeSessionID: stringPointer(sessionID)}, nil
}

func parseCodexEvents(raw []byte, expectedSessionID string, emit adapter.Emit) (string, error) {
	sessionID := strings.TrimSpace(expectedSessionID)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return sessionID, failure("protocol_error", "Codex emitted malformed JSONL", false)
		}
		var eventType string
		if json.Unmarshal(event["type"], &eventType) != nil || strings.TrimSpace(eventType) == "" {
			return sessionID, failure("protocol_error", "Codex event is missing type", false)
		}
		if eventType == "thread.started" {
			var started string
			if json.Unmarshal(event["thread_id"], &started) != nil || strings.TrimSpace(started) == "" {
				return sessionID, failure("protocol_error", "Codex thread.started is missing thread_id", false)
			}
			if sessionID != "" && sessionID != started {
				return sessionID, failure("native_session_drift", "Codex resume returned a different thread id", false)
			}
			sessionID = started
		}
		if err := emitLines(adapter.OutputStdout, []byte(line+"\n"), sessionID, emit); err != nil {
			return sessionID, err
		}
		switch eventType {
		case "turn.failed":
			return sessionID, failure("provider_failed", rawErrorMessage(event["error"], "Codex turn failed"), true)
		case "error":
			return sessionID, failure("provider_error", rawString(event["message"], "Codex emitted an error"), true)
		}
	}
	if err := scanner.Err(); err != nil {
		return sessionID, err
	}
	return sessionID, nil
}

func rawString(raw json.RawMessage, fallback string) string {
	var value string
	if json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func rawErrorMessage(raw json.RawMessage, fallback string) string {
	var detail struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &detail) == nil && strings.TrimSpace(detail.Message) != "" {
		return detail.Message
	}
	return rawString(raw, fallback)
}

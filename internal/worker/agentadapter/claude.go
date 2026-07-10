package agentadapter

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"hq/internal/worker/adapter"
)

type Claude struct {
	Runner Runner
	Path   string
}

type ClaudePayload struct {
	Action       string `json:"action"`
	Prompt       string `json:"prompt,omitempty"`
	CWD          string `json:"cwd,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	Name         string `json:"name,omitempty"`
	MaxTurns     int    `json:"max_turns,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
	Bare         bool   `json:"bare,omitempty"`
}

func (p ClaudePayload) validate() error {
	switch p.Action {
	case "print", "background":
		if strings.TrimSpace(p.Prompt) == "" {
			return blocked("invalid_payload", "claude print/background requires prompt")
		}
	case "resume":
		if strings.TrimSpace(p.Prompt) == "" || strings.TrimSpace(p.SessionID) == "" {
			return blocked("invalid_payload", "claude resume requires prompt and session_id")
		}
	case "logs", "attach":
		if strings.TrimSpace(p.SessionID) == "" {
			return blocked("invalid_payload", "claude logs/attach requires session_id")
		}
	default:
		return blocked("invalid_payload", "claude action must be print, resume, background, logs, or attach")
	}
	if p.MaxTurns < 0 {
		return blocked("invalid_payload", "claude max_turns must be non-negative")
	}
	if p.OutputFormat != "" && p.OutputFormat != "json" && p.OutputFormat != "stream-json" {
		return blocked("invalid_payload", "claude output_format must be json or stream-json")
	}
	if (p.Action == "background" || p.Action == "logs" || p.Action == "attach") && p.OutputFormat != "" {
		return blocked("invalid_payload", "claude output_format is only valid for print/resume")
	}
	if p.Name != "" && p.Action != "background" {
		return blocked("invalid_payload", "claude name is only valid for background")
	}
	return nil
}

func (a Claude) Run(ctx context.Context, request adapter.Request, emit adapter.Emit) (adapter.Completion, error) {
	if err := validateAdapterRequest(request, "claude"); err != nil {
		return adapter.Completion{}, err
	}
	var payload ClaudePayload
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
		path = "claude"
	}
	switch payload.Action {
	case "print", "resume":
		return a.print(ctx, runner, path, request, payload, emit)
	case "background":
		return a.background(ctx, runner, path, request, payload, emit)
	case "logs":
		return a.logs(ctx, runner, path, request, payload, emit)
	case "attach":
		return adapter.Completion{FinalText: "Claude attach target recorded", NativeSessionID: stringPointer(payload.SessionID)}, nil
	default:
		return adapter.Completion{}, blocked("invalid_payload", "unsupported Claude action")
	}
}

func (a Claude) print(ctx context.Context, runner Runner, path string, request adapter.Request, payload ClaudePayload, emit adapter.Emit) (adapter.Completion, error) {
	format := payload.OutputFormat
	if format == "" {
		format = "json"
	}
	args := []string{"-p", payload.Prompt, "--output-format", format}
	if format == "stream-json" {
		args = append(args, "--verbose")
	}
	if payload.Action == "resume" {
		args = append(args, "--resume", payload.SessionID)
	}
	if payload.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", payload.MaxTurns))
	}
	if payload.Bare {
		args = append(args, "--bare")
	}
	result, runErr := runner.Run(ctx, Command{Path: path, Args: args, Dir: effectiveDir(request, payload.CWD)})
	if emitErr := emitStderr(result, payload.SessionID, emit); emitErr != nil {
		return adapter.Completion{}, emitErr
	}
	finalText, sessionID, parseErr := parseClaudeOutput(result.Stdout, format, payload.SessionID, emit)
	if parseErr != nil {
		return adapter.Completion{}, parseErr
	}
	if runErr != nil {
		return adapter.Completion{}, runErr
	}
	return adapter.Completion{FinalText: finalText, NativeSessionID: stringPointer(sessionID)}, nil
}

func (a Claude) background(ctx context.Context, runner Runner, path string, request adapter.Request, payload ClaudePayload, emit adapter.Emit) (adapter.Completion, error) {
	sessionID := deterministicUUID(request.RunID)
	args := []string{"--bg", "--session-id", sessionID}
	if payload.Name != "" {
		args = append(args, "--name", payload.Name)
	}
	args = append(args, payload.Prompt)
	result, err := runner.Run(ctx, Command{Path: path, Args: args, Dir: effectiveDir(request, payload.CWD)})
	if emitErr := emitStderr(result, sessionID, emit); emitErr != nil {
		return adapter.Completion{}, emitErr
	}
	if err != nil {
		return adapter.Completion{}, err
	}
	if emitErr := emitLines(adapter.OutputStdout, result.Stdout, sessionID, emit); emitErr != nil {
		return adapter.Completion{}, emitErr
	}
	text := strings.TrimSpace(string(result.Stdout))
	if text == "" {
		text = fmt.Sprintf("Claude background session %s started", sessionID)
	}
	return adapter.Completion{FinalText: text, NativeSessionID: stringPointer(sessionID)}, nil
}

func (a Claude) logs(ctx context.Context, runner Runner, path string, request adapter.Request, payload ClaudePayload, emit adapter.Emit) (adapter.Completion, error) {
	result, err := runner.Run(ctx, Command{Path: path, Args: []string{"logs", payload.SessionID}, Dir: effectiveDir(request, payload.CWD)})
	if emitErr := emitStderr(result, payload.SessionID, emit); emitErr != nil {
		return adapter.Completion{}, emitErr
	}
	if err != nil {
		return adapter.Completion{}, err
	}
	if emitErr := emitLines(adapter.OutputStdout, result.Stdout, payload.SessionID, emit); emitErr != nil {
		return adapter.Completion{}, emitErr
	}
	text := strings.TrimSpace(string(result.Stdout))
	if text == "" {
		return adapter.Completion{}, failure("missing_logs", "Claude logs returned no text", true)
	}
	return adapter.Completion{FinalText: text, NativeSessionID: stringPointer(payload.SessionID)}, nil
}

func parseClaudeOutput(raw []byte, format, expectedSessionID string, emit adapter.Emit) (string, string, error) {
	if format == "stream-json" {
		return parseClaudeStream(raw, expectedSessionID, emit)
	}
	var response struct {
		Result    string `json:"result"`
		SessionID string `json:"session_id"`
		IsError   bool   `json:"is_error"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", expectedSessionID, failure("protocol_error", "Claude emitted malformed JSON", false)
	}
	sessionID := strings.TrimSpace(response.SessionID)
	if expectedSessionID != "" {
		if sessionID != "" && sessionID != expectedSessionID {
			return "", expectedSessionID, failure("native_session_drift", "Claude resume returned a different session id", false)
		}
		sessionID = expectedSessionID
	}
	if err := emitLines(adapter.OutputStdout, append(bytes.TrimSpace(raw), '\n'), sessionID, emit); err != nil {
		return "", sessionID, err
	}
	text := strings.TrimSpace(response.Result)
	if response.IsError {
		if text == "" {
			text = "Claude reported an error"
		}
		return "", sessionID, failure("provider_error", text, true)
	}
	if text == "" {
		return "", sessionID, failure("missing_final_answer", "Claude JSON result is empty", false)
	}
	return text, sessionID, nil
}

func parseClaudeStream(raw []byte, expectedSessionID string, emit adapter.Emit) (string, string, error) {
	sessionID := strings.TrimSpace(expectedSessionID)
	finalText := ""
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return "", sessionID, failure("protocol_error", "Claude emitted malformed stream-json", false)
		}
		if rawID, ok := event["session_id"]; ok {
			var observed string
			if json.Unmarshal(rawID, &observed) == nil && strings.TrimSpace(observed) != "" {
				if sessionID != "" && sessionID != observed {
					return "", sessionID, failure("native_session_drift", "Claude stream changed session id", false)
				}
				sessionID = observed
			}
		}
		if err := emitLines(adapter.OutputStdout, []byte(line+"\n"), sessionID, emit); err != nil {
			return "", sessionID, err
		}
		var eventType string
		_ = json.Unmarshal(event["type"], &eventType)
		if eventType == "result" {
			var isError bool
			_ = json.Unmarshal(event["is_error"], &isError)
			finalText = strings.TrimSpace(rawString(event["result"], ""))
			if isError {
				if finalText == "" {
					finalText = "Claude reported an error"
				}
				return "", sessionID, failure("provider_error", finalText, true)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", sessionID, err
	}
	if finalText == "" {
		return "", sessionID, failure("missing_final_answer", "Claude stream-json contained no final result", false)
	}
	return finalText, sessionID, nil
}

func deterministicUUID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	bytes := sum[:16]
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}

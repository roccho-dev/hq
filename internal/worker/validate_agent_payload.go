package worker

import (
	"encoding/json"
	"fmt"
	"strings"
)

func validateAgentPayload(target string, payload map[string]json.RawMessage) []Diagnostic {
	switch target {
	case "herdr":
		return validateHerdrInstructionPayload(payload)
	case "codex":
		return validateCodexInstructionPayload(payload)
	case "claude":
		return validateClaudeInstructionPayload(payload)
	default:
		return []Diagnostic{invalid("invalid_payload", "payload", "target has no payload contract")}
	}
}

func validateHerdrInstructionPayload(payload map[string]json.RawMessage) []Diagnostic {
	action, ok := requiredString(payload, "action")
	if !ok {
		return payloadError("action", "action must be a non-empty string")
	}
	allowed := map[string]struct{}{"action": {}, "cwd": {}}
	switch action {
	case "start":
		addAllowed(allowed, "name", "no_focus", "split", "command_argv", "prompt")
		if _, ok := requiredString(payload, "name"); !ok {
			return payloadError("name", "name must be a non-empty string")
		}
		if !validStringSlice(payload["command_argv"]) {
			return payloadError("command_argv", "command_argv must be a non-empty array of non-empty strings")
		}
		if _, ok := requiredString(payload, "prompt"); !ok {
			return payloadError("prompt", "prompt must be a non-empty string")
		}
		if err := optionalBool(payload, "no_focus"); err != "" {
			return payloadError("no_focus", err)
		}
		if value, present, valid := optionalStringValue(payload, "split"); !valid || (present && value != "right" && value != "down") {
			return payloadError("split", "split must be right or down")
		}
	case "read":
		addAllowed(allowed, "agent", "source", "lines")
		if _, ok := requiredString(payload, "agent"); !ok {
			return payloadError("agent", "agent must be a non-empty string")
		}
		if value, present, valid := optionalStringValue(payload, "source"); !valid || (present && value != "visible" && value != "recent" && value != "recent-unwrapped" && value != "detection") {
			return payloadError("source", "source is invalid")
		}
		if err := optionalPositiveInt(payload, "lines"); err != "" {
			return payloadError("lines", err)
		}
	case "observe":
		addAllowed(allowed, "agent", "source", "lines", "wait_status", "timeout_ms")
		if _, ok := requiredString(payload, "agent"); !ok {
			return payloadError("agent", "agent must be a non-empty string")
		}
		if value, present, valid := optionalStringValue(payload, "source"); !valid || (present && value != "visible" && value != "recent" && value != "recent-unwrapped" && value != "detection") {
			return payloadError("source", "source is invalid")
		}
		if value, present, valid := optionalStringValue(payload, "wait_status"); !valid || (present && value != "idle" && value != "working" && value != "blocked" && value != "unknown") {
			return payloadError("wait_status", "wait_status is invalid")
		}
		if err := optionalPositiveInt(payload, "lines"); err != "" {
			return payloadError("lines", err)
		}
		if err := optionalPositiveInt(payload, "timeout_ms"); err != "" {
			return payloadError("timeout_ms", err)
		}
	case "attach":
		addAllowed(allowed, "agent", "takeover")
		if _, ok := requiredString(payload, "agent"); !ok {
			return payloadError("agent", "agent must be a non-empty string")
		}
		if err := optionalBool(payload, "takeover"); err != "" {
			return payloadError("takeover", err)
		}
	default:
		return payloadError("action", "action must be start, read, observe, or attach")
	}
	if err := optionalString(payload, "cwd"); err != "" {
		return payloadError("cwd", err)
	}
	return rejectPayloadUnknown(payload, allowed)
}

func validateCodexInstructionPayload(payload map[string]json.RawMessage) []Diagnostic {
	action, ok := requiredString(payload, "action")
	if !ok || (action != "exec" && action != "resume") {
		return payloadError("action", "action must be exec or resume")
	}
	allowed := map[string]struct{}{"action": {}, "prompt": {}, "cwd": {}, "session_id": {}, "output_path": {}, "sandbox": {}, "skip_git_repo_check": {}}
	if _, ok := requiredString(payload, "prompt"); !ok {
		return payloadError("prompt", "prompt must be a non-empty string")
	}
	if action == "resume" {
		if _, ok := requiredString(payload, "session_id"); !ok {
			return payloadError("session_id", "session_id must be a non-empty string for resume")
		}
	} else if _, exists := payload["session_id"]; exists {
		return payloadError("session_id", "session_id is only valid for resume")
	}
	for _, key := range []string{"cwd", "output_path"} {
		if err := optionalString(payload, key); err != "" {
			return payloadError(key, err)
		}
	}
	if value, present, valid := optionalStringValue(payload, "sandbox"); !valid || (present && value != "read-only" && value != "workspace-write" && value != "danger-full-access") {
		return payloadError("sandbox", "sandbox is invalid")
	}
	if err := optionalBool(payload, "skip_git_repo_check"); err != "" {
		return payloadError("skip_git_repo_check", err)
	}
	return rejectPayloadUnknown(payload, allowed)
}

func validateClaudeInstructionPayload(payload map[string]json.RawMessage) []Diagnostic {
	action, ok := requiredString(payload, "action")
	if !ok {
		return payloadError("action", "action must be a non-empty string")
	}
	allowed := map[string]struct{}{"action": {}, "cwd": {}}
	switch action {
	case "print":
		addAllowed(allowed, "prompt", "max_turns", "output_format", "bare")
		if _, ok := requiredString(payload, "prompt"); !ok {
			return payloadError("prompt", "prompt must be a non-empty string")
		}
	case "resume":
		addAllowed(allowed, "prompt", "session_id", "max_turns", "output_format", "bare")
		if _, ok := requiredString(payload, "prompt"); !ok {
			return payloadError("prompt", "prompt must be a non-empty string")
		}
		if _, ok := requiredString(payload, "session_id"); !ok {
			return payloadError("session_id", "session_id must be a non-empty string")
		}
	case "background":
		addAllowed(allowed, "prompt", "name")
		if _, ok := requiredString(payload, "prompt"); !ok {
			return payloadError("prompt", "prompt must be a non-empty string")
		}
		if err := optionalString(payload, "name"); err != "" {
			return payloadError("name", err)
		}
	case "logs", "attach":
		addAllowed(allowed, "session_id")
		if _, ok := requiredString(payload, "session_id"); !ok {
			return payloadError("session_id", "session_id must be a non-empty string")
		}
	default:
		return payloadError("action", "action must be print, resume, background, logs, or attach")
	}
	if err := optionalString(payload, "cwd"); err != "" {
		return payloadError("cwd", err)
	}
	if action == "print" || action == "resume" {
		if err := optionalPositiveInt(payload, "max_turns"); err != "" {
			return payloadError("max_turns", err)
		}
		if value, present, valid := optionalStringValue(payload, "output_format"); !valid || (present && value != "json" && value != "stream-json") {
			return payloadError("output_format", "output_format must be json or stream-json")
		}
		if err := optionalBool(payload, "bare"); err != "" {
			return payloadError("bare", err)
		}
	}
	return rejectPayloadUnknown(payload, allowed)
}

func payloadError(field, message string) []Diagnostic {
	return []Diagnostic{invalid("invalid_payload", "payload."+field, message)}
}

func addAllowed(allowed map[string]struct{}, keys ...string) {
	for _, key := range keys {
		allowed[key] = struct{}{}
	}
}

func rejectPayloadUnknown(payload map[string]json.RawMessage, allowed map[string]struct{}) []Diagnostic {
	for key := range payload {
		if _, ok := allowed[key]; !ok {
			return payloadError(key, "unsupported target payload field")
		}
	}
	return nil
}

func requiredString(payload map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := payload[key]
	if !ok {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

func optionalString(payload map[string]json.RawMessage, key string) string {
	_, present, valid := optionalStringValue(payload, key)
	if present && !valid {
		return fmt.Sprintf("%s must be a non-empty string", key)
	}
	return ""
}

func optionalStringValue(payload map[string]json.RawMessage, key string) (string, bool, bool) {
	raw, present := payload[key]
	if !present {
		return "", false, true
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || strings.TrimSpace(value) == "" {
		return "", true, false
	}
	return value, true, true
}

func optionalBool(payload map[string]json.RawMessage, key string) string {
	raw, present := payload[key]
	if !present {
		return ""
	}
	var value bool
	if json.Unmarshal(raw, &value) != nil {
		return fmt.Sprintf("%s must be a boolean", key)
	}
	return ""
}

func optionalPositiveInt(payload map[string]json.RawMessage, key string) string {
	raw, present := payload[key]
	if !present {
		return ""
	}
	var value int
	if json.Unmarshal(raw, &value) != nil || value <= 0 {
		return fmt.Sprintf("%s must be a positive integer", key)
	}
	return ""
}

func validStringSlice(raw json.RawMessage) bool {
	var values []string
	if json.Unmarshal(raw, &values) != nil || len(values) == 0 {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

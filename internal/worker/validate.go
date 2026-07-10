package worker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Contract struct {
	Version string
	Targets map[string]struct{}
}

func DefaultContract() Contract {
	return Contract{Version: InstructionVersionV1, Targets: map[string]struct{}{"sh": {}, "herdr": {}, "codex": {}, "claude": {}, "host": {}}}
}

func (c Contract) Validate(row ReadRow) []Diagnostic {
	if row.ParseError != nil {
		return []Diagnostic{*row.ParseError}
	}
	inst := row.Instruction
	var out []Diagnostic
	if strings.TrimSpace(inst.ID) == "" {
		out = append(out, invalid("invalid_id", "id", "id must be a non-empty string"))
	}
	if inst.Version != c.Version {
		out = append(out, invalid("unknown_version", "version", fmt.Sprintf("version must be %q", c.Version)))
	}
	if inst.Op != "run" {
		out = append(out, invalid("unknown_op", "op", "op must be run in instruction.v1"))
	}
	if _, ok := c.Targets[inst.Target]; !ok {
		out = append(out, invalid("unknown_target", "target", "target must be sh, herdr, codex, claude, or host"))
	}
	if !isRFC3339UTC(inst.CreatedAt) {
		out = append(out, invalid("invalid_created_at", "created_at", "created_at must be RFC3339 UTC ending in Z"))
	}
	if inst.Reason != nil && strings.TrimSpace(*inst.Reason) == "" {
		out = append(out, invalid("invalid_reason", "reason", "reason must be non-empty when present"))
	}
	if inst.ReplyTo != nil && strings.TrimSpace(*inst.ReplyTo) == "" {
		out = append(out, invalid("invalid_reply_to", "reply_to", "reply_to must be non-empty when present"))
	}
	for i, label := range inst.Labels {
		if strings.TrimSpace(label) == "" {
			out = append(out, invalid("invalid_labels", fmt.Sprintf("labels[%d]", i), "labels must contain only non-empty strings"))
		}
	}
	if len(inst.Policy) != 0 {
		var policy map[string]json.RawMessage
		if json.Unmarshal(inst.Policy, &policy) != nil || policy == nil || len(policy) != 0 {
			out = append(out, invalid("unsupported_policy", "policy", "policy is reserved and must be an empty object in instruction.v1"))
		}
	}
	out = append(out, validatePayload(inst.Target, inst.Payload)...)
	for _, field := range row.UnknownFields {
		out = append(out, invalid("unknown_field", field, "top-level field is not part of instruction.v1"))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Field == out[j].Field {
			return out[i].Code < out[j].Code
		}
		return out[i].Field < out[j].Field
	})
	return out
}

func (c Contract) ValidateRows(rows []ReadRow) map[int][]Diagnostic {
	out := make(map[int][]Diagnostic, len(rows))
	seen := map[string]int{}
	for i, row := range rows {
		diags := c.Validate(row)
		if len(diags) == 0 {
			if first, ok := seen[row.Instruction.ID]; ok {
				diags = append(diags, Diagnostic{Code: "duplicate_id", Field: "id", Message: fmt.Sprintf("instruction id duplicates line %d", rows[first].Source.Line)})
			} else {
				seen[row.Instruction.ID] = i
			}
		}
		out[i] = diags
	}
	return out
}

func validatePayload(target string, raw json.RawMessage) []Diagnostic {
	trimmed := bytes.TrimSpace(raw)
	var payload map[string]json.RawMessage
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || json.Unmarshal(trimmed, &payload) != nil || payload == nil {
		return []Diagnostic{invalid("invalid_payload", "payload", "payload must be a JSON object")}
	}
	switch target {
	case "herdr", "codex", "claude":
		return validateAgentPayload(target, payload)
	case "host":
		return validateHostPayload(payload)
	case "sh":
	default:
		return nil
	}
	allowed := map[string]struct{}{"cwd": {}, "argv": {}}
	for key := range payload {
		if _, ok := allowed[key]; !ok {
			return []Diagnostic{invalid("invalid_payload", "payload."+key, "unsupported target payload field")}
		}
	}
	var argv []string
	if json.Unmarshal(payload["argv"], &argv) != nil || len(argv) == 0 {
		return []Diagnostic{invalid("invalid_payload", "payload.argv", "argv must be a non-empty array of non-empty strings")}
	}
	for _, arg := range argv {
		if strings.TrimSpace(arg) == "" {
			return []Diagnostic{invalid("invalid_payload", "payload.argv", "argv must contain only non-empty strings")}
		}
	}
	if rawCWD, ok := payload["cwd"]; ok {
		var cwd string
		if json.Unmarshal(rawCWD, &cwd) != nil || strings.TrimSpace(cwd) == "" {
			return []Diagnostic{invalid("invalid_payload", "payload.cwd", "cwd must be a non-empty string")}
		}
	}
	return nil
}

func validateHostPayload(payload map[string]json.RawMessage) []Diagnostic {
	allowed := map[string]struct{}{"capability": {}, "path": {}}
	for key := range payload {
		if _, ok := allowed[key]; !ok {
			return []Diagnostic{invalid("invalid_payload", "payload."+key, "unsupported host payload field")}
		}
	}
	var capabilityID string
	if json.Unmarshal(payload["capability"], &capabilityID) != nil || capabilityID != "host.open" {
		return []Diagnostic{invalid("invalid_payload", "payload.capability", "capability must be host.open")}
	}
	var path string
	if json.Unmarshal(payload["path"], &path) != nil || strings.TrimSpace(path) == "" {
		return []Diagnostic{invalid("invalid_payload", "payload.path", "path must be a non-empty string")}
	}
	return nil
}

func invalid(code, field, message string) Diagnostic {
	return Diagnostic{Code: code, Field: field, Message: message}
}

func isRFC3339UTC(value string) bool {
	if !strings.HasSuffix(value, "Z") || strings.TrimSpace(value) == "" {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

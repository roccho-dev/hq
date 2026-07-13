package worker

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Policy struct {
	WorkspaceRoot  string
	AllowedTargets map[string]struct{}
	DeniedTargets  map[string]struct{}
	DeniedOps      map[string]struct{}
	AllowedEnv     map[string]struct{}
	Environment    map[string]string
	DefaultTimeout time.Duration
	MaxTimeout     time.Duration
}

func DefaultPolicy(workspaceRoot string) Policy {
	return Policy{
		WorkspaceRoot:  workspaceRoot,
		AllowedTargets: map[string]struct{}{"sh": {}, "herdr": {}, "codex": {}, "claude": {}, "host": {}, "local-tool": {}},
		DeniedTargets:  map[string]struct{}{},
		DeniedOps:      map[string]struct{}{},
		AllowedEnv:     map[string]struct{}{"CI": {}, "LANG": {}, "LC_ALL": {}, "NO_COLOR": {}, "TERM": {}},
		Environment:    map[string]string{},
		DefaultTimeout: 10 * time.Minute,
		MaxTimeout:     60 * time.Minute,
	}
}

func (p Policy) Evaluate(inst Instruction) PolicyDecision {
	deny := func(code, message string) PolicyDecision {
		return PolicyDecision{Allowed: false, Code: code, Message: message}
	}
	if _, denied := p.DeniedTargets[inst.Target]; denied {
		return deny("target_denied", "target is explicitly denied")
	}
	if _, allowed := p.AllowedTargets[inst.Target]; !allowed {
		return deny("target_not_allowed", "target is not allowlisted")
	}
	if _, denied := p.DeniedOps[inst.Op]; denied {
		return deny("op_denied", "operation is explicitly denied")
	}
	if p.DefaultTimeout <= 0 {
		return deny("invalid_timeout_default", "default timeout must be positive")
	}
	if p.MaxTimeout > 0 && p.DefaultTimeout > p.MaxTimeout {
		return deny("timeout_exceeds_max", fmt.Sprintf("default timeout exceeds maximum of %d seconds", int64(p.MaxTimeout/time.Second)))
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(inst.Payload, &payload); err != nil || payload == nil {
		return deny("invalid_payload", "payload must be valid before policy evaluation")
	}
	root, err := filepath.Abs(p.WorkspaceRoot)
	if err != nil {
		return deny("invalid_workspace", err.Error())
	}
	root = filepath.Clean(root)
	effective := root
	if raw, ok := payload["cwd"]; ok {
		var cwd string
		if err := json.Unmarshal(raw, &cwd); err != nil || strings.TrimSpace(cwd) == "" {
			return deny("invalid_cwd", "payload.cwd must be a non-empty string")
		}
		if filepath.IsAbs(cwd) {
			effective = filepath.Clean(cwd)
		} else {
			effective = filepath.Clean(filepath.Join(root, cwd))
		}
	}
	rel, err := filepath.Rel(root, effective)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return deny("cwd_outside_workspace", "payload.cwd escapes the configured workspace")
	}

	envKeys := make([]string, 0, len(p.Environment))
	for key := range p.Environment {
		if _, ok := p.AllowedEnv[key]; !ok {
			return deny("env_not_allowed", "environment key is not allowlisted: "+key)
		}
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	return PolicyDecision{
		Allowed:         true,
		Code:            "allowed",
		Message:         "instruction is within configured worker bounds",
		EffectiveCWD:    effective,
		TimeoutSeconds:  int64(p.DefaultTimeout / time.Second),
		EnvironmentKeys: envKeys,
	}
}

func SummarizePayload(raw json.RawMessage) PayloadSummary {
	summary := PayloadSummary{Bytes: len(raw)}
	var payload map[string]json.RawMessage
	if json.Unmarshal(raw, &payload) == nil && payload != nil {
		for key := range payload {
			summary.Keys = append(summary.Keys, key)
		}
		sort.Strings(summary.Keys)
	}
	return summary
}

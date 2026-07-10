// Package hostopen invokes one envs-verified host.open provider directly.
// It never constructs shell text and never searches PATH.
package hostopen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"hq/internal/capability"
	"hq/internal/worker/adapter"
)

type Adapter struct { binding capability.Binding }

func New(binding capability.Binding) (*Adapter, error) {
	if err := binding.Validate(binding.DeploymentID); err != nil { return nil, err }
	if err := binding.VerifyExecutable(); err != nil { return nil, err }
	return &Adapter{binding: binding}, nil
}

func (a *Adapter) Run(ctx context.Context, request adapter.Request, _ adapter.Emit) (adapter.Completion, error) {
	if a == nil { return adapter.Completion{}, errors.New("host.open adapter is nil") }
	if err := request.Validate(); err != nil { return adapter.Completion{}, err }
	if request.Target != "host" || request.Operation != "run" { return adapter.Completion{}, adapter.NewBlockedError("host_contract_mismatch", "host.open requires target=host and op=run") }
	payload, err := decodePayload(request.Payload)
	if err != nil { return adapter.Completion{}, adapter.NewBlockedError("invalid_host_open_payload", err.Error()) }
	if payload.Capability != capability.HostOpenCapability { return adapter.Completion{}, adapter.NewBlockedError("capability_mismatch", "host payload capability must be host.open") }
	if !filepath.IsAbs(payload.Path) { return adapter.Completion{}, adapter.NewBlockedError("path_not_absolute", "host.open path must be absolute") }
	if _, err := os.Stat(payload.Path); err != nil { return adapter.Completion{}, adapter.NewBlockedError("path_unavailable", err.Error()) }
	if err := a.binding.VerifyExecutable(); err != nil { return adapter.Completion{}, adapter.NewBlockedError("provider_integrity_failed", err.Error()) }
	args := []string{payload.Path}
	if request.IdempotencyKey != "" {
		if a.binding.IdempotencyContract == "" { return adapter.Completion{}, adapter.NewBlockedError("idempotency_unsupported", "provider does not declare an idempotency contract") }
		args = append([]string{"--hq-idempotency-key", request.IdempotencyKey, "--"}, args...)
	}
	command := exec.CommandContext(ctx, a.binding.ExecutablePath, args...)
	command.Dir = request.CWD
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout; command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" { message = err.Error() }
		return adapter.Completion{}, &adapter.FailureError{Class: adapter.FailureFailed, Code: "provider_failed", Message: message}
	}
	return adapter.Completion{FinalText: fmt.Sprintf("host.open completed via %s", a.binding.ProviderID), FinalPath: payload.Path}, nil
}

type payload struct { Capability string `json:"capability"`; Path string `json:"path"` }
func decodePayload(raw json.RawMessage) (payload, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw)); decoder.DisallowUnknownFields()
	var value payload
	if err := decoder.Decode(&value); err != nil { return payload{}, err }
	if strings.TrimSpace(value.Path) == "" { return payload{}, errors.New("path is required") }
	return value, nil
}

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

type Adapter struct{ binding capability.Binding }

func New(b capability.Binding) (*Adapter, error) {
	if err := b.Validate(b.DeploymentID); err != nil {
		return nil, err
	}
	if err := b.VerifyExecutable(); err != nil {
		return nil, err
	}
	return &Adapter{b}, nil
}

func (a *Adapter) Run(ctx context.Context, r adapter.Request, _ adapter.Emit) (adapter.Completion, error) {
	if a == nil {
		return adapter.Completion{}, errors.New("host.open adapter is nil")
	}
	if err := r.Validate(); err != nil {
		return adapter.Completion{}, err
	}
	if r.Target != "host" || r.Operation != "run" {
		return adapter.Completion{}, adapter.NewBlockedError("host_contract_mismatch", "host.open requires target=host and op=run")
	}
	p, err := decodePayload(r.Payload)
	if err != nil {
		return adapter.Completion{}, adapter.NewBlockedError("invalid_host_open_payload", err.Error())
	}
	if p.Capability != capability.HostOpenCapability {
		return adapter.Completion{}, adapter.NewBlockedError("capability_mismatch", "host payload capability must be host.open")
	}
	if !filepath.IsAbs(p.Path) {
		return adapter.Completion{}, adapter.NewBlockedError("path_not_absolute", "host.open path must be absolute")
	}
	nativePath := filepath.Clean(p.Path)
	if _, err := os.Stat(nativePath); err != nil {
		return adapter.Completion{}, adapter.NewBlockedError("path_unavailable", err.Error())
	}
	if err := a.binding.VerifyExecutable(); err != nil {
		return adapter.Completion{}, adapter.NewBlockedError("provider_integrity_failed", err.Error())
	}
	args := []string{nativePath}
	if r.IdempotencyKey != "" {
		if a.binding.IdempotencyContract == "" {
			return adapter.Completion{}, adapter.NewBlockedError("idempotency_unsupported", "provider does not declare an idempotency contract")
		}
		args = append([]string{"--hq-idempotency-key", r.IdempotencyKey, "--"}, args...)
	}
	if err := ctx.Err(); err != nil {
		return adapter.Completion{}, err
	}
	// host.open.v1 is a one-way GUI launch/handoff contract. Once Start
	// succeeds, waiting for process exit would conflate a successful OS handoff
	// with a provider's later exit status.
	cmd := exec.Command(a.binding.ExecutablePath, args...)
	cmd.Dir = r.CWD
	if err := cmd.Start(); err != nil {
		return adapter.Completion{}, &adapter.FailureError{Class: adapter.FailureFailed, Code: "provider_failed", Message: err.Error()}
	}
	releaseWarning := ""
	if err := cmd.Process.Release(); err != nil {
		// Start already crossed the effect boundary. Handle-release failure is
		// diagnostic only and must not claim that provider launch failed.
		releaseWarning = fmt.Sprintf("; process release warning: %v", err)
	}
	return adapter.Completion{
		FinalText: fmt.Sprintf("host.open launch handed off via %s%s", a.binding.ProviderID, releaseWarning),
		FinalPath: nativePath,
	}, nil
}

type payload struct {
	Capability string `json:"capability"`
	Path       string `json:"path"`
}

func decodePayload(raw json.RawMessage) (payload, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var p payload
	if err := d.Decode(&p); err != nil {
		return payload{}, err
	}
	if strings.TrimSpace(p.Path) == "" {
		return payload{}, errors.New("path is required")
	}
	return p, nil
}

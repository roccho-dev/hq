package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Adapter interface {
	Run(context.Context, Request, Emit) (Completion, error)
}

// Preparer resolves a request-specific provider without starting it. This is
// the only dynamic step permitted before the worker records durable started
// evidence.
type Preparer interface {
	Prepare(context.Context, Request) (Prepared, error)
}

type PreparerFunc func(context.Context, Request) (Prepared, error)

func (f PreparerFunc) Prepare(ctx context.Context, request Request) (Prepared, error) {
	return f(ctx, request)
}

// Prepared binds the exact transient adapter and verified provider selected
// for one request. Provider may be nil only for legacy/static registrations.
type Prepared struct {
	Adapter  Adapter
	Provider *ProviderDescriptor
}

type Emit func(Output) error
type Request struct {
	RunID          string
	InstructionID  string
	Target         string
	Operation      string
	Payload        json.RawMessage
	CWD            string
	IdempotencyKey string
}

var canonicalTargets = map[string]struct{}{"sh": {}, "herdr": {}, "codex": {}, "claude": {}, "host": {}, "local-tool": {}}

func IsCanonicalTarget(t string) bool { _, ok := canonicalTargets[t]; return ok }
func (r Request) Validate() error {
	if strings.TrimSpace(r.RunID) == "" {
		return errors.New("run_id is required")
	}
	if strings.TrimSpace(r.InstructionID) == "" {
		return errors.New("instruction_id is required")
	}
	if !IsCanonicalTarget(r.Target) {
		return fmt.Errorf("target %q is not part of instruction.v1", r.Target)
	}
	if strings.TrimSpace(r.Operation) == "" {
		return errors.New("operation is required")
	}
	if len(r.Payload) == 0 || !json.Valid(r.Payload) {
		return errors.New("payload must be valid JSON")
	}
	return nil
}

type ProviderDescriptor struct {
	CapabilityID        string `json:"capability_id"`
	ProviderID          string `json:"provider_id"`
	ContractVersion     string `json:"contract_version"`
	DeploymentID        string `json:"deployment_id"`
	ProviderKind        string `json:"provider_kind"`
	IntegrityDigest     string `json:"integrity_digest"`
	IdempotencyContract string `json:"idempotency_contract,omitempty"`
}

func (d ProviderDescriptor) Validate() error {
	for f, v := range map[string]string{"capability_id": d.CapabilityID, "provider_id": d.ProviderID, "contract_version": d.ContractVersion, "deployment_id": d.DeploymentID, "provider_kind": d.ProviderKind, "integrity_digest": d.IntegrityDigest} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("%s is required", f)
		}
	}
	return nil
}
func (d ProviderDescriptor) Equal(o ProviderDescriptor) bool {
	return d.CapabilityID == o.CapabilityID && d.ProviderID == o.ProviderID && d.ContractVersion == o.ContractVersion && d.DeploymentID == o.DeploymentID && d.ProviderKind == o.ProviderKind && d.IntegrityDigest == o.IntegrityDigest && d.IdempotencyContract == o.IdempotencyContract
}

type OutputKind string

const (
	OutputStdout OutputKind = "stdout"
	OutputStderr OutputKind = "stderr"
)

type Output struct {
	Kind            OutputKind
	Message         string
	NativeSessionID *string
}

func (o Output) Validate() error {
	if o.Kind != OutputStdout && o.Kind != OutputStderr {
		return fmt.Errorf("unknown output kind %q", o.Kind)
	}
	return validateNativeSessionID(o.NativeSessionID)
}

type Completion struct {
	FinalText       string
	FinalPath       string
	NativeSessionID *string
}

func (c Completion) Validate() error {
	if strings.TrimSpace(c.FinalText) == "" && strings.TrimSpace(c.FinalPath) == "" {
		return errors.New("completion requires final text and/or path")
	}
	return validateNativeSessionID(c.NativeSessionID)
}
func validateNativeSessionID(v *string) error {
	if v != nil && strings.TrimSpace(*v) == "" {
		return errors.New("native_session_id cannot be empty")
	}
	return nil
}

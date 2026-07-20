package adapter

import (
	"context"
	"encoding/hex"
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
	Adapter         Adapter
	Provider        *ProviderDescriptor
	RunViewRequired bool
}

// RunViewGateway projects one already-accepted run into a replaceable native
// view. The canonical result log remains authoritative.
type RunViewGateway interface {
	Open(context.Context, Request) (*RunView, error)
}

type RunView struct {
	Policy          string
	Provider        ProviderDescriptor
	NativeSessionID string
}

func (v RunView) Validate() error {
	if v.Policy != "required" {
		return errors.New("run view policy must be required")
	}
	if err := v.Provider.Validate(); err != nil {
		return fmt.Errorf("run view provider: %w", err)
	}
	if strings.TrimSpace(v.NativeSessionID) == "" {
		return errors.New("run view native_session_id is required")
	}
	return nil
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
	CapabilityID        string                         `json:"capability_id"`
	ProviderID          string                         `json:"provider_id"`
	ContractVersion     string                         `json:"contract_version"`
	DeploymentID        string                         `json:"deployment_id"`
	ProviderKind        string                         `json:"provider_kind"`
	IntegrityDigest     string                         `json:"integrity_digest"`
	ConfigurationDigest string                         `json:"configuration_digest,omitempty"`
	Dependencies        []ProviderDependencyDescriptor `json:"dependencies,omitempty"`
	IdempotencyContract string                         `json:"idempotency_contract,omitempty"`
}

type ProviderDependencyDescriptor struct {
	Name                string `json:"name"`
	ProviderID          string `json:"provider_id"`
	ContractVersion     string `json:"contract_version"`
	DeploymentID        string `json:"deployment_id"`
	ProviderKind        string `json:"provider_kind"`
	IntegrityDigest     string `json:"integrity_digest"`
	ConfigurationDigest string `json:"configuration_digest,omitempty"`
}

func (d ProviderDescriptor) Validate() error {
	for field, value := range map[string]string{"capability_id": d.CapabilityID, "provider_id": d.ProviderID, "contract_version": d.ContractVersion, "deployment_id": d.DeploymentID, "provider_kind": d.ProviderKind, "integrity_digest": d.IntegrityDigest} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	if err := validateOptionalDigest(d.ConfigurationDigest); err != nil {
		return fmt.Errorf("configuration_digest %w", err)
	}
	seenDependencies := map[string]struct{}{}
	for index, dependency := range d.Dependencies {
		if err := dependency.Validate(); err != nil {
			return fmt.Errorf("dependency %d: %w", index+1, err)
		}
		if _, duplicate := seenDependencies[dependency.Name]; duplicate {
			return fmt.Errorf("duplicate provider dependency name %q", dependency.Name)
		}
		seenDependencies[dependency.Name] = struct{}{}
	}
	return nil
}

func (d ProviderDependencyDescriptor) Validate() error {
	for field, value := range map[string]string{"name": d.Name, "provider_id": d.ProviderID, "contract_version": d.ContractVersion, "deployment_id": d.DeploymentID, "provider_kind": d.ProviderKind, "integrity_digest": d.IntegrityDigest} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	if !validDependencyName(d.Name) {
		return errors.New("name is invalid")
	}
	if err := validateOptionalDigest(d.ConfigurationDigest); err != nil {
		return fmt.Errorf("configuration_digest %w", err)
	}
	return nil
}

func (d ProviderDescriptor) Equal(other ProviderDescriptor) bool {
	if d.CapabilityID != other.CapabilityID || d.ProviderID != other.ProviderID || d.ContractVersion != other.ContractVersion || d.DeploymentID != other.DeploymentID || d.ProviderKind != other.ProviderKind || d.IntegrityDigest != other.IntegrityDigest || d.ConfigurationDigest != other.ConfigurationDigest || d.IdempotencyContract != other.IdempotencyContract || len(d.Dependencies) != len(other.Dependencies) {
		return false
	}
	for index := range d.Dependencies {
		if d.Dependencies[index] != other.Dependencies[index] {
			return false
		}
	}
	return true
}

func validDependencyName(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func validateOptionalDigest(value string) error {
	if value == "" {
		return nil
	}
	hexDigest := strings.TrimPrefix(value, "sha256:")
	if !strings.HasPrefix(value, "sha256:") || len(hexDigest) != 64 || hexDigest != strings.ToLower(hexDigest) {
		return errors.New("must be sha256:<64 lowercase hex characters>")
	}
	if _, err := hex.DecodeString(hexDigest); err != nil {
		return errors.New("contains invalid hexadecimal data")
	}
	return nil
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

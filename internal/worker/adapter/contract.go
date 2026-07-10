// Package adapter defines the transient boundary between worker core and
// target-specific executors. It defines no durable event row.
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Adapter executes one already-validated request. It may stream transient
// Output values and return one transient Completion or an error.
type Adapter interface { Run(context.Context, Request, Emit) (Completion, error) }

type Emit func(Output) error

type Request struct {
	RunID string
	InstructionID string
	Target string
	Operation string
	Payload json.RawMessage
	CWD string
	IdempotencyKey string
}

var canonicalTargets = map[string]struct{}{
	"sh": {}, "herdr": {}, "codex": {}, "claude": {}, "host": {},
}

func IsCanonicalTarget(target string) bool { _, ok := canonicalTargets[target]; return ok }

func (r Request) Validate() error {
	if strings.TrimSpace(r.RunID) == "" { return errors.New("run_id is required") }
	if strings.TrimSpace(r.InstructionID) == "" { return errors.New("instruction_id is required") }
	if !IsCanonicalTarget(r.Target) { return fmt.Errorf("target %q is not part of instruction.v1", r.Target) }
	if strings.TrimSpace(r.Operation) == "" { return errors.New("operation is required") }
	if len(r.Payload) == 0 || !json.Valid(r.Payload) { return errors.New("payload must be valid JSON") }
	return nil
}

type ProviderDescriptor struct {
	CapabilityID string `json:"capability_id"`
	ProviderID string `json:"provider_id"`
	ContractVersion string `json:"contract_version"`
	DeploymentID string `json:"deployment_id"`
	ProviderKind string `json:"provider_kind"`
	IntegrityDigest string `json:"integrity_digest"`
	IdempotencyContract string `json:"idempotency_contract,omitempty"`
}

func (d ProviderDescriptor) Validate() error {
	for field, value := range map[string]string{
		"capability_id": d.CapabilityID, "provider_id": d.ProviderID,
		"contract_version": d.ContractVersion, "deployment_id": d.DeploymentID,
		"provider_kind": d.ProviderKind, "integrity_digest": d.IntegrityDigest,
	} {
		if strings.TrimSpace(value) == "" { return fmt.Errorf("%s is required", field) }
	}
	return nil
}

func (d ProviderDescriptor) Equal(other ProviderDescriptor) bool {
	return d.CapabilityID == other.CapabilityID && d.ProviderID == other.ProviderID &&
		d.ContractVersion == other.ContractVersion && d.DeploymentID == other.DeploymentID &&
		d.ProviderKind == other.ProviderKind && d.IntegrityDigest == other.IntegrityDigest &&
		d.IdempotencyContract == other.IdempotencyContract
}

type OutputKind string
const ( OutputStdout OutputKind = "stdout"; OutputStderr OutputKind = "stderr" )

type Output struct { Kind OutputKind; Message string; NativeSessionID *string }
func (o Output) Validate() error {
	if o.Kind != OutputStdout && o.Kind != OutputStderr { return fmt.Errorf("unknown output kind %q", o.Kind) }
	return validateNativeSessionID(o.NativeSessionID)
}

type Completion struct { FinalText string; FinalPath string; NativeSessionID *string }
func (c Completion) Validate() error {
	if strings.TrimSpace(c.FinalText) == "" && strings.TrimSpace(c.FinalPath) == "" { return errors.New("completion requires final text and/or path") }
	return validateNativeSessionID(c.NativeSessionID)
}

func validateNativeSessionID(value *string) error {
	if value != nil && strings.TrimSpace(*value) == "" { return errors.New("native_session_id cannot be empty") }
	return nil
}

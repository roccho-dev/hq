package worker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"hq/internal/worker/adapter"
)

var canonicalTargets = map[string]struct{}{"sh": {}, "herdr": {}, "codex": {}, "claude": {}, "host": {}, "local-tool": {}}

const RunViewVersionV1 = "run-view.v1"

func (r ResultRow) Validate() error {
	if strings.TrimSpace(r.EventID) == "" {
		return errors.New("event_id is required")
	}
	if r.Version != ResultVersionV1 {
		return fmt.Errorf("version must be %q", ResultVersionV1)
	}
	if strings.TrimSpace(r.RunID) == "" || strings.TrimSpace(r.InstructionID) == "" {
		return errors.New("run_id and instruction_id are required")
	}
	if _, ok := canonicalTargets[r.Target]; !ok {
		return fmt.Errorf("unknown target %q", r.Target)
	}
	if r.Seq < 0 {
		return errors.New("seq must be non-negative")
	}
	if r.RecordedAt.IsZero() {
		return errors.New("recorded_at is required")
	}
	_, offset := r.RecordedAt.Zone()
	if offset != 0 {
		return errors.New("recorded_at must be UTC")
	}
	if r.NativeSessionID != nil && strings.TrimSpace(*r.NativeSessionID) == "" {
		return errors.New("native_session_id cannot be empty")
	}
	if r.Provider != nil {
		if r.Kind != ResultStarted {
			return errors.New("provider evidence is allowed only on started results")
		}
		if err := r.Provider.Validate(); err != nil {
			return err
		}
	}
	if r.View != nil {
		if r.Kind != ResultStarted {
			return errors.New("run view evidence is allowed only on started results")
		}
		if err := r.View.Validate(); err != nil {
			return err
		}
	}
	switch r.Kind {
	case ResultAccepted:
		if r.Message != nil || r.Final != nil || r.Error != nil || r.Provider != nil || r.View != nil {
			return fmt.Errorf("%s cannot carry message, final, error, provider, or view", r.Kind)
		}
	case ResultStarted:
		if r.Message != nil || r.Final != nil || r.Error != nil {
			return fmt.Errorf("%s cannot carry message, final, or error", r.Kind)
		}
	case ResultStdout, ResultStderr:
		if r.Message == nil || r.Final != nil || r.Error != nil {
			return fmt.Errorf("%s requires message and forbids final/error", r.Kind)
		}
	case ResultCompleted:
		if r.Message != nil || r.Error != nil || r.Final == nil || (strings.TrimSpace(r.Final.Text) == "" && strings.TrimSpace(r.Final.Path) == "") {
			return errors.New("completed requires non-empty final text and/or path")
		}
	case ResultFailed, ResultBlocked, ResultTimeout, ResultCancelled:
		if r.Message != nil || r.Final != nil || r.Error == nil {
			return fmt.Errorf("%s requires error and forbids message/final", r.Kind)
		}
		if err := r.Error.Validate(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown result kind %q", r.Kind)
	}
	return nil
}
func (v RunViewEvidence) Validate() error {
	if v.Version != RunViewVersionV1 {
		return fmt.Errorf("run view version must be %q", RunViewVersionV1)
	}
	if v.Policy != "required" {
		return errors.New("run view policy must be required")
	}
	if strings.TrimSpace(v.NativeSessionID) == "" {
		return errors.New("run view native_session_id is required")
	}
	if err := v.Provider.Validate(); err != nil {
		return fmt.Errorf("run view provider: %w", err)
	}
	return nil
}
func (p ProviderEvidence) Validate() error {
	if (p.IdempotencyContract == "") != (p.IdempotencyKey == "") {
		return errors.New("idempotency_contract and idempotency_key must appear together")
	}
	return p.descriptor().ValidatePersisted()
}
func (p ProviderEvidence) SameProvider(other ProviderEvidence) bool {
	return p.descriptor().Equal(other.descriptor())
}
func (p ProviderEvidence) descriptor() adapter.ProviderDescriptor {
	dependencies := make([]adapter.ProviderDependencyDescriptor, 0, len(p.Dependencies))
	for _, dependency := range p.Dependencies {
		dependencies = append(dependencies, adapter.ProviderDependencyDescriptor{
			Name: dependency.Name, ProviderID: dependency.ProviderID, ContractVersion: dependency.ContractVersion,
			DeploymentID: dependency.DeploymentID, ProviderKind: dependency.ProviderKind,
			IntegrityDigest: dependency.IntegrityDigest, ConfigurationDigest: dependency.ConfigurationDigest,
		})
	}
	return adapter.ProviderDescriptor{
		CapabilityID: p.CapabilityID, ProviderID: p.ProviderID, ContractVersion: p.ContractVersion,
		DeploymentID: p.DeploymentID, ProviderKind: p.ProviderKind, IntegrityDigest: p.IntegrityDigest,
		ConfigurationDigest: p.ConfigurationDigest, Dependencies: dependencies, IdempotencyContract: p.IdempotencyContract,
	}
}
func (e ResultError) Validate() error {
	if strings.TrimSpace(e.Code) == "" || strings.TrimSpace(e.Message) == "" {
		return errors.New("error code and message are required")
	}
	return nil
}
func (v ValidationRow) Validate() error {
	if v.Version != ValidationVersionV1 {
		return fmt.Errorf("version must be %q", ValidationVersionV1)
	}
	if v.SourceLine < 1 {
		return errors.New("source_line must be a positive integer")
	}
	if v.Status != StatusBlocked {
		return errors.New("validation status must be blocked")
	}
	if err := v.Error.Validate(); err != nil {
		return err
	}
	if v.InstructionID != "" && strings.TrimSpace(v.InstructionID) == "" {
		return errors.New("instruction_id cannot be blank")
	}
	if v.Target != "" {
		if _, ok := canonicalTargets[v.Target]; !ok {
			return fmt.Errorf("unknown target %q", v.Target)
		}
	}
	return nil
}
func decodeStrict(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values in one row")
		}
		return err
	}
	return nil
}

// Package workerservice owns the managed local hq-worker observation loop.
// Host installation and supervision remain envs responsibilities.
package workerservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"hq/internal/capability"
	"hq/internal/hqprofile"
	"hq/internal/worker"
	"hq/internal/worker/adapter"
	"hq/internal/worker/hostopen"
	"hq/internal/workeraccept"
	"hq/internal/workerclaim"
)

const LifecycleKind = "hq.workerLifecycle.v1"
const HealthKind = "hq.workerHealth.v1"

const (
	StateReady = "configured_ready"
	StateNotConfigured = "not_configured"
	StateSourceUnavailable = "source_unavailable"
	StateEvidenceInvalid = "evidence_invalid"
	StateProviderUnavailable = "provider_unavailable"
	StateAlreadyActive = "worker_already_active"
	StateStale = "worker_stale_unhealthy"
	StateReconciliation = "reconciliation_required"
)

type Lifecycle struct {
	Kind string `json:"kind"`; State string `json:"state"`; Profile string `json:"profile"`; DeploymentID string `json:"deployment_id"`
	WorkerID string `json:"worker_id"`; ClaimID string `json:"claim_id,omitempty"`; ObservedAt time.Time `json:"observed_at"`; Message string `json:"message,omitempty"`
}
type Health struct {
	Kind string `json:"kind"`; State string `json:"state"`; Ready bool `json:"ready"`; Profile string `json:"profile"`; DeploymentID string `json:"deployment_id,omitempty"`
	WorkerID string `json:"worker_id,omitempty"`; ClaimID string `json:"claim_id,omitempty"`; ObservedAt time.Time `json:"observed_at"`; Message string `json:"message,omitempty"`
}

func Serve(ctx context.Context, profile hqprofile.Profile, workerID string, out io.Writer) error {
	claim, err := workerclaim.Acquire(profile.WorkspaceRoot, workerID, time.Now())
	if err != nil { return err }
	defer func() { _ = workerclaim.RemoveHeartbeat(profile.WorkspaceRoot); _ = claim.Release() }()
	owner := claim.Owner()
	if err := encode(out, Lifecycle{Kind: LifecycleKind, State: "starting", Profile: profile.Name, DeploymentID: profile.DeploymentID, WorkerID: owner.WorkerID, ClaimID: owner.ClaimID, ObservedAt: time.Now().UTC()}); err != nil { return err }
	ticker := time.NewTicker(profile.PollInterval()); defer ticker.Stop()
	for {
		state, processErr := processOnce(ctx, profile)
		if processErr != nil {
			_, _ = claim.WriteHeartbeat(profile.DeploymentID, profile.Name, state, time.Now())
			return processErr
		}
		if _, err := claim.WriteHeartbeat(profile.DeploymentID, profile.Name, state, time.Now()); err != nil { return err }
		select {
		case <-ctx.Done():
			_ = encode(out, Lifecycle{Kind: LifecycleKind, State: "stopped", Profile: profile.Name, DeploymentID: profile.DeploymentID, WorkerID: owner.WorkerID, ClaimID: owner.ClaimID, ObservedAt: time.Now().UTC()})
			return nil
		case <-ticker.C:
		}
	}
}

func HealthCheck(profile hqprofile.Profile, now time.Time) Health {
	report := Health{Kind: HealthKind, State: StateNotConfigured, Profile: profile.Name, DeploymentID: profile.DeploymentID, ObservedAt: now.UTC()}
	inspection, err := workerclaim.Inspect(profile.WorkspaceRoot, now, profile.HealthTimeout())
	if err != nil { report.State = StateEvidenceInvalid; report.Message = err.Error(); return report }
	if !inspection.Exists || inspection.Owner == nil { report.State = StateNotConfigured; report.Message = "managed worker claim is absent"; return report }
	report.WorkerID, report.ClaimID = inspection.Owner.WorkerID, inspection.Owner.ClaimID
	heartbeat, fresh, err := workerclaim.HeartbeatFresh(profile.WorkspaceRoot, inspection.Owner.ClaimID, now, profile.HealthTimeout())
	if err != nil { report.State = StateEvidenceInvalid; report.Message = err.Error(); return report }
	if !fresh { report.State = StateStale; report.Message = "managed worker heartbeat is missing or stale"; return report }
	if heartbeat.DeploymentID != profile.DeploymentID || heartbeat.Profile != profile.Name { report.State = StateEvidenceInvalid; report.Message = "worker heartbeat deployment/profile mismatch"; return report }
	if _, err := loadRegistry(profile); err != nil { report.State = StateProviderUnavailable; report.Message = err.Error(); return report }
	data, err := worker.LoadEventFile(profile.EventsPath)
	if err != nil { report.State = StateEvidenceInvalid; report.Message = err.Error(); return report }
	if hasReconciliation(data.Results) || heartbeat.State == StateReconciliation { report.State = StateReconciliation; report.Message = "one or more runs require typed reconciliation"; return report }
	report.State, report.Ready = StateReady, true
	return report
}

func processOnce(ctx context.Context, profile hqprofile.Profile) (string, error) {
	file, err := os.Open(profile.AcceptedPath)
	if err != nil { return StateSourceUnavailable, err }
	rows, err := workeraccept.Read(profile.AcceptedPath, file); _ = file.Close()
	if err != nil { return StateSourceUnavailable, err }
	prior, err := worker.LoadEventFile(profile.EventsPath)
	if err != nil { return StateEvidenceInvalid, err }
	pending := pendingRows(rows, prior)
	if len(pending) == 0 {
		if hasReconciliation(prior.Results) { return StateReconciliation, nil }
		return StateReady, nil
	}
	registry, err := loadRegistry(profile)
	if err != nil { return StateProviderUnavailable, err }
	approvals, err := worker.ApprovalsForAccepted(pending, "hq.submit:"+profile.DeploymentID)
	if err != nil { return StateEvidenceInvalid, err }
	runner := worker.NewRunner(profile.WorkspaceRoot, registry, approvals)
	_, _, err = runner.Process(ctx, pending, prior, false, worker.NewEventLog(profile.EventsPath))
	if err != nil { return StateEvidenceInvalid, err }
	updated, err := worker.LoadEventFile(profile.EventsPath)
	if err != nil { return StateEvidenceInvalid, err }
	if hasReconciliation(updated.Results) { return StateReconciliation, nil }
	return StateReady, nil
}

func loadRegistry(profile hqprofile.Profile) (*adapter.Registry, error) {
	binding, err := capability.Load(profile.CapabilitiesPath, profile.DeploymentID, capability.HostOpenCapability)
	if err != nil { return nil, err }
	host, err := hostopen.New(binding)
	if err != nil { return nil, err }
	descriptor := adapter.ProviderDescriptor{CapabilityID: binding.CapabilityID, ProviderID: binding.ProviderID, ContractVersion: binding.ContractVersion, DeploymentID: binding.DeploymentID, ProviderKind: binding.ProviderKind, IntegrityDigest: binding.IntegrityDigest, IdempotencyContract: binding.IdempotencyContract}
	return adapter.NewRegistry(adapter.Registration{Target: "host", Adapter: host, Provider: &descriptor})
}

func pendingRows(rows []worker.ReadRow, prior worker.LogData) []worker.ReadRow {
	terminal := map[string]bool{}
	lastSeq := map[string]int{}
	for _, result := range prior.Results {
		if result.Seq < lastSeq[result.InstructionID] { continue }
		lastSeq[result.InstructionID] = result.Seq
		terminal[result.InstructionID] = isTerminal(result.Kind)
	}
	validatedLines := map[int]bool{}
	for _, validation := range prior.Validations { validatedLines[validation.SourceLine] = true }
	out := make([]worker.ReadRow, 0, len(rows))
	for _, row := range rows {
		if row.ParseError != nil && validatedLines[row.Source.Line] { continue }
		if row.Instruction.ID != "" && terminal[row.Instruction.ID] { continue }
		out = append(out, row)
	}
	return out
}

func isTerminal(kind string) bool {
	switch kind { case worker.ResultCompleted, worker.ResultFailed, worker.ResultBlocked, worker.ResultTimeout, worker.ResultCancelled: return true }
	return false
}
func hasReconciliation(results []worker.ResultRow) bool {
	latest := map[string]worker.ResultRow{}
	for _, row := range results { current, ok := latest[row.RunID]; if !ok || row.Seq > current.Seq { latest[row.RunID] = row } }
	keys := make([]string, 0, len(latest)); for key := range latest { keys = append(keys, key) }; sort.Strings(keys)
	for _, key := range keys { row := latest[key]; if row.Kind == worker.ResultBlocked && row.Error != nil && row.Error.Code == "reconcile_required" { return true } }
	return false
}
func encode(w io.Writer, value any) error { encoder := json.NewEncoder(w); encoder.SetEscapeHTML(false); return encoder.Encode(value) }

var _ = errors.New
var _ = fmt.Sprintf
var _ = strings.TrimSpace

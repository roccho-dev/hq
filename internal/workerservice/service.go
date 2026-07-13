// Package workerservice owns the managed local hq-worker observation loop.
// Host installation and process supervision remain envs responsibilities.
package workerservice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"time"

	"hq/internal/adapter/current"
	"hq/internal/capability"
	"hq/internal/core"
	"hq/internal/hqprofile"
	"hq/internal/localtool"
	"hq/internal/worker"
	"hq/internal/worker/adapter"
	"hq/internal/worker/hostopen"
	"hq/internal/workeraccept"
	"hq/internal/workerclaim"
)

const LifecycleKind = "hq.workerLifecycle.v1"
const HealthKind = "hq.workerHealth.v1"

const (
	StateReady               = "configured_ready"
	StateNotConfigured       = "not_configured"
	StateSourceUnavailable   = "source_unavailable"
	StateEvidenceInvalid     = "evidence_invalid"
	StateProviderUnavailable = "provider_unavailable"
	StateStale               = "worker_stale_unhealthy"
	StateReconciliation      = "reconciliation_required"
)

type Lifecycle struct {
	Kind         string    `json:"kind"`
	State        string    `json:"state"`
	Profile      string    `json:"profile"`
	DeploymentID string    `json:"deployment_id"`
	WorkerID     string    `json:"worker_id"`
	ClaimID      string    `json:"claim_id,omitempty"`
	ObservedAt   time.Time `json:"observed_at"`
	Message      string    `json:"message,omitempty"`
}

type Health struct {
	Kind         string    `json:"kind"`
	State        string    `json:"state"`
	Ready        bool      `json:"ready"`
	Profile      string    `json:"profile"`
	DeploymentID string    `json:"deployment_id,omitempty"`
	WorkerID     string    `json:"worker_id,omitempty"`
	ClaimID      string    `json:"claim_id,omitempty"`
	ObservedAt   time.Time `json:"observed_at"`
	Message      string    `json:"message,omitempty"`
}

func Serve(ctx context.Context, profile hqprofile.Profile, workerID string, out io.Writer) error {
	claim, err := workerclaim.Acquire(profile.WorkspaceRoot, workerID, time.Now())
	if err != nil {
		return err
	}
	defer func() {
		_ = workerclaim.RemoveHeartbeat(profile.WorkspaceRoot)
		_ = claim.Release()
	}()
	owner := claim.Owner()
	if err := encode(out, Lifecycle{Kind: LifecycleKind, State: "starting", Profile: profile.Name, DeploymentID: profile.DeploymentID, WorkerID: owner.WorkerID, ClaimID: owner.ClaimID, ObservedAt: time.Now().UTC()}); err != nil {
		return err
	}
	ticker := time.NewTicker(profile.PollInterval())
	defer ticker.Stop()
	for {
		state, processErr := processOnce(ctx, profile)
		if _, heartbeatErr := claim.WriteHeartbeat(profile.DeploymentID, profile.Name, state, time.Now()); heartbeatErr != nil {
			return heartbeatErr
		}
		if processErr != nil {
			return processErr
		}
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
	if err != nil {
		report.State, report.Message = StateEvidenceInvalid, err.Error()
		return report
	}
	if !inspection.Exists || inspection.Owner == nil {
		report.Message = "managed worker claim is absent"
		return report
	}
	report.WorkerID, report.ClaimID = inspection.Owner.WorkerID, inspection.Owner.ClaimID
	heartbeat, fresh, err := workerclaim.HeartbeatFresh(profile.WorkspaceRoot, inspection.Owner.ClaimID, now, profile.HealthTimeout())
	if err != nil {
		report.State, report.Message = StateEvidenceInvalid, err.Error()
		return report
	}
	if !fresh {
		report.State, report.Message = StateStale, "managed worker heartbeat is missing or stale"
		return report
	}
	if heartbeat.DeploymentID != profile.DeploymentID || heartbeat.Profile != profile.Name {
		report.State, report.Message = StateEvidenceInvalid, "worker heartbeat deployment/profile mismatch"
		return report
	}
	if _, err := loadRegistry(profile); err != nil {
		report.State, report.Message = StateProviderUnavailable, err.Error()
		return report
	}
	data, err := worker.LoadEventFile(profile.EventsPath)
	if err != nil {
		report.State, report.Message = StateEvidenceInvalid, err.Error()
		return report
	}
	if hasReconciliation(data.Results) || heartbeat.State == StateReconciliation {
		report.State, report.Message = StateReconciliation, "one or more runs require typed reconciliation"
		return report
	}
	report.State, report.Ready = StateReady, true
	return report
}

func processOnce(ctx context.Context, profile hqprofile.Profile) (string, error) {
	file, err := os.Open(profile.AcceptedPath)
	if err != nil {
		return StateSourceUnavailable, err
	}
	rows, readErr := workeraccept.Read(profile.AcceptedPath, file)
	closeErr := file.Close()
	if readErr != nil {
		return StateSourceUnavailable, readErr
	}
	if closeErr != nil {
		return StateSourceUnavailable, closeErr
	}
	world, err := loadProfileWorld(profile)
	if err != nil {
		return StateEvidenceInvalid, err
	}
	rows = validateSelectedWorldRows(rows, world)
	prior, err := worker.LoadEventFile(profile.EventsPath)
	if err != nil {
		return StateEvidenceInvalid, err
	}
	pending := pendingRows(rows, prior)
	if len(pending) == 0 {
		if hasReconciliation(prior.Results) {
			return StateReconciliation, nil
		}
		return StateReady, nil
	}
	registry, err := loadRegistry(profile)
	if err != nil {
		return StateProviderUnavailable, err
	}
	approvals, err := worker.ApprovalsForAccepted(pending, "hq.submit:"+profile.DeploymentID)
	if err != nil {
		return StateEvidenceInvalid, err
	}
	runner := worker.NewRunner(profile.WorkspaceRoot, registry, approvals)
	if _, _, err := runner.Process(ctx, pending, prior, false, worker.NewEventLog(profile.EventsPath)); err != nil {
		return StateEvidenceInvalid, err
	}
	updated, err := worker.LoadEventFile(profile.EventsPath)
	if err != nil {
		return StateEvidenceInvalid, err
	}
	if hasReconciliation(updated.Results) {
		return StateReconciliation, nil
	}
	return StateReady, nil
}

func loadRegistry(profile hqprofile.Profile) (*adapter.Registry, error) {
	if profile.WorldPath == "" {
		return hostRegistry(profile)
	}
	worldFile, err := os.Open(profile.WorldPath)
	if err != nil {
		return nil, err
	}
	world, loadErr := current.LoadRuntimeWorldJSONL(worldFile)
	closeErr := worldFile.Close()
	if loadErr != nil {
		return nil, loadErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	registrations := []adapter.Registration{}
	if worldSelectsTarget(world, "host") {
		if profile.CapabilitiesPath == "" {
			return nil, errors.New("selected world declares host commands but profile has no capabilities_path")
		}
		host, err := hostRegistration(profile)
		if err != nil {
			return nil, err
		}
		registrations = append(registrations, host)
	}
	if len(world.LocalTools) != 0 {
		if profile.ExecutableBindingsPath == "" {
			return nil, errors.New("selected world declares local tools but profile has no executable_bindings_path")
		}
		registrations = append(registrations, adapter.Registration{Target: "local-tool", Preparer: localtool.Preparer{World: world, BindingsPath: profile.ExecutableBindingsPath}})
	}
	return adapter.NewRegistry(registrations...)
}

func hostRegistry(profile hqprofile.Profile) (*adapter.Registry, error) {
	host, err := hostRegistration(profile)
	if err != nil {
		return nil, err
	}
	return adapter.NewRegistry(host)
}

func hostRegistration(profile hqprofile.Profile) (adapter.Registration, error) {
	binding, err := capability.Load(profile.CapabilitiesPath, profile.DeploymentID, capability.HostOpenCapability)
	if err != nil {
		return adapter.Registration{}, err
	}
	hostAdapter, err := hostopen.New(binding)
	if err != nil {
		return adapter.Registration{}, err
	}
	descriptor := adapter.ProviderDescriptor{CapabilityID: binding.CapabilityID, ProviderID: binding.ProviderID, ContractVersion: binding.ContractVersion, DeploymentID: binding.DeploymentID, ProviderKind: binding.ProviderKind, IntegrityDigest: binding.IntegrityDigest, IdempotencyContract: binding.IdempotencyContract}
	return adapter.Registration{Target: "host", Adapter: hostAdapter, Provider: &descriptor}, nil
}

func worldSelectsTarget(world *core.JsonlWorld, target string) bool {
	for _, command := range world.Commands {
		if value, ok := command.Instruction["target"].(string); ok && value == target {
			return true
		}
	}
	return false
}

func pendingRows(rows []worker.ReadRow, prior worker.LogData) []worker.ReadRow {
	latest := map[string]worker.ResultRow{}
	for _, result := range prior.Results {
		current, exists := latest[result.InstructionID]
		if !exists || result.Seq > current.Seq {
			latest[result.InstructionID] = result
		}
	}
	validatedLines := map[int]bool{}
	for _, validation := range prior.Validations {
		validatedLines[validation.SourceLine] = true
	}
	out := make([]worker.ReadRow, 0, len(rows))
	for _, row := range rows {
		if row.ParseError != nil && validatedLines[row.Source.Line] {
			continue
		}
		if result, exists := latest[row.Instruction.ID]; exists && isTerminal(result.Kind) {
			continue
		}
		out = append(out, row)
	}
	return out
}

func isTerminal(kind string) bool {
	switch kind {
	case worker.ResultCompleted, worker.ResultFailed, worker.ResultBlocked, worker.ResultTimeout, worker.ResultCancelled:
		return true
	default:
		return false
	}
}

func hasReconciliation(results []worker.ResultRow) bool {
	latest := map[string]worker.ResultRow{}
	for _, result := range results {
		current, exists := latest[result.RunID]
		if !exists || result.Seq > current.Seq {
			latest[result.RunID] = result
		}
	}
	for _, result := range latest {
		if result.Kind == worker.ResultStarted {
			return true
		}
	}
	return false
}

func encode(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func SortedStates(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

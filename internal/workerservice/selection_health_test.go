package workerservice

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hq/internal/core"
	"hq/internal/hqprofile"
	"hq/internal/workerclaim"
)

func TestFreshManagedWorkerHeartbeatAndHealthBindExactInProcessSelection(t *testing.T) {
	profile := writeSelectionProfile(t, "world.worker-a")
	selection, err := loadProfileSelection(profile)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, profile, "worker-selection-proof", io.Discard) }()

	heartbeat := waitForHeartbeat(t, profile)
	if heartbeat.Kind != workerclaim.HeartbeatKindV2 || heartbeat.DeploymentID != profile.DeploymentID || heartbeat.Profile != profile.Name || heartbeat.SelectedWorld == nil || *heartbeat.SelectedWorld != selection.Ref {
		cancel()
		t.Fatalf("heartbeat=%+v selection=%+v", heartbeat, selection.Ref)
	}
	health := HealthCheck(profile, time.Now())
	if !health.Ready || health.Kind != HealthKind || health.DeploymentID != profile.DeploymentID || health.SelectedWorld == nil || *health.SelectedWorld != selection.Ref {
		cancel()
		t.Fatalf("health=%+v selection=%+v", health, selection.Ref)
	}
	if _, err := os.Stat(profile.EventsPath); !os.IsNotExist(err) {
		cancel()
		t.Fatalf("selection observation created event/history evidence: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("managed worker did not stop")
	}
}

func TestHealthRejectsWorldAndDeploymentMismatchBeforeProviderPreparation(t *testing.T) {
	profile := writeSelectionProfile(t, "world.worker-current")
	if err := os.WriteFile(profile.WorldPath, []byte(strictHostSelectionWorld("world.worker-current")), 0o600); err != nil {
		t.Fatal(err)
	}
	profile.CapabilitiesPath = filepath.Join(t.TempDir(), "must-not-open.json")
	selection, err := loadProfileSelection(profile)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC)
	claim, err := workerclaim.Acquire(profile.WorkspaceRoot, "worker-mismatch", now)
	if err != nil {
		t.Fatal(err)
	}
	wrong := core.WorldRef{WorldID: "world.worker-other", Digest: selection.Ref.Digest}
	if _, err := claim.WriteHeartbeat(profile.DeploymentID, profile.Name, StateReady, wrong, now); err != nil {
		t.Fatal(err)
	}
	// The host command and missing capability file would fail provider
	// preparation if health crossed the mismatch boundary.
	health := HealthCheck(profile, now.Add(time.Millisecond))
	if health.Ready || health.State != StateEvidenceInvalid || health.SelectedWorld != nil || health.Message != "worker heartbeat selected-world mismatch" {
		t.Fatalf("world mismatch health=%+v", health)
	}
	if _, err := os.Stat(profile.EventsPath); !os.IsNotExist(err) {
		t.Fatalf("world mismatch emitted provider/event evidence: %v", err)
	}

	if _, err := claim.WriteHeartbeat(profile.DeploymentID, profile.Name, StateReady, selection.Ref, now.Add(2*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	changedDeployment := profile
	changedDeployment.DeploymentID = "deployment-other"
	health = HealthCheck(changedDeployment, now.Add(3*time.Millisecond))
	if health.Ready || health.State != StateEvidenceInvalid || health.SelectedWorld != nil || health.Message != "worker heartbeat deployment/profile mismatch" {
		t.Fatalf("deployment mismatch health=%+v", health)
	}
}

func TestLegacyHeartbeatIsNeverSelectedWorldReady(t *testing.T) {
	profile := writeSelectionProfile(t, "world.worker-legacy")
	now := time.Date(2026, 7, 13, 2, 0, 0, 0, time.UTC)
	claim, err := workerclaim.Acquire(profile.WorkspaceRoot, "worker-v1", now)
	if err != nil {
		t.Fatal(err)
	}
	owner := claim.Owner()
	legacy := map[string]any{
		"kind": workerclaim.HeartbeatKindV1, "claim_id": owner.ClaimID,
		"worker_id": owner.WorkerID, "workspace": owner.Workspace,
		"deployment_id": profile.DeploymentID, "profile": profile.Name,
		"state": StateReady, "observed_at": now,
	}
	data, _ := json.Marshal(legacy)
	heartbeatPath := filepath.Join(profile.WorkspaceRoot, ".hq", "worker", "heartbeat.json")
	if err := os.WriteFile(heartbeatPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	health := HealthCheck(profile, now.Add(time.Millisecond))
	if health.Ready || health.State != StateEvidenceInvalid || health.SelectedWorld != nil || health.Message != "legacy worker heartbeat cannot prove selected-world readiness" {
		t.Fatalf("legacy health=%+v", health)
	}
	health = HealthCheck(profile, now.Add(10*time.Second))
	if health.Ready || health.State != StateStale || health.SelectedWorld != nil {
		t.Fatalf("stale legacy health=%+v", health)
	}
}

func TestMatchedHeartbeatNonReadyStatesRemainFailClosed(t *testing.T) {
	profile := writeSelectionProfile(t, "world.worker-state")
	selection, err := loadProfileSelection(profile)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 2, 30, 0, 0, time.UTC)
	claim, err := workerclaim.Acquire(profile.WorkspaceRoot, "worker-state", now)
	if err != nil {
		t.Fatal(err)
	}
	for index, state := range []string{
		StateNotConfigured, StateSourceUnavailable, StateEvidenceInvalid,
		StateProviderUnavailable, StateStale, StateReconciliation,
	} {
		at := now.Add(time.Duration(index) * time.Millisecond)
		if _, err := claim.WriteHeartbeat(profile.DeploymentID, profile.Name, state, selection.Ref, at); err != nil {
			t.Fatal(err)
		}
		health := HealthCheck(profile, at.Add(time.Microsecond))
		if health.Ready || health.State != state || health.SelectedWorld == nil || *health.SelectedWorld != selection.Ref {
			t.Fatalf("heartbeat state=%q health=%+v", state, health)
		}
	}
	if _, err := claim.WriteHeartbeat(profile.DeploymentID, profile.Name, "unknown-state", selection.Ref, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	health := HealthCheck(profile, now.Add(time.Second+time.Microsecond))
	if health.Ready || health.State != StateEvidenceInvalid || health.SelectedWorld == nil || *health.SelectedWorld != selection.Ref {
		t.Fatalf("unknown heartbeat state health=%+v", health)
	}
	if _, err := os.Stat(profile.EventsPath); !os.IsNotExist(err) {
		t.Fatalf("non-ready heartbeat created provider/event evidence: %v", err)
	}
}

func TestManagedWorkerKeepsStartupSnapshotAndHealthDetectsAtomicReplacement(t *testing.T) {
	profile := writeSelectionProfile(t, "world.worker-before")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, profile, "worker-snapshot", io.Discard) }()
	before := waitForHeartbeat(t, profile)
	if before.SelectedWorld == nil || before.SelectedWorld.WorldID != "world.worker-before" {
		cancel()
		t.Fatalf("before heartbeat=%+v", before)
	}
	if err := os.WriteFile(profile.WorldPath, []byte(strictSelectionWorld("world.worker-after")), 0o600); err != nil {
		cancel()
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		heartbeat, err := workerclaim.ReadHeartbeat(profile.WorkspaceRoot)
		if err == nil && heartbeat.ObservedAt.After(before.ObservedAt) {
			if heartbeat.SelectedWorld == nil || heartbeat.SelectedWorld.WorldID != "world.worker-before" {
				cancel()
				t.Fatalf("running worker hot-reloaded world: %+v", heartbeat)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	health := HealthCheck(profile, time.Now())
	if health.Ready || health.State != StateEvidenceInvalid || health.Message != "worker heartbeat selected-world mismatch" {
		cancel()
		t.Fatalf("replacement health=%+v", health)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("managed worker did not stop")
	}
}

func writeSelectionProfile(t *testing.T, worldID string) hqprofile.Profile {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	eventsDir := filepath.Join(workspace, ".hq", "events")
	if err := os.MkdirAll(eventsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(root, "world.jsonl")
	acceptedPath := filepath.Join(root, "accepted.jsonl")
	if err := os.WriteFile(worldPath, []byte(strictSelectionWorld(worldID)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(acceptedPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return hqprofile.Profile{
		Kind: hqprofile.Kind, Name: "local", DeploymentID: "deployment-1",
		WorldPath: worldPath, AcceptedPath: acceptedPath, WorkspaceRoot: workspace,
		EventsPath: filepath.Join(eventsDir, "events.jsonl"), PollIntervalMS: 20, HealthTimeoutMS: 5000,
	}
}

func strictSelectionWorld(worldID string) string {
	return `{"kind":"hq.world.v1","world_id":"` + worldID + `"}` + "\n" +
		`{"key":"reason","type":"string","required":true}` + "\n"
}

func strictHostSelectionWorld(worldID string) string {
	return `{"kind":"hq.world.v1","world_id":"` + worldID + `"}` + "\n" +
		`{"kind":"hq.command.v1","command_id":"host.open","command_version":"1","name":"host.open","instruction":{"version":"instruction.v1","op":"run","target":"host","payload":{"capability":"host.open"}},"fields":[{"name":"path","type":"path","required":true,"bind":"payload.path"}]}` + "\n"
}

func waitForHeartbeat(t *testing.T, profile hqprofile.Profile) workerclaim.Heartbeat {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		heartbeat, err := workerclaim.ReadHeartbeat(profile.WorkspaceRoot)
		if err == nil {
			return heartbeat
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("managed worker heartbeat was not written")
	return workerclaim.Heartbeat{}
}

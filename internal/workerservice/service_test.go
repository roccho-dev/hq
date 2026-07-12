package workerservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hq/internal/capability"
	"hq/internal/hqprofile"
	"hq/internal/worker"
)

func init() {
	if os.Getenv("HQ_TEST_HOST_PROVIDER") == "1" {
		marker := os.Getenv("HQ_TEST_HOST_PROVIDER_MARKER")
		if marker != "" {
			_ = os.WriteFile(marker, []byte("invoked"), 0o600)
		}
		os.Exit(0)
	}
}

func TestServeAutomaticallyProcessesAcceptedHostIntentWithExactProvider(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	eventDir := filepath.Join(workspace, ".hq", "events")
	if err := os.MkdirAll(eventDir, 0o700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	providerBytes, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(providerBytes)
	capabilitiesPath := filepath.Join(root, "capabilities.json")
	registry := capability.Registry{
		Kind:         capability.RegistryKind,
		DeploymentID: "dep-test",
		Bindings: []capability.Binding{{
			Kind:               capability.BindingKind,
			CapabilityID:       capability.HostOpenCapability,
			ProviderID:         "test-host-provider",
			ContractVersion:    capability.HostOpenContract,
			DeploymentID:       "dep-test",
			ProviderKind:       "executable",
			ExecutablePath:     executable,
			IntegrityDigest:    "sha256:" + hex.EncodeToString(digest[:]),
			VerificationStatus: "verified",
		}},
	}
	encodedRegistry, _ := json.Marshal(registry)
	if err := os.WriteFile(capabilitiesPath, encodedRegistry, 0o600); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(root, "target")
	if err := os.Mkdir(targetPath, 0o700); err != nil {
		t.Fatal(err)
	}
	acceptedPath := filepath.Join(root, "accepted.jsonl")
	accepted := map[string]any{
		"kind":  "accepted.instruction",
		"queue": "instruction.jsonl",
		"instruction": map[string]any{
			"id":         "ins-host-test-001",
			"version":    "instruction.v1",
			"op":         "run",
			"target":     "host",
			"payload":    map[string]any{"capability": "host.open", "path": targetPath},
			"created_at": "2026-07-10T07:00:00Z",
		},
	}
	acceptedBytes, _ := json.Marshal(accepted)
	if err := os.WriteFile(acceptedPath, append(acceptedBytes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	eventsPath := filepath.Join(eventDir, "events.jsonl")
	profile := hqprofile.Profile{
		Name: "local", DeploymentID: "dep-test", AcceptedPath: acceptedPath,
		WorkspaceRoot: workspace, EventsPath: eventsPath, CapabilitiesPath: capabilitiesPath,
		PollIntervalMS: 20, HealthTimeoutMS: 500,
	}
	marker := filepath.Join(root, "provider.marker")
	t.Setenv("HQ_TEST_HOST_PROVIDER", "1")
	t.Setenv("HQ_TEST_HOST_PROVIDER_MARKER", marker)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, profile, "worker-test", os.Stdout) }()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, loadErr := worker.LoadEventFile(eventsPath)
		if loadErr == nil && len(data.Results) > 0 && data.Results[len(data.Results)-1].Kind == worker.ResultCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	data, err := worker.LoadEventFile(eventsPath)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if len(data.Results) != 3 || data.Results[0].Kind != worker.ResultAccepted || data.Results[1].Kind != worker.ResultStarted || data.Results[2].Kind != worker.ResultCompleted {
		cancel()
		t.Fatalf("results=%+v", data.Results)
	}
	if data.Results[1].Provider == nil || data.Results[1].Provider.ProviderID != "test-host-provider" || data.Results[1].Provider.DeploymentID != "dep-test" {
		cancel()
		t.Fatalf("provider evidence=%+v", data.Results[1].Provider)
	}
	if _, err := os.Stat(marker); err != nil {
		cancel()
		t.Fatalf("provider was not invoked: %v", err)
	}
	health := HealthCheck(profile, time.Now())
	for deadline := time.Now().Add(2 * time.Second); !health.Ready && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
		health = HealthCheck(profile, time.Now())
	}
	if !health.Ready || health.State != StateReady {
		cancel()
		t.Fatalf("health=%+v", health)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("managed worker did not stop gracefully")
	}
}

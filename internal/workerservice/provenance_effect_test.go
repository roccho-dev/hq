package workerservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hq/internal/capability"
	"hq/internal/core"
	"hq/internal/hqprofile"
	"hq/internal/worker"
)

func TestSelectedWorldMismatchStartsZeroProviderProcesses(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	eventsDir := filepath.Join(workspace, ".hq", "events")
	if err := os.MkdirAll(eventsDir, 0o700); err != nil {
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
	providerDigest := sha256.Sum256(providerBytes)
	capabilitiesPath := filepath.Join(root, "capabilities.json")
	registry := capability.Registry{
		Kind: capability.RegistryKind, DeploymentID: "dep-mismatch",
		Bindings: []capability.Binding{{
			Kind: capability.BindingKind, CapabilityID: capability.HostOpenCapability,
			ProviderID: "must-not-run", ContractVersion: capability.HostOpenContract,
			DeploymentID: "dep-mismatch", ProviderKind: "executable", ExecutablePath: executable,
			IntegrityDigest: "sha256:" + hex.EncodeToString(providerDigest[:]), VerificationStatus: "verified",
		}},
	}
	encodedRegistry, _ := json.Marshal(registry)
	if err := os.WriteFile(capabilitiesPath, encodedRegistry, 0o600); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(root, "world.jsonl")
	worldData := `{"kind":"hq.world.v1","world_id":"world.active"}` + "\n" +
		`{"kind":"hq.command.v1","command_id":"host.open","command_version":"1","name":"host.open","instruction":{"version":"instruction.v1","op":"run","target":"host","payload":{"capability":"host.open"}},"fields":[{"name":"path","type":"path","required":true,"bind":"payload.path"}]}` + "\n"
	if err := os.WriteFile(worldPath, []byte(worldData), 0o600); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(root, "target")
	if err := os.Mkdir(targetPath, 0o700); err != nil {
		t.Fatal(err)
	}
	instruction := map[string]any{
		"id": "ins-mismatch", "version": "instruction.v1", "op": "run", "target": "host",
		"payload": map[string]any{"capability": "host.open", "path": targetPath},
		"created_at": "2026-07-13T00:00:00Z",
	}
	instructionDigest, _ := core.CanonicalDigest(instruction)
	wrongWorldDigest, _ := core.CanonicalDigest("wrong-world")
	accepted := map[string]any{
		"kind": "accepted.instruction", "queue": "instruction.jsonl", "instruction": instruction,
		"provenance": core.CompileProvenance{
			Kind: core.CompileProvenanceKind, InputKind: core.CanonicalJSONInputKind,
			World: core.WorldRef{WorldID: "world.active", Digest: wrongWorldDigest},
			InstructionDigest: instructionDigest,
		},
	}
	acceptedBytes, _ := json.Marshal(accepted)
	acceptedPath := filepath.Join(root, "accepted.jsonl")
	if err := os.WriteFile(acceptedPath, append(acceptedBytes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "provider.marker")
	t.Setenv("HQ_TEST_HOST_PROVIDER", "1")
	t.Setenv("HQ_TEST_HOST_PROVIDER_MARKER", marker)
	profile := hqprofile.Profile{
		Name: "local", DeploymentID: "dep-mismatch", WorldPath: worldPath,
		AcceptedPath: acceptedPath, WorkspaceRoot: workspace,
		EventsPath: filepath.Join(eventsDir, "events.jsonl"), CapabilitiesPath: capabilitiesPath,
		PollIntervalMS: 20, HealthTimeoutMS: 500,
	}
	if _, err := processOnce(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("provider was invoked despite selected-world mismatch: %v", err)
	}
	data, err := worker.LoadEventFile(profile.EventsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Results) != 0 {
		t.Fatalf("provider results exist: %+v", data.Results)
	}
	if len(data.Validations) != 1 || data.Validations[0].Error.Code != "selected_world_mismatch" {
		t.Fatalf("validation evidence=%+v", data.Validations)
	}
}

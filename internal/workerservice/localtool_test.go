package workerservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hq/internal/hqprofile"
	"hq/internal/localtool"
	"hq/internal/worker"
)

func TestManagedLocalToolHelperProcess(t *testing.T) {
	for _, argument := range os.Args {
		if argument == "--managed-local-tool-proof" {
			_, _ = io.WriteString(os.Stdout, "dummy-managed-ok")
			os.Exit(0)
		}
	}
}

func TestManagedWorkerProcessesDataOnlyLocalToolWithoutHostCapability(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	eventDir := filepath.Join(workspace, ".hq", "events")
	if err := os.MkdirAll(eventDir, 0o700); err != nil {
		t.Fatal(err)
	}
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, filepath.Base(source))
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	digestText := "sha256:" + hex.EncodeToString(digest[:])
	bindingsPath := filepath.Join(root, "verified-executable-bindings.json")
	bindings := localtool.VerifiedBindings{Schema: localtool.VerifiedBindingsSchema, Entries: []localtool.VerifiedBinding{{
		BindingRef: "local-tool.dummy", ResourceID: "app.dummy", ContractVersion: "1", Executable: executable,
		MaterialDigest: digestText, DeploymentID: "app.dummy@1:" + digestText,
		DeclarationEventID: "dummy-declared", SelectionEventID: "dummy-selected",
	}}}
	encodedBindings, _ := json.Marshal(bindings)
	if err := os.WriteFile(bindingsPath, encodedBindings, 0o600); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(root, "world.jsonl")
	world := `{"kind":"hq.world.v1","world_id":"world.local-tool-test"}
{"kind":"hq.local-tool.v1","tool_id":"dummy","tool_version":"1","binding_ref":"local-tool.dummy","binding_contract_version":"1","actions":[{"action_id":"proof","argv":[{"literal":"-test.run=^TestManagedLocalToolHelperProcess$"},{"literal":"--"},{"literal":"--managed-local-tool-proof"}],"stdin":{"mode":"none","max_bytes":0},"limits":{"timeout_ms":2000,"stdout_bytes":4096,"stderr_bytes":4096},"output":{"format":"text"},"lifecycle":"one-shot","risk":"low","approval":"explicit"}]}
{"kind":"hq.command.v1","command_id":"dummy.proof","command_version":"1","name":"dummy.proof","instruction":{"version":"instruction.v1","op":"run","target":"local-tool","payload":{"tool_id":"dummy","tool_version":"1","action_id":"proof","input":{}}}}` + "\n"
	if err := os.WriteFile(worldPath, []byte(world), 0o600); err != nil {
		t.Fatal(err)
	}
	acceptedPath := filepath.Join(root, "accepted.jsonl")
	accepted := map[string]any{
		"kind": "accepted.instruction", "queue": "instruction.jsonl",
		"instruction": map[string]any{
			"id": "ins-local-tool-001", "version": "instruction.v1", "op": "run", "target": "local-tool",
			"payload":    map[string]any{"tool_id": "dummy", "tool_version": "1", "action_id": "proof", "input": map[string]any{}},
			"created_at": "2026-07-12T23:00:00Z",
		},
	}
	acceptedData, _ := json.Marshal(accepted)
	if err := os.WriteFile(acceptedPath, append(acceptedData, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := hqprofile.Profile{
		Kind: hqprofile.Kind, Name: "local-tool-only", DeploymentID: "hq-proof-1", WorldPath: worldPath,
		AcceptedPath: acceptedPath, WorkspaceRoot: workspace, EventsPath: filepath.Join(eventDir, "events.jsonl"),
		ExecutableBindingsPath: bindingsPath, PollIntervalMS: 20, HealthTimeoutMS: 500,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, profile, "worker-local-tool", io.Discard) }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ledger, loadErr := worker.LoadEventFile(profile.EventsPath)
		if loadErr == nil && len(ledger.Results) > 0 && ledger.Results[len(ledger.Results)-1].Kind == worker.ResultCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	ledger, err := worker.LoadEventFile(profile.EventsPath)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if len(ledger.Results) < 3 {
		cancel()
		t.Fatalf("results=%+v", ledger.Results)
	}
	started := ledger.Results[1]
	terminal := ledger.Results[len(ledger.Results)-1]
	if started.Kind != worker.ResultStarted || started.Provider == nil || started.Provider.ProviderID != "local-tool.dummy" {
		cancel()
		t.Fatalf("started=%+v", started)
	}
	if terminal.Kind != worker.ResultCompleted || terminal.Final == nil || terminal.Final.Text != "dummy-managed-ok" {
		cancel()
		t.Fatalf("terminal=%+v", terminal)
	}
	health := HealthCheck(profile, time.Now())
	for deadline := time.Now().Add(2 * time.Second); !health.Ready && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
		health = HealthCheck(profile, time.Now())
	}
	if !health.Ready || health.Kind != HealthKind || health.SelectedWorld == nil || health.SelectedWorld.WorldID != "world.local-tool-test" {
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
		t.Fatal("managed worker did not stop")
	}
}

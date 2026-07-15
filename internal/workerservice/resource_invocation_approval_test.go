package workerservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"hq/internal/hqprofile"
	"hq/internal/localtool"
	"hq/internal/worker"
	"hq/internal/workeraccept"
	"hq/internal/workersafety"
)

func TestManagedResourceInvocationApprovalHelper(t *testing.T) {
	for index, argument := range os.Args {
		if argument == "--managed-resource-proof" && index+1 < len(os.Args) {
			_ = os.WriteFile(os.Args[index+1], []byte("invoked"), 0o600)
			_, _ = io.WriteString(os.Stdout, "managed-resource-ok")
			os.Exit(0)
		}
	}
}

func TestManagedWorkerHoldsResourceInvocationUntilExplicitApproval(t *testing.T) {
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
		BindingRef: "local-tool.resource-proof", ResourceID: "app.resource-proof", ContractVersion: "1", Executable: executable,
		MaterialDigest: digestText, DeploymentID: "app.resource-proof@1:" + digestText,
		DeclarationEventID: "resource-declared", SelectionEventID: "resource-selected",
	}}}
	encodedBindings, _ := json.Marshal(bindings)
	if err := os.WriteFile(bindingsPath, encodedBindings, 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "provider.marker")
	worldPath := filepath.Join(root, "world.jsonl")
	world := `{"kind":"hq.world.v1","world_id":"world.resource-approval-test"}
{"kind":"hq.local-tool.v1","tool_id":"resource-proof","tool_version":"1","binding_ref":"local-tool.resource-proof","binding_contract_version":"1","invocation":{"policy_version":"resource-proof.v1","max_argv":8,"max_arg_bytes":4096,"limits":{"timeout_ms":2000,"stdout_bytes":4096,"stderr_bytes":4096}}}` + "\n"
	if err := os.WriteFile(worldPath, []byte(world), 0o600); err != nil {
		t.Fatal(err)
	}
	acceptedPath := filepath.Join(root, "accepted.jsonl")
	accepted := map[string]any{
		"kind": "accepted.instruction", "queue": "instruction.jsonl",
		"instruction": map[string]any{
			"id": "ins-managed-resource-001", "version": "instruction.v1", "op": "run", "target": "local-tool",
			"payload": map[string]any{
				"tool_id": "resource-proof", "tool_version": "1", "policy_version": "resource-proof.v1",
				"argv": []string{"-test.run=^TestManagedResourceInvocationApprovalHelper$", "--", "--managed-resource-proof", marker},
			},
			"created_at": "2026-07-15T07:00:00Z",
		},
	}
	acceptedData, _ := json.Marshal(accepted)
	if err := os.WriteFile(acceptedPath, append(acceptedData, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := hqprofile.Profile{
		Kind: hqprofile.Kind, Name: "resource-approval", DeploymentID: "hq-resource-proof-1", WorldPath: worldPath,
		AcceptedPath: acceptedPath, WorkspaceRoot: workspace, EventsPath: filepath.Join(eventDir, "events.jsonl"),
		ExecutableBindingsPath: bindingsPath, PollIntervalMS: 20, HealthTimeoutMS: 500,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, profile, "worker-resource-approval", io.Discard) }()

	var held worker.LogData
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ledger, loadErr := worker.LoadEventFile(profile.EventsPath)
		if loadErr == nil && len(ledger.Results) == 1 && ledger.Results[0].Kind == worker.ResultAccepted && len(ledger.Policies) > 0 && ledger.Policies[len(ledger.Policies)-1].Status == workersafety.PolicyApprovalRequired {
			held = ledger
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(held.Results) != 1 {
		cancel()
		t.Fatalf("resource invocation did not remain held: %+v", held)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		cancel()
		t.Fatalf("provider started before approval: %v", err)
	}
	acceptedFile, err := os.Open(acceptedPath)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	rows, readErr := workeraccept.Read(acceptedPath, acceptedFile)
	closeErr := acceptedFile.Close()
	if readErr != nil || closeErr != nil || len(rows) != 1 {
		cancel()
		t.Fatalf("rows=%+v readErr=%v closeErr=%v", rows, readErr, closeErr)
	}
	instructionDigest, err := worker.InstructionDigest(rows[0].Instruction)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	approval := worker.ApprovalRecord{
		Version: worker.ApprovalVersionV1, InstructionID: rows[0].Instruction.ID, Approved: true,
		ApprovedBy: "owner@test", InstructionDigest: instructionDigest,
	}
	type appendResult struct {
		appended bool
		err      error
	}
	const concurrentApprovals = 32
	start := make(chan struct{})
	results := make(chan appendResult, concurrentApprovals)
	var ready sync.WaitGroup
	ready.Add(concurrentApprovals)
	for index := 0; index < concurrentApprovals; index++ {
		go func() {
			ready.Done()
			<-start
			appended, err := worker.AppendWorkspaceApproval(workspace, approval)
			results <- appendResult{appended: appended, err: err}
		}()
	}
	ready.Wait()
	close(start)
	appended := 0
	for index := 0; index < concurrentApprovals; index++ {
		result := <-results
		if result.err != nil {
			cancel()
			t.Fatalf("concurrent approval failed: %v", result.err)
		}
		if result.appended {
			appended++
		}
	}
	if appended != 1 {
		cancel()
		t.Fatalf("concurrent approval appended=%d want=1", appended)
	}
	approvalStore, err := worker.LoadWorkspaceApprovals(workspace)
	if err != nil || approvalStore.ApprovalFor(rows[0].Instruction.ID) == nil {
		cancel()
		t.Fatalf("approval ledger unreadable after concurrent append: store=%+v err=%v", approvalStore, err)
	}

	var completed worker.LogData
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case serveErr := <-done:
			cancel()
			t.Fatalf("managed worker stopped while polling concurrent approval ledger: %v", serveErr)
		default:
		}
		ledger, loadErr := worker.LoadEventFile(profile.EventsPath)
		if loadErr == nil && len(ledger.Results) >= 3 && ledger.Results[len(ledger.Results)-1].Kind == worker.ResultCompleted {
			completed = ledger
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(completed.Results) < 3 {
		cancel()
		t.Fatalf("approved invocation did not complete: %+v", completed)
	}
	acceptedRunID := completed.Results[0].RunID
	for _, result := range completed.Results {
		if result.RunID != acceptedRunID {
			cancel()
			t.Fatalf("approval changed run identity: %+v", completed.Results)
		}
	}
	if completed.Results[1].Kind != worker.ResultStarted || completed.Results[len(completed.Results)-1].Kind != worker.ResultCompleted {
		cancel()
		t.Fatalf("results=%+v", completed.Results)
	}
	if _, err := os.Stat(marker); err != nil {
		cancel()
		t.Fatalf("provider did not start after approval: %v", err)
	}
	if len(completed.Policies) < 2 || completed.Policies[0].Status != workersafety.PolicyApprovalRequired || completed.Policies[len(completed.Policies)-1].Status != workersafety.PolicyAllowed {
		cancel()
		t.Fatalf("policies=%+v", completed.Policies)
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

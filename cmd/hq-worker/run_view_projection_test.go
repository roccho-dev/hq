package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hq/internal/core"
	"hq/internal/hqprofile"
	"hq/internal/localtool"
	"hq/internal/worker"
	workeradapter "hq/internal/worker/adapter"
)

type recordingRunViewPreparer struct {
	requests []workeradapter.Request
	prepared workeradapter.Prepared
	err      error
}

func (p *recordingRunViewPreparer) Prepare(_ context.Context, request workeradapter.Request) (workeradapter.Prepared, error) {
	p.requests = append(p.requests, request)
	return p.prepared, p.err
}

type recordingRunViewAdapter struct {
	requests   []workeradapter.Request
	completion workeradapter.Completion
	err        error
}

func (a *recordingRunViewAdapter) Run(_ context.Context, request workeradapter.Request, _ workeradapter.Emit) (workeradapter.Completion, error) {
	a.requests = append(a.requests, request)
	return a.completion, a.err
}

func TestRunViewSelectionUsesOnlyCanonicalBoundedEvidence(t *testing.T) {
	environment := runViewTestEnvironment("required", "view.open", true)
	beforeInstructions, _ := json.Marshal(environment.Instructions)
	beforeResults, _ := json.Marshal(environment.Results)

	selection, err := selectRunView("run-1", environment)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Policy != "required" || selection.ViewID != "hq-run-1" {
		t.Fatalf("selection=%+v", selection)
	}

	afterInstructions, _ := json.Marshal(environment.Instructions)
	afterResults, _ := json.Marshal(environment.Results)
	if !bytes.Equal(beforeInstructions, afterInstructions) || !bytes.Equal(beforeResults, afterResults) {
		t.Fatal("read projection changed canonical instructions or result evidence")
	}
}

func TestRunViewListLoadsSelectedProfileWorld(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	eventsDir := filepath.Join(workspace, ".hq", "events")
	if err := os.MkdirAll(eventsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(root, "world.jsonl")
	acceptedPath := filepath.Join(root, "accepted.jsonl")
	eventsPath := filepath.Join(eventsDir, "events.jsonl")
	world := strings.Join([]string{
		`{"kind":"hq.world.v1","world_id":"world.run-view-profile-test"}`,
		`{"key":"op","type":"string","required":true}`,
	}, "\n") + "\n"
	for path, content := range map[string]string{worldPath: world, acceptedPath: "", eventsPath: ""} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	profile := hqprofile.Profile{
		Kind: hqprofile.Kind, Name: "local", DeploymentID: "dep-run-view-profile-test",
		WorldPath: worldPath, AcceptedPath: acceptedPath, WorkspaceRoot: workspace, EventsPath: eventsPath,
		PollIntervalMS: 50, HealthTimeoutMS: 500,
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "local.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runRunView([]string{"list", "--profile", "local", "--profile-root", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunViewReadExecutesOneFiniteProviderActionAndBoundsContent(t *testing.T) {
	environment := runViewTestEnvironment("required", "view.open", true)
	selection, err := selectRunView("run-1", environment)
	if err != nil {
		t.Fatal(err)
	}
	provider := workeradapter.ProviderDescriptor{
		CapabilityID: "local-tool:view@1/view.read", ProviderID: "local-tool.view", ContractVersion: "1",
		DeploymentID: "view@1", ProviderKind: "executable", IntegrityDigest: "sha256:" + strings.Repeat("0", 64),
	}
	adapter := &recordingRunViewAdapter{completion: workeradapter.Completion{FinalText: "current-view-content"}}
	preparer := &recordingRunViewPreparer{prepared: workeradapter.Prepared{Adapter: adapter, Provider: &provider}}
	profile := hqprofile.Profile{WorkspaceRoot: "/workspace", EventsPath: "/events/events.jsonl"}

	var output bytes.Buffer
	if code := executeRunViewOperation(context.Background(), &output, "read", preparer, selection, profile, 7); code != 0 {
		t.Fatalf("code=%d output=%q", code, output.String())
	}
	if len(preparer.requests) != 1 || len(adapter.requests) != 1 {
		t.Fatalf("prepare=%d execute=%d", len(preparer.requests), len(adapter.requests))
	}
	var payload runViewLocalToolPayload
	if err := json.Unmarshal(preparer.requests[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ActionID != "view.read" || string(payload.Input["run_id"]) != `"run-1"` {
		t.Fatalf("payload=%s", preparer.requests[0].Payload)
	}
	var receipt runViewOperationReceipt
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Content == nil || receipt.Content.Text != "current" || !receipt.Content.TextTruncated ||
		receipt.Content.Final == nil || receipt.Content.Final.Text != "compact" || !receipt.Content.Final.TextTruncated {
		t.Fatalf("receipt=%+v", receipt)
	}
	if strings.Contains(output.String(), "provider-stream-must-not-appear") {
		t.Fatalf("raw agent stream escaped the bounded contract: %q", output.String())
	}
}

func TestRunViewSelectionCoversPolicyAndEvidenceFailures(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		selection, err := selectRunView("run-1", runViewTestEnvironment("none", "view.open", false))
		if err != nil {
			t.Fatal(err)
		}
		if selection.Policy != "none" || selection.Failure != nil || selection.OpenActionID != "" {
			t.Fatalf("selection=%+v", selection)
		}
	})

	t.Run("world mismatch", func(t *testing.T) {
		environment := runViewTestEnvironment("required", "view.open", true)
		environment.World.LocalTools = environment.World.LocalTools[:1]
		selection, err := selectRunView("run-1", environment)
		if err != nil {
			t.Fatal(err)
		}
		if selection.Failure == nil || selection.Failure.Code != "view_world_mismatch" {
			t.Fatalf("selection=%+v", selection)
		}
	})

	t.Run("corrupt canonical evidence", func(t *testing.T) {
		environment := runViewTestEnvironment("required", "view.open", true)
		environment.Results[3].InstructionID = "wrong-instruction"
		_, err := selectRunView("run-1", environment)
		if err == nil || runViewSelectionErrorCode(err) != "view_evidence_invalid" {
			t.Fatalf("error=%v code=%q", err, runViewSelectionErrorCode(err))
		}
	})

	t.Run("missing run remains distinct", func(t *testing.T) {
		environment := runViewTestEnvironment("required", "view.open", true)
		_, err := selectRunView("missing", environment)
		if err == nil || runViewSelectionErrorCode(err) != "run_not_found" {
			t.Fatalf("error=%v code=%q", err, runViewSelectionErrorCode(err))
		}
	})
}

func TestRunViewOperationUsesOneExactFiniteAction(t *testing.T) {
	environment := runViewTestEnvironment("required", "view.open", true)
	selection, err := selectRunView("run-1", environment)
	if err != nil {
		t.Fatal(err)
	}
	provider := workeradapter.ProviderDescriptor{
		CapabilityID: "local-tool:view@1/view.focus", ProviderID: "local-tool.view", ContractVersion: "1",
		DeploymentID: "view@1", ProviderKind: "executable", IntegrityDigest: "sha256:" + strings.Repeat("0", 64),
	}
	adapter := &recordingRunViewAdapter{completion: workeradapter.Completion{FinalText: "focused"}}
	preparer := &recordingRunViewPreparer{prepared: workeradapter.Prepared{Adapter: adapter, Provider: &provider}}
	profile := hqprofile.Profile{WorkspaceRoot: "/workspace", EventsPath: "/events/events.jsonl"}

	beforeInstructionCount := len(environment.Instructions)
	beforeResultCount := len(environment.Results)
	var output bytes.Buffer
	if code := executeRunViewOperation(context.Background(), &output, "focus", preparer, selection, profile, 1024); code != 0 {
		t.Fatalf("code=%d output=%q", code, output.String())
	}
	if len(preparer.requests) != 1 || len(adapter.requests) != 1 {
		t.Fatalf("prepare=%d execute=%d", len(preparer.requests), len(adapter.requests))
	}
	request := preparer.requests[0]
	if request.Target != "local-tool" || request.Operation != "run" || request.RunID != "run-1" || request.InstructionID != "ins-1" {
		t.Fatalf("request=%+v", request)
	}
	var payload runViewLocalToolPayload
	if err := json.Unmarshal(request.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ActionID != "view.focus" || string(payload.Input["view_id"]) != `"hq-run-1"` || string(payload.Input["run_id"]) != `"run-1"` {
		t.Fatalf("payload=%s", request.Payload)
	}
	if _, ok := payload.Input["native_session_id"]; ok {
		t.Fatalf("native reference escaped into provider input: %s", request.Payload)
	}
	if len(environment.Instructions) != beforeInstructionCount || len(environment.Results) != beforeResultCount {
		t.Fatal("view operation changed accepted-instruction or agent-run evidence counts")
	}
	var receipt runViewOperationReceipt
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "focused" {
		t.Fatalf("receipt=%+v", receipt)
	}
}

func TestRunViewOperationReturnsTypedBindingAndActionFailures(t *testing.T) {
	environment := runViewTestEnvironment("required", "view.open", true)
	selection, err := selectRunView("run-1", environment)
	if err != nil {
		t.Fatal(err)
	}
	profile := hqprofile.Profile{WorkspaceRoot: "/workspace", EventsPath: "/events/events.jsonl"}

	t.Run("binding unavailable", func(t *testing.T) {
		preparer := &recordingRunViewPreparer{err: errors.New("verified binding mismatch")}
		var output bytes.Buffer
		if code := executeRunViewOperation(context.Background(), &output, "open", preparer, selection, profile, 1024); code != 2 {
			t.Fatalf("code=%d output=%q", code, output.String())
		}
		var receipt runViewOperationReceipt
		if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.Failure == nil || receipt.Failure.Code != "view_unavailable" ||
			!strings.Contains(receipt.Failure.Message, "binding mismatch") {
			t.Fatalf("receipt=%+v", receipt)
		}
	})

	t.Run("finite action absent", func(t *testing.T) {
		selection.ViewTool.Actions = selection.ViewTool.Actions[:2]
		preparer := &recordingRunViewPreparer{}
		var output bytes.Buffer
		if code := executeRunViewOperation(context.Background(), &output, "read", preparer, selection, profile, 1024); code != 2 {
			t.Fatalf("code=%d output=%q", code, output.String())
		}
		var receipt runViewOperationReceipt
		if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.Failure == nil || receipt.Failure.Code != "view_operation_unavailable" || len(preparer.requests) != 0 {
			t.Fatalf("receipt=%+v prepares=%d", receipt, len(preparer.requests))
		}
	})
}

func TestRunViewOperationContractRejectsFallbackInputs(t *testing.T) {
	selection := runViewSelection{RunID: "run-1", ViewID: "hq-run-1"}
	action := core.LocalToolAction{Inputs: []core.LocalToolInput{{Name: "ambient_path", Type: "string", Required: true}}}
	if _, failure := buildRunViewOperationInput(action, selection, "focus", "/events", 1024, false); failure == nil || failure.Code != "view_contract_invalid" {
		t.Fatalf("failure=%+v", failure)
	}
	if got := runViewOperationActionID("view.open", "close"); got != "view.close" {
		t.Fatalf("action=%q", got)
	}
	if got := runViewOperationActionID("untyped", "focus"); got != "" {
		t.Fatalf("untyped action gained an implicit fallback: %q", got)
	}
}

func TestRunViewOperationUsesDeterministicIdentityWithoutNativeHint(t *testing.T) {
	environment := runViewTestEnvironment("required", "view.open", false)
	environment.World.LocalTools[1].Actions[1].Inputs = []core.LocalToolInput{
		{Name: "view_id", Type: "string", Required: true},
		{Name: "run_id", Type: "string", Required: true},
	}
	selection, err := selectRunView("run-1", environment)
	if err != nil {
		t.Fatal(err)
	}
	provider := workeradapter.ProviderDescriptor{
		CapabilityID: "local-tool:view@1/view.focus", ProviderID: "local-tool.view", ContractVersion: "1",
		DeploymentID: "view@1", ProviderKind: "executable", IntegrityDigest: "sha256:" + strings.Repeat("0", 64),
	}
	adapter := &recordingRunViewAdapter{completion: workeradapter.Completion{FinalText: "focused"}}
	preparer := &recordingRunViewPreparer{prepared: workeradapter.Prepared{Adapter: adapter, Provider: &provider}}
	beforeInstructions, _ := json.Marshal(environment.Instructions)
	beforeResults, _ := json.Marshal(environment.Results)
	var outputs [2]bytes.Buffer
	for index := range outputs {
		code := executeRunViewOperation(
			context.Background(), &outputs[index], "focus", preparer, selection,
			hqprofile.Profile{WorkspaceRoot: "/workspace", EventsPath: "/events/events.jsonl"}, 1024,
		)
		if code != 0 {
			t.Fatalf("iteration=%d code=%d output=%q", index, code, outputs[index].String())
		}
	}
	if len(adapter.requests) != 2 || outputs[0].String() != outputs[1].String() ||
		string(beforeInstructions) != mustMarshalJSON(t, environment.Instructions) ||
		string(beforeResults) != mustMarshalJSON(t, environment.Results) {
		t.Fatalf("requests=%d first=%q second=%q", len(adapter.requests), outputs[0].String(), outputs[1].String())
	}
	var payload runViewLocalToolPayload
	if err := json.Unmarshal(adapter.requests[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if string(payload.Input["view_id"]) != `"hq-run-1"` || string(payload.Input["run_id"]) != `"run-1"` {
		t.Fatalf("payload=%s", adapter.requests[0].Payload)
	}
	if _, ok := payload.Input["native_session_id"]; ok {
		t.Fatalf("native hint was invented: %s", adapter.requests[0].Payload)
	}
}

func mustMarshalJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestRunViewOperationRejectsRequiredNativeReferenceBeforeProviderExecution(t *testing.T) {
	environment := runViewTestEnvironment("required", "view.open", true)
	environment.World.LocalTools[1].Actions[1].Inputs = append(
		environment.World.LocalTools[1].Actions[1].Inputs,
		core.LocalToolInput{Name: "native_session_id", Type: "string", Required: true},
	)
	selection, err := selectRunView("run-1", environment)
	if err != nil {
		t.Fatal(err)
	}
	preparer := &recordingRunViewPreparer{}
	var output bytes.Buffer
	code := executeRunViewOperation(
		context.Background(), &output, "focus", preparer, selection,
		hqprofile.Profile{WorkspaceRoot: "/workspace", EventsPath: "/events/events.jsonl"}, 1024,
	)
	if code != 2 || len(preparer.requests) != 0 {
		t.Fatalf("code=%d output=%q prepares=%d", code, output.String(), len(preparer.requests))
	}
	var receipt runViewOperationReceipt
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Failure == nil || receipt.Failure.Code != "view_contract_invalid" {
		t.Fatalf("receipt=%+v", receipt)
	}
}

func TestRunViewDestroyedSameProviderFailsClosedWithoutFallback(t *testing.T) {
	environment := runViewTestEnvironment("required", "view.open", true)
	selection, err := selectRunView("run-1", environment)
	if err != nil {
		t.Fatal(err)
	}
	provider := workeradapter.ProviderDescriptor{
		CapabilityID: "local-tool:view@1/view.focus", ProviderID: "local-tool.view", ContractVersion: "1",
		DeploymentID: "view@1", ProviderKind: "executable", IntegrityDigest: "sha256:" + strings.Repeat("0", 64),
	}
	beforeInstructions, _ := json.Marshal(environment.Instructions)
	beforeResults, _ := json.Marshal(environment.Results)
	for _, operation := range []string{"focus", "read", "close"} {
		t.Run(operation, func(t *testing.T) {
			adapter := &recordingRunViewAdapter{err: errors.New("generic view was destroyed")}
			preparer := &recordingRunViewPreparer{prepared: workeradapter.Prepared{Adapter: adapter, Provider: &provider}}
			var output bytes.Buffer
			code := executeRunViewOperation(
				context.Background(), &output, operation, preparer, selection,
				hqprofile.Profile{WorkspaceRoot: "/workspace", EventsPath: "/events/events.jsonl"}, 1024,
			)
			if code != 2 || len(preparer.requests) != 1 || len(adapter.requests) != 1 {
				t.Fatalf("code=%d output=%q prepares=%d executes=%d", code, output.String(), len(preparer.requests), len(adapter.requests))
			}
			var receipt runViewOperationReceipt
			if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Failure == nil || receipt.Failure.Code != "view_provider_failed" ||
				!strings.Contains(receipt.Failure.Message, "destroyed") {
				t.Fatalf("receipt=%+v", receipt)
			}
			assertRunViewPublicJSONShape(t, output.Bytes())
		})
	}
	if string(beforeInstructions) != mustMarshalJSON(t, environment.Instructions) || string(beforeResults) != mustMarshalJSON(t, environment.Results) {
		t.Fatal("destroyed-view failures changed canonical evidence")
	}
}

func TestRunViewUsesOptionalNativeHintOnlyAfterProviderMatch(t *testing.T) {
	environment := runViewTestEnvironment("required", "view.open", true)
	environment.World.LocalTools[1].Actions[1].Inputs = append(
		environment.World.LocalTools[1].Actions[1].Inputs,
		core.LocalToolInput{Name: "native_session_id", Type: "string"},
	)
	selection, err := selectRunView("run-1", environment)
	if err != nil {
		t.Fatal(err)
	}
	provider := workeradapter.ProviderDescriptor{
		CapabilityID: "local-tool:view@1/view.focus", ProviderID: "local-tool.view", ContractVersion: "1",
		DeploymentID: "view@1", ProviderKind: "executable", IntegrityDigest: "sha256:" + strings.Repeat("0", 64),
	}
	adapter := &recordingRunViewAdapter{completion: workeradapter.Completion{FinalText: "focused"}}
	preparer := &recordingRunViewPreparer{prepared: workeradapter.Prepared{Adapter: adapter, Provider: &provider}}
	var output bytes.Buffer
	code := executeRunViewOperation(
		context.Background(), &output, "focus", preparer, selection,
		hqprofile.Profile{WorkspaceRoot: "/workspace", EventsPath: "/events/events.jsonl"}, 1024,
	)
	if code != 0 || len(adapter.requests) != 1 {
		t.Fatalf("code=%d output=%q executes=%d", code, output.String(), len(adapter.requests))
	}
	var payload runViewLocalToolPayload
	if err := json.Unmarshal(adapter.requests[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if string(payload.Input["native_session_id"]) != `"native-1"` {
		t.Fatalf("payload=%s", adapter.requests[0].Payload)
	}
	assertRunViewPublicJSONShape(t, output.Bytes())
}

func TestRunViewRejectsStaleProviderBeforeEffectAndPreservesTypedProviderFailure(t *testing.T) {
	environment := runViewTestEnvironment("required", "view.open", true)
	selection, err := selectRunView("run-1", environment)
	if err != nil {
		t.Fatal(err)
	}
	profile := hqprofile.Profile{WorkspaceRoot: "/workspace", EventsPath: "/events/events.jsonl"}

	t.Run("provider mismatch", func(t *testing.T) {
		provider := workeradapter.ProviderDescriptor{
			CapabilityID: "local-tool:view@1/view.focus", ProviderID: "local-tool.view", ContractVersion: "1",
			DeploymentID: "view@1", ProviderKind: "executable", IntegrityDigest: "sha256:" + strings.Repeat("1", 64),
		}
		adapter := &recordingRunViewAdapter{}
		preparer := &recordingRunViewPreparer{prepared: workeradapter.Prepared{Adapter: adapter, Provider: &provider}}
		var output bytes.Buffer
		if code := executeRunViewOperation(context.Background(), &output, "focus", preparer, selection, profile, 1024); code != 2 {
			t.Fatalf("code=%d output=%q", code, output.String())
		}
		var receipt runViewOperationReceipt
		if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.Failure == nil || receipt.Failure.Code != "view_reference_stale" || len(adapter.requests) != 0 {
			t.Fatalf("receipt=%+v executes=%d", receipt, len(adapter.requests))
		}
	})

	t.Run("provider reports stale native session", func(t *testing.T) {
		provider := workeradapter.ProviderDescriptor{
			CapabilityID: "local-tool:view@1/view.focus", ProviderID: "local-tool.view", ContractVersion: "1",
			DeploymentID: "view@1", ProviderKind: "executable", IntegrityDigest: "sha256:" + strings.Repeat("0", 64),
		}
		adapter := &recordingRunViewAdapter{err: workeradapter.NewBlockedError("view_reference_stale", "native view no longer exists")}
		preparer := &recordingRunViewPreparer{prepared: workeradapter.Prepared{Adapter: adapter, Provider: &provider}}
		var output bytes.Buffer
		if code := executeRunViewOperation(context.Background(), &output, "focus", preparer, selection, profile, 1024); code != 2 {
			t.Fatalf("code=%d output=%q", code, output.String())
		}
		var receipt runViewOperationReceipt
		if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.Failure == nil || receipt.Failure.Code != "view_reference_stale" || len(adapter.requests) != 1 {
			t.Fatalf("receipt=%+v executes=%d", receipt, len(adapter.requests))
		}
	})
}

func TestRunViewRevalidatesOpenActionDependencyAcrossFiniteOperations(t *testing.T) {
	environment := runViewTestEnvironment("required", "view.open", true)
	root := t.TempDir()
	executable := filepath.Join(root, "view-helper")
	contents := []byte("verified view helper")
	if err := os.WriteFile(executable, contents, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(contents))
	binding := localtool.VerifiedBinding{
		BindingRef: "local-tool.view-helper", ResourceID: "app.view-helper", ContractVersion: "1",
		Executable: executable, MaterialDigest: digest, DeploymentID: "app.view-helper@1:" + digest,
		DeclarationEventID: "view-helper-declared", SelectionEventID: "view-helper-selected",
	}
	registryPath := filepath.Join(root, "verified-executable-bindings.json")
	registry, err := json.Marshal(localtool.VerifiedBindings{
		Schema: localtool.VerifiedBindingsSchema, Entries: []localtool.VerifiedBinding{binding},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryPath, registry, 0o600); err != nil {
		t.Fatal(err)
	}
	environment.World.LocalTools[1].Bindings = []core.LocalToolBinding{{
		Name: "view_helper", BindingRef: binding.BindingRef, BindingContractVersion: binding.ContractVersion,
	}}
	environment.Results[1].View.Provider.Dependencies = []worker.ProviderDependencyEvidence{{
		Name: "view_helper", ProviderID: binding.BindingRef, ContractVersion: binding.ContractVersion,
		DeploymentID: binding.DeploymentID, ProviderKind: "executable", IntegrityDigest: binding.MaterialDigest,
	}}
	selection, err := selectRunView("run-1", environment)
	if err != nil {
		t.Fatal(err)
	}
	provider := workeradapter.ProviderDescriptor{
		CapabilityID: "local-tool:view@1/view.focus", ProviderID: "local-tool.view", ContractVersion: "1",
		DeploymentID: "view@1", ProviderKind: "executable", IntegrityDigest: "sha256:" + strings.Repeat("0", 64),
	}
	profile := hqprofile.Profile{
		WorkspaceRoot: "/workspace", EventsPath: "/events/events.jsonl", ExecutableBindingsPath: registryPath,
	}

	t.Run("matching open dependency remains valid", func(t *testing.T) {
		adapter := &recordingRunViewAdapter{completion: workeradapter.Completion{FinalText: "focused"}}
		preparer := &recordingRunViewPreparer{prepared: workeradapter.Prepared{Adapter: adapter, Provider: &provider}}
		var output bytes.Buffer
		if code := executeRunViewOperation(context.Background(), &output, "focus", preparer, selection, profile, 1024); code != 0 {
			t.Fatalf("code=%d output=%q", code, output.String())
		}
		if len(adapter.requests) != 1 {
			t.Fatalf("executes=%d", len(adapter.requests))
		}
	})

	t.Run("drifted open dependency blocks before effect", func(t *testing.T) {
		if err := os.WriteFile(executable, []byte("drifted view helper"), 0o700); err != nil {
			t.Fatal(err)
		}
		adapter := &recordingRunViewAdapter{}
		preparer := &recordingRunViewPreparer{prepared: workeradapter.Prepared{Adapter: adapter, Provider: &provider}}
		var output bytes.Buffer
		if code := executeRunViewOperation(context.Background(), &output, "focus", preparer, selection, profile, 1024); code != 2 {
			t.Fatalf("code=%d output=%q", code, output.String())
		}
		var receipt runViewOperationReceipt
		if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.Failure == nil || receipt.Failure.Code != "view_reference_stale" || len(adapter.requests) != 0 {
			t.Fatalf("receipt=%+v executes=%d", receipt, len(adapter.requests))
		}
	})
}

func TestRunViewCloseOpenThenFocusReadCloseUsesStableGenericViewID(t *testing.T) {
	environment := runViewTestEnvironment("required", "view.open", true)
	provider := workeradapter.ProviderDescriptor{
		CapabilityID: "local-tool:view@1", ProviderID: "local-tool.view", ContractVersion: "1",
		DeploymentID: "view@1", ProviderKind: "executable", IntegrityDigest: "sha256:" + strings.Repeat("0", 64),
	}
	newNativeReference := "native-r2"
	adapter := &recordingRunViewAdapter{completion: workeradapter.Completion{FinalText: "current", NativeSessionID: &newNativeReference}}
	preparer := &recordingRunViewPreparer{prepared: workeradapter.Prepared{Adapter: adapter, Provider: &provider}}
	profile := hqprofile.Profile{WorkspaceRoot: "/workspace", EventsPath: "/events/events.jsonl"}
	beforeInstructions, _ := json.Marshal(environment.Instructions)
	beforeResults, _ := json.Marshal(environment.Results)

	for _, operation := range []string{"close", "open"} {
		selection, err := selectRunView("run-1", environment)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if code := executeRunViewOperation(context.Background(), &output, operation, preparer, selection, profile, 1024); code != 0 {
			t.Fatalf("operation=%s code=%d output=%q", operation, code, output.String())
		}
		assertRunViewPublicJSONShape(t, output.Bytes())
	}
	for _, operation := range []string{"focus", "read", "close"} {
		selection, err := selectRunView("run-1", environment)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if code := executeRunViewOperation(context.Background(), &output, operation, preparer, selection, profile, 1024); code != 0 {
			t.Fatalf("operation=%s code=%d output=%q", operation, code, output.String())
		}
		assertRunViewPublicJSONShape(t, output.Bytes())
	}
	if len(preparer.requests) != 5 || len(adapter.requests) != 5 {
		t.Fatalf("prepares=%d executes=%d", len(preparer.requests), len(adapter.requests))
	}
	for _, request := range adapter.requests {
		var payload runViewLocalToolPayload
		if err := json.Unmarshal(request.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if string(payload.Input["view_id"]) != `"hq-run-1"` || string(payload.Input["run_id"]) != `"run-1"` {
			t.Fatalf("payload=%s", request.Payload)
		}
		if _, ok := payload.Input["native_session_id"]; ok {
			t.Fatalf("native replacement became public state: %s", request.Payload)
		}
	}
	if string(beforeInstructions) != mustMarshalJSON(t, environment.Instructions) || string(beforeResults) != mustMarshalJSON(t, environment.Results) {
		t.Fatal("close/open sequence changed canonical evidence")
	}
}

func TestRunViewSixOperationsPreserveEvidenceAndAreIdempotent(t *testing.T) {
	environment := runViewTestEnvironment("required", "view.open", true)
	eventsFile, err := os.CreateTemp(t.TempDir(), "events-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range environment.Results {
		if err := worker.EncodeJSONLine(eventsFile, row); err != nil {
			t.Fatal(err)
		}
	}
	if err := eventsFile.Close(); err != nil {
		t.Fatal(err)
	}
	environment.Profile = hqprofile.Profile{WorkspaceRoot: "/workspace", EventsPath: eventsFile.Name()}
	selection, err := selectRunView("run-1", environment)
	if err != nil {
		t.Fatal(err)
	}
	provider := workeradapter.ProviderDescriptor{
		CapabilityID: "local-tool:view@1", ProviderID: "local-tool.view", ContractVersion: "1",
		DeploymentID: "view@1", ProviderKind: "executable", IntegrityDigest: "sha256:" + strings.Repeat("0", 64),
	}
	adapter := &recordingRunViewAdapter{completion: workeradapter.Completion{FinalText: "current"}}
	preparer := &recordingRunViewPreparer{prepared: workeradapter.Prepared{Adapter: adapter, Provider: &provider}}
	beforeInstructions, _ := json.Marshal(environment.Instructions)
	beforeResults, _ := json.Marshal(environment.Results)

	var listOutputs [2]bytes.Buffer
	for index := range listOutputs {
		var stderr bytes.Buffer
		if code := listRunViews(context.Background(), &listOutputs[index], &stderr, environment, preparer, 0, 1024); code != 0 {
			t.Fatalf("list iteration=%d code=%d stderr=%q", index, code, stderr.String())
		}
		assertRunViewPublicJSONShape(t, listOutputs[index].Bytes())
	}
	if listOutputs[0].String() != listOutputs[1].String() {
		t.Fatalf("list outputs differ:\n%s\n%s", listOutputs[0].String(), listOutputs[1].String())
	}

	for _, operation := range []string{"open", "focus", "read", "close"} {
		var outputs [2]bytes.Buffer
		for index := range outputs {
			if code := executeRunViewOperation(context.Background(), &outputs[index], operation, preparer, selection, environment.Profile, 1024); code != 0 {
				t.Fatalf("operation=%s iteration=%d code=%d output=%q", operation, index, code, outputs[index].String())
			}
			assertRunViewPublicJSONShape(t, outputs[index].Bytes())
		}
		if outputs[0].String() != outputs[1].String() {
			t.Fatalf("operation=%s outputs differ:\n%s\n%s", operation, outputs[0].String(), outputs[1].String())
		}
	}

	var tailOutputs [2]bytes.Buffer
	for index := range tailOutputs {
		if _, _, failure := prepareRunViewOperation(context.Background(), preparer, selection, environment.Profile, "open", 1024, false); failure != nil {
			t.Fatalf("tail iteration=%d prepare failure=%+v", index, failure)
		}
		var stderr bytes.Buffer
		if code := tailRunView(context.Background(), &tailOutputs[index], &stderr, eventsFile.Name(), selection, false, time.Millisecond, 1024); code != 0 {
			t.Fatalf("tail iteration=%d code=%d stderr=%q", index, code, stderr.String())
		}
		assertRunViewPublicJSONShape(t, tailOutputs[index].Bytes())
	}
	if tailOutputs[0].String() != tailOutputs[1].String() {
		t.Fatalf("tail outputs differ:\n%s\n%s", tailOutputs[0].String(), tailOutputs[1].String())
	}
	if len(preparer.requests) != 12 || len(adapter.requests) != 8 {
		t.Fatalf("prepares=%d executes=%d", len(preparer.requests), len(adapter.requests))
	}
	if string(beforeInstructions) != mustMarshalJSON(t, environment.Instructions) || string(beforeResults) != mustMarshalJSON(t, environment.Results) {
		t.Fatal("six operations changed accepted-instruction or agent-run evidence")
	}
}

func assertRunViewPublicJSONShape(t *testing.T, output []byte) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		assertRunViewPublicKeys(t, record)
		allowed := map[string]bool{}
		switch {
		case record["operation"] != nil:
			for _, key := range []string{"version", "operation", "run_id", "view_id", "instruction_id", "lifecycle", "policy", "status", "content", "failure"} {
				allowed[key] = true
			}
		case record["seq"] != nil:
			for _, key := range []string{"version", "run_id", "view_id", "seq", "recorded_at", "lifecycle", "final", "failure"} {
				allowed[key] = true
			}
		default:
			for _, key := range []string{"version", "run_id", "view_id", "instruction_id", "lifecycle", "policy", "available", "final", "failure"} {
				allowed[key] = true
			}
		}
		for key := range record {
			if !allowed[key] {
				t.Fatalf("public run-view JSON contains unsupported top-level key %q: %s", key, output)
			}
		}
	}
}

func assertRunViewPublicKeys(t *testing.T, value any) {
	t.Helper()
	switch value := value.(type) {
	case map[string]any:
		for key, nested := range value {
			lower := strings.ToLower(key)
			for _, forbidden := range []string{"view_reference", "native", "provider", "executable"} {
				if strings.Contains(lower, forbidden) {
					t.Fatalf("public run-view JSON contains forbidden key %q", key)
				}
			}
			assertRunViewPublicKeys(t, nested)
		}
	case []any:
		for _, nested := range value {
			assertRunViewPublicKeys(t, nested)
		}
	}
}

func TestProjectedRunViewEventsSuppressRawStreams(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	raw := "provider-stream-must-not-appear"
	if event, ok := projectedRunViewEvent(worker.ResultRow{RunID: "run-1", Kind: worker.ResultStdout, Seq: 2, RecordedAt: now, Message: &raw}, "hq-run-1", 16); ok || event.Version != "" {
		t.Fatalf("stdout projected: %+v", event)
	}
	final := &worker.FinalResult{Text: "compact-final"}
	event, ok := projectedRunViewEvent(worker.ResultRow{RunID: "run-1", Kind: worker.ResultCompleted, Seq: 3, RecordedAt: now, Final: final}, "hq-run-1", 7)
	if !ok || event.Final == nil || event.Final.Text != "compact" || !event.Final.TextTruncated {
		t.Fatalf("event=%+v", event)
	}
}

func TestGenericRunViewProjectionHasNoProviderNamedBranches(t *testing.T) {
	content, err := os.ReadFile("run_view_projection.go")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(content))
	for _, forbidden := range []string{"herdr", "codex", "claude"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("generic projection contains provider name %q", forbidden)
		}
	}
}

func runViewTestEnvironment(policy, openActionID string, withReference bool) runViewEnvironment {
	stringArg := func(value string) *string { return &value }
	viewTool := core.LocalToolDefinition{
		ToolID: "view", ToolVersion: "1", BindingRef: "local-tool.view", BindingContractVersion: "1",
		Actions: []core.LocalToolAction{
			{ActionID: "view.open", Inputs: []core.LocalToolInput{{Name: "view_id", Type: "string", Required: true}, {Name: "run_id", Type: "string", Required: true}, {Name: "events_path", Type: "string", Required: true}}},
			{ActionID: "view.focus", Inputs: []core.LocalToolInput{{Name: "view_id", Type: "string", Required: true}, {Name: "run_id", Type: "string", Required: true}}},
			{ActionID: "view.read", Inputs: []core.LocalToolInput{{Name: "view_id", Type: "string", Required: true}, {Name: "run_id", Type: "string", Required: true}, {Name: "max_bytes", Type: "integer", Required: true}}},
			{ActionID: "view.close", Inputs: []core.LocalToolInput{{Name: "view_id", Type: "string", Required: true}, {Name: "run_id", Type: "string", Required: true}}},
		},
	}
	mainAction := core.LocalToolAction{ActionID: "run"}
	if policy != "none" {
		mainAction.RunView = &core.LocalToolRunView{Policy: policy, ToolID: "view", ToolVersion: "1", ActionID: openActionID}
	}
	mainTool := core.LocalToolDefinition{
		ToolID: "main", ToolVersion: "1", BindingRef: "local-tool.main", BindingContractVersion: "1",
		Actions: []core.LocalToolAction{mainAction},
	}
	instruction := worker.Instruction{
		ID: "ins-1", Version: worker.InstructionVersionV1, Op: "run", Target: "local-tool",
		Payload: json.RawMessage(`{"tool_id":"main","tool_version":"1","action_id":"run","input":{}}`), CreatedAt: "2026-07-20T12:00:00Z",
	}
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	results := []worker.ResultRow{
		{EventID: "e0", Version: worker.ResultVersionV1, RunID: "run-1", InstructionID: "ins-1", Target: "local-tool", Kind: worker.ResultAccepted, Seq: 0, RecordedAt: now},
		{EventID: "e1", Version: worker.ResultVersionV1, RunID: "run-1", InstructionID: "ins-1", Target: "local-tool", Kind: worker.ResultStarted, Seq: 1, RecordedAt: now.Add(time.Second)},
		{EventID: "e2", Version: worker.ResultVersionV1, RunID: "run-1", InstructionID: "ins-1", Target: "local-tool", Kind: worker.ResultStdout, Seq: 2, RecordedAt: now.Add(2 * time.Second), Message: stringArg("provider-stream-must-not-appear")},
		{EventID: "e3", Version: worker.ResultVersionV1, RunID: "run-1", InstructionID: "ins-1", Target: "local-tool", Kind: worker.ResultCompleted, Seq: 3, RecordedAt: now.Add(3 * time.Second), Final: &worker.FinalResult{Text: "compact-final"}},
	}
	if withReference {
		results[1].View = &worker.RunViewEvidence{
			Version: worker.RunViewVersionV1, Policy: "required", NativeSessionID: "native-1",
			Provider: worker.ProviderEvidence{
				CapabilityID: "local-tool:view@1/view.open", ProviderID: "local-tool.view", ContractVersion: "1",
				DeploymentID: "view@1", ProviderKind: "executable", IntegrityDigest: "sha256:" + strings.Repeat("0", 64),
			},
		}
	}
	return runViewEnvironment{World: &core.JsonlWorld{LocalTools: []core.LocalToolDefinition{mainTool, viewTool}}, Instructions: []worker.Instruction{instruction}, Results: results}
}

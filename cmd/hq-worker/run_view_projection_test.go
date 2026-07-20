package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"hq/internal/core"
	"hq/internal/hqprofile"
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

func TestRunViewSelectionAndReadUseOnlyCanonicalBoundedEvidence(t *testing.T) {
	environment := runViewTestEnvironment("required", "view.open", true)
	beforeInstructions, _ := json.Marshal(environment.Instructions)
	beforeResults, _ := json.Marshal(environment.Results)

	selection, err := selectRunView("run-1", environment)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Policy != "required" || selection.ViewID != "hq-run-1" || selection.NativeSessionID != "native-1" {
		t.Fatalf("selection=%+v", selection)
	}

	var output bytes.Buffer
	if code := emitRunViewRead(&output, selection, 4); code != 0 {
		t.Fatalf("code=%d output=%q", code, output.String())
	}
	var receipt runViewOperationReceipt
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Content == nil || receipt.Content.Final == nil || receipt.Content.Final.Text != "comp" || !receipt.Content.Final.TextTruncated {
		t.Fatalf("receipt=%+v", receipt)
	}
	if strings.Contains(output.String(), "provider-stream-must-not-appear") {
		t.Fatalf("raw provider output escaped the bounded contract: %q", output.String())
	}
	afterInstructions, _ := json.Marshal(environment.Instructions)
	afterResults, _ := json.Marshal(environment.Results)
	if !bytes.Equal(beforeInstructions, afterInstructions) || !bytes.Equal(beforeResults, afterResults) {
		t.Fatal("read projection changed canonical instructions or result evidence")
	}
}

func TestRunViewSelectionSupportsOptionalPolicyWithoutDispatch(t *testing.T) {
	environment := runViewTestEnvironment("optional", "view.open", false)
	selection, err := selectRunView("run-1", environment)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Policy != "optional" || selection.Failure != nil || selection.OpenActionID != "view.open" {
		t.Fatalf("selection=%+v", selection)
	}
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
	if payload.ActionID != "view.focus" || string(payload.Input["native_session_id"]) != `"native-1"` || string(payload.Input["run_id"]) != `"run-1"` {
		t.Fatalf("payload=%s", request.Payload)
	}
	if len(environment.Instructions) != beforeInstructionCount || len(environment.Results) != beforeResultCount {
		t.Fatal("view operation changed accepted-instruction or agent-run evidence counts")
	}
	var receipt runViewOperationReceipt
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "focused" || receipt.ViewReference != "native-1" {
		t.Fatalf("receipt=%+v", receipt)
	}
}

func TestRunViewOperationContractRejectsFallbackInputs(t *testing.T) {
	selection := runViewSelection{RunID: "run-1", ViewID: "hq-run-1", NativeSessionID: "native-1"}
	action := core.LocalToolAction{Inputs: []core.LocalToolInput{{Name: "ambient_path", Type: "string", Required: true}}}
	if _, failure := buildRunViewOperationInput(action, selection, "/events", 1024, false); failure == nil || failure.Code != "view_contract_invalid" {
		t.Fatalf("failure=%+v", failure)
	}
	if got := runViewOperationActionID("view.open", "close"); got != "view.close" {
		t.Fatalf("action=%q", got)
	}
	if got := runViewOperationActionID("untyped", "focus"); got != "" {
		t.Fatalf("untyped action gained an implicit fallback: %q", got)
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
			{ActionID: "view.focus", Inputs: []core.LocalToolInput{{Name: "view_id", Type: "string", Required: true}, {Name: "run_id", Type: "string", Required: true}, {Name: "native_session_id", Type: "string", Required: true}}},
			{ActionID: "view.close", Inputs: []core.LocalToolInput{{Name: "view_id", Type: "string", Required: true}, {Name: "run_id", Type: "string", Required: true}, {Name: "native_session_id", Type: "string", Required: true}}},
		},
	}
	mainTool := core.LocalToolDefinition{
		ToolID: "main", ToolVersion: "1", BindingRef: "local-tool.main", BindingContractVersion: "1",
		Actions: []core.LocalToolAction{{
			ActionID: "run", RunView: &core.LocalToolRunView{Policy: policy, ToolID: "view", ToolVersion: "1", ActionID: openActionID},
		}},
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

package localtool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"hq/internal/core"
	"hq/internal/worker/adapter"
)

func TestLocalToolHelperProcess(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	args := os.Args[separator+1:]
	switch args[0] {
	case "echo":
		_, _ = io.WriteString(os.Stdout, strings.Join(args[1:], "|"))
	case "env":
		_, _ = io.WriteString(os.Stdout, os.Getenv("HQ_LOCAL_TOOL_POISON"))
	case "stdin":
		data, _ := io.ReadAll(os.Stdin)
		_, _ = os.Stdout.Write(data)
	case "json":
		_, _ = io.WriteString(os.Stdout, `{"session":{"id":"session-1"}}`)
	case "badjson":
		_, _ = io.WriteString(os.Stdout, `{broken`)
	case "jsonl":
		_, _ = io.WriteString(os.Stdout, "{\"session\":\"session-1\"}\n{\"session\":\"session-2\"}\n")
	case "finaljsonl":
		_, _ = io.WriteString(os.Stdout, "{\"type\":\"progress\",\"message\":\"raw-progress\"}\n{\"session\":\"session-final\",\"result\":\"compact final\"}\n")
	case "badjsonl":
		_, _ = io.WriteString(os.Stdout, "{\"value\":1}\nnope\n")
	case "stdout":
		count, _ := strconv.Atoi(args[1])
		_, _ = io.WriteString(os.Stdout, strings.Repeat("x", count))
	case "stderr":
		count, _ := strconv.Atoi(args[1])
		_, _ = io.WriteString(os.Stderr, strings.Repeat("x", count))
	case "sleep":
		milliseconds, _ := strconv.Atoi(args[1])
		time.Sleep(time.Duration(milliseconds) * time.Millisecond)
	case "exit":
		os.Exit(7)
	}
	os.Exit(0)
}

func TestPreparerExecutesExactBindingWithLiteralArgvAndEmptyEnvironment(t *testing.T) {
	t.Setenv("HQ_LOCAL_TOOL_POISON", "must-not-leak")
	executable, registryPath := executableBindingFixture(t, "local-tool.dummy", "1", "app.dummy")
	fakeDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeDirectory, filepath.Base(executable)), []byte("poisoned PATH executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	argument := `;&|$()^%PATH%`
	tool := helperTool("echo", "text", core.LocalToolInput{Name: "value", Type: "string", Required: true})
	tool.Actions[0].Argv = append(tool.Actions[0].Argv, core.LocalToolArg{Field: stringRef("value")})
	prepared := prepare(t, tool, registryPath, map[string]any{"value": argument})
	var stdout string
	completion, err := prepared.Adapter.Run(context.Background(), requestFor(tool, map[string]any{"value": argument}), func(output adapter.Output) error {
		if output.Kind == adapter.OutputStdout {
			stdout += output.Message
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stdout != argument || completion.FinalText != argument {
		t.Fatalf("literal argv was reinterpreted: stdout=%q completion=%q", stdout, completion.FinalText)
	}

	envTool := helperTool("env", "text")
	envPrepared := prepare(t, envTool, registryPath, nil)
	envCompletion, err := envPrepared.Adapter.Run(context.Background(), requestFor(envTool, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if envCompletion.FinalText != "local tool completed" {
		t.Fatalf("ambient environment leaked: %q", envCompletion.FinalText)
	}
}

func TestPreparerRejectsInputAndBindingMismatchBeforeExecution(t *testing.T) {
	_, registryPath := executableBindingFixture(t, "local-tool.dummy", "1", "app.dummy")
	tool := helperTool("echo", "text", core.LocalToolInput{Name: "value", Type: "integer", Required: true})
	tool.Actions[0].Argv = append(tool.Actions[0].Argv, core.LocalToolArg{Field: stringRef("value")})
	tests := []struct {
		name  string
		input map[string]any
		code  string
	}{
		{"missing", nil, "local_tool_input_missing"},
		{"extra", map[string]any{"value": 1, "other": true}, "local_tool_input_unknown"},
		{"wrong-type", map[string]any{"value": "1"}, "local_tool_input_invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := (Preparer{World: &core.JsonlWorld{LocalTools: []core.LocalToolDefinition{tool}}, BindingsPath: registryPath}).Prepare(context.Background(), requestFor(tool, test.input))
			assertFailureCode(t, err, test.code)
		})
	}
	tool.BindingContractVersion = "2"
	_, err := (Preparer{World: &core.JsonlWorld{LocalTools: []core.LocalToolDefinition{tool}}, BindingsPath: registryPath}).Prepare(context.Background(), requestFor(tool, map[string]any{"value": 1}))
	assertFailureCode(t, err, "binding_contract_mismatch")
}

func TestPreparerRejectsUnknownToolVersionActionAndBinding(t *testing.T) {
	_, registryPath := executableBindingFixture(t, "local-tool.dummy", "1", "app.dummy")
	tool := helperTool("echo", "text")
	world := &core.JsonlWorld{LocalTools: []core.LocalToolDefinition{tool}}
	request := requestFor(tool, nil)
	var payloadValue map[string]any
	_ = json.Unmarshal(request.Payload, &payloadValue)
	for name, change := range map[string]struct {
		field string
		value any
		code  string
	}{
		"tool":    {"tool_id", "unknown", "local_tool_unknown"},
		"version": {"tool_version", "2", "local_tool_unknown"},
		"action":  {"action_id", "unknown", "local_tool_action_unknown"},
	} {
		t.Run(name, func(t *testing.T) {
			copy := map[string]any{}
			for key, item := range payloadValue {
				copy[key] = item
			}
			copy[change.field] = change.value
			request.Payload, _ = json.Marshal(copy)
			_, err := (Preparer{World: world, BindingsPath: registryPath}).Prepare(context.Background(), request)
			assertFailureCode(t, err, change.code)
		})
	}
	missing := tool
	missing.BindingRef = "local-tool.missing"
	_, err := (Preparer{World: &core.JsonlWorld{LocalTools: []core.LocalToolDefinition{missing}}, BindingsPath: registryPath}).Prepare(context.Background(), requestFor(missing, nil))
	assertFailureCode(t, err, "binding_unavailable")
}

func TestPreparedAdapterReverifiesDigestBeforeLaunch(t *testing.T) {
	executable, registryPath := executableBindingFixture(t, "local-tool.dummy", "1", "app.dummy")
	tool := helperTool("echo", "text")
	prepared := prepare(t, tool, registryPath, nil)
	if err := os.WriteFile(executable, []byte("tampered after prepare"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := prepared.Adapter.Run(context.Background(), requestFor(tool, nil), nil)
	assertFailureCode(t, err, "binding_digest_mismatch")
}

func TestPreparedAdapterEnforcesOutputTimeoutAndCancellation(t *testing.T) {
	_, registryPath := executableBindingFixture(t, "local-tool.dummy", "1", "app.dummy")
	tests := []struct {
		name, mode, argument, code string
	}{
		{"stdout", "stdout", "64", "stdout_limit_exceeded"},
		{"stderr", "stderr", "64", "stderr_limit_exceeded"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tool := helperTool(test.mode, "text")
			tool.Actions[0].Argv = append(tool.Actions[0].Argv, core.LocalToolArg{Literal: stringRef(test.argument)})
			tool.Actions[0].Limits.StdoutBytes = 8
			tool.Actions[0].Limits.StderrBytes = 8
			prepared := prepare(t, tool, registryPath, nil)
			_, err := prepared.Adapter.Run(context.Background(), requestFor(tool, nil), nil)
			assertFailureCode(t, err, test.code)
		})
	}

	timeoutTool := helperTool("sleep", "text")
	timeoutTool.Actions[0].Argv = append(timeoutTool.Actions[0].Argv, core.LocalToolArg{Literal: stringRef("250")})
	timeoutTool.Actions[0].Limits.TimeoutMS = 20
	prepared := prepare(t, timeoutTool, registryPath, nil)
	_, err := prepared.Adapter.Run(context.Background(), requestFor(timeoutTool, nil), nil)
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("expected deadline error, got %v", err)
	}

	cancelTool := helperTool("sleep", "text")
	cancelTool.Actions[0].Argv = append(cancelTool.Actions[0].Argv, core.LocalToolArg{Literal: stringRef("250")})
	prepared = prepare(t, cancelTool, registryPath, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = prepared.Adapter.Run(ctx, requestFor(cancelTool, nil), nil)
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestPreparedAdapterUsesBoundedDeclaredStdin(t *testing.T) {
	_, registryPath := executableBindingFixture(t, "local-tool.dummy", "1", "app.dummy")
	tool := helperTool("stdin", "text", core.LocalToolInput{Name: "body", Type: "string", Required: true})
	tool.Actions[0].Stdin = core.LocalToolStdin{Mode: "field", Field: "body", MaxBytes: 4}
	prepared := prepare(t, tool, registryPath, map[string]any{"body": "safe"})
	completion, err := prepared.Adapter.Run(context.Background(), requestFor(tool, map[string]any{"body": "safe"}), nil)
	if err != nil || completion.FinalText != "safe" {
		t.Fatalf("completion=%+v err=%v", completion, err)
	}
	_, err = (Preparer{World: &core.JsonlWorld{LocalTools: []core.LocalToolDefinition{tool}}, BindingsPath: registryPath}).Prepare(context.Background(), requestFor(tool, map[string]any{"body": "oversize"}))
	assertFailureCode(t, err, "stdin_limit_exceeded")
}

func TestPreparedAdapterValidatesJSONJSONLAndNativeReferences(t *testing.T) {
	_, registryPath := executableBindingFixture(t, "local-tool.dummy", "1", "app.dummy")
	valid := helperTool("json", "json")
	valid.Actions[0].NativeRefs.Session = &core.LocalToolNativeSelector{Source: "stdout", Path: []string{"session", "id"}}
	prepared := prepare(t, valid, registryPath, nil)
	completion, err := prepared.Adapter.Run(context.Background(), requestFor(valid, nil), nil)
	if err != nil || completion.NativeSessionID == nil || *completion.NativeSessionID != "session-1" {
		t.Fatalf("completion=%+v err=%v", completion, err)
	}
	jsonl := helperTool("jsonl", "jsonl")
	jsonl.Actions[0].NativeRefs.Session = &core.LocalToolNativeSelector{Source: "stdout", Path: []string{"session"}}
	prepared = prepare(t, jsonl, registryPath, nil)
	completion, err = prepared.Adapter.Run(context.Background(), requestFor(jsonl, nil), nil)
	if err != nil || completion.NativeSessionID == nil || *completion.NativeSessionID != "session-2" {
		t.Fatalf("JSONL last-record selector completion=%+v err=%v", completion, err)
	}

	for _, test := range []struct{ mode, format, code string }{
		{"badjson", "json", "local_tool_output_invalid"},
		{"badjsonl", "jsonl", "local_tool_output_invalid"},
	} {
		tool := helperTool(test.mode, test.format)
		prepared := prepare(t, tool, registryPath, nil)
		_, err := prepared.Adapter.Run(context.Background(), requestFor(tool, nil), nil)
		assertFailureCode(t, err, test.code)
	}

	missing := helperTool("json", "json")
	missing.Actions[0].NativeRefs.Session = &core.LocalToolNativeSelector{Source: "stdout", Path: []string{"missing"}}
	prepared = prepare(t, missing, registryPath, nil)
	_, err = prepared.Adapter.Run(context.Background(), requestFor(missing, nil), nil)
	assertFailureCode(t, err, "local_tool_native_reference_missing")
}

func TestPreparedAdapterSelectsCompactFinalFromLastJSONLRecord(t *testing.T) {
	_, registryPath := executableBindingFixture(t, "local-tool.dummy", "1", "app.dummy")
	tool := helperTool("finaljsonl", "jsonl")
	tool.Actions[0].Output.Final = &core.LocalToolNativeSelector{Source: "stdout", Path: []string{"result"}}
	tool.Actions[0].NativeRefs.Session = &core.LocalToolNativeSelector{Source: "stdout", Path: []string{"session"}}
	prepared := prepare(t, tool, registryPath, nil)
	completion, err := prepared.Adapter.Run(context.Background(), requestFor(tool, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if completion.FinalText != "compact final" || completion.NativeSessionID == nil || *completion.NativeSessionID != "session-final" {
		t.Fatalf("completion=%+v", completion)
	}

	missing := tool
	missing.Actions[0].Output.Final = &core.LocalToolNativeSelector{Source: "stdout", Path: []string{"missing"}}
	prepared = prepare(t, missing, registryPath, nil)
	_, err = prepared.Adapter.Run(context.Background(), requestFor(missing, nil), nil)
	assertFailureCode(t, err, "local_tool_final_output_missing")
}

func TestVerifiedBindingRejectsPoisonedRegistryEntries(t *testing.T) {
	executable, registryPath := executableBindingFixture(t, "local-tool.dummy", "1", "app.dummy")
	digest := sha256File(t, executable)
	base := VerifiedBinding{
		BindingRef: "local-tool.dummy", ResourceID: "app.dummy", ContractVersion: "1", Executable: executable,
		MaterialDigest: digest, DeploymentID: "app.dummy@1:" + digest, DeclarationEventID: "decl-1", SelectionEventID: "select-1",
	}
	tests := []struct {
		name   string
		mutate func(*VerifiedBindings)
		code   string
	}{
		{"duplicate-binding", func(r *VerifiedBindings) {
			duplicate := r.Entries[0]
			duplicate.DeclarationEventID = "decl-2"
			duplicate.SelectionEventID = "select-2"
			r.Entries = append(r.Entries, duplicate)
		}, "binding_duplicate"},
		{"duplicate-provenance", func(r *VerifiedBindings) {
			duplicate := r.Entries[0]
			duplicate.BindingRef = "local-tool.other"
			r.Entries = append(r.Entries, duplicate)
		}, "binding_provenance_duplicate"},
		{"cross-kind-provenance", func(r *VerifiedBindings) { r.Entries[0].SelectionEventID = r.Entries[0].DeclarationEventID }, "binding_provenance_duplicate"},
		{"wrong-deployment", func(r *VerifiedBindings) { r.Entries[0].DeploymentID = "wrong" }, "binding_registry_invalid"},
		{"relative-path", func(r *VerifiedBindings) { r.Entries[0].Executable = "dummy.exe" }, "binding_registry_invalid"},
		{"wrong-digest", func(r *VerifiedBindings) {
			r.Entries[0].MaterialDigest = "sha256:" + strings.Repeat("0", 64)
			r.Entries[0].DeploymentID = "app.dummy@1:" + r.Entries[0].MaterialDigest
		}, "binding_digest_mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := VerifiedBindings{Schema: VerifiedBindingsSchema, Entries: []VerifiedBinding{base}}
			test.mutate(&registry)
			writeRegistry(t, registryPath, registry)
			_, err := LoadVerifiedBinding(registryPath, "local-tool.dummy", "1")
			assertFailureCode(t, err, test.code)
		})
	}

	nonRegular := t.TempDir()
	base.Executable = nonRegular
	writeRegistry(t, registryPath, VerifiedBindings{Schema: VerifiedBindingsSchema, Entries: []VerifiedBinding{base}})
	_, err := LoadVerifiedBinding(registryPath, "local-tool.dummy", "1")
	assertFailureCode(t, err, "binding_executable_not_regular")
}

func TestVerifiedBindingRejectsRegistryLargerThanFourMiB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.json")
	if err := os.WriteFile(path, bytes.Repeat([]byte{' '}, (4<<20)+1), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadVerifiedBinding(path, "local-tool.dummy", "1")
	assertFailureCode(t, err, "binding_registry_invalid")
}

func helperTool(mode, output string, inputs ...core.LocalToolInput) core.LocalToolDefinition {
	return core.LocalToolDefinition{
		Kind: "hq.local-tool.v1", ToolID: "dummy", ToolVersion: "1", BindingRef: "local-tool.dummy", BindingContractVersion: "1",
		Actions: []core.LocalToolAction{{
			ActionID: "proof", Inputs: inputs,
			Argv:  []core.LocalToolArg{{Literal: stringRef("-test.run=^TestLocalToolHelperProcess$")}, {Literal: stringRef("--")}, {Literal: stringRef(mode)}},
			Stdin: core.LocalToolStdin{Mode: "none"}, Limits: core.LocalToolLimits{TimeoutMS: 2000, StdoutBytes: 4096, StderrBytes: 4096},
			Output: core.LocalToolOutput{Format: output}, Lifecycle: "one-shot", Risk: "low", Approval: "explicit",
		}},
	}
}

func executableBindingFixture(t *testing.T, bindingRef, contractVersion, resourceID string) (string, string) {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), filepath.Base(source))
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256File(t, executable)
	registryPath := filepath.Join(t.TempDir(), "verified-executable-bindings.json")
	writeRegistry(t, registryPath, VerifiedBindings{Schema: VerifiedBindingsSchema, Entries: []VerifiedBinding{{
		BindingRef: bindingRef, ResourceID: resourceID, ContractVersion: contractVersion, Executable: executable,
		MaterialDigest: digest, DeploymentID: resourceID + "@1:" + digest, DeclarationEventID: "decl-1", SelectionEventID: "select-1",
	}}})
	return executable, registryPath
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func writeRegistry(t *testing.T, path string, registry VerifiedBindings) {
	t.Helper()
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func prepare(t *testing.T, tool core.LocalToolDefinition, registryPath string, input map[string]any) adapter.Prepared {
	t.Helper()
	prepared, err := (Preparer{World: &core.JsonlWorld{LocalTools: []core.LocalToolDefinition{tool}}, BindingsPath: registryPath}).Prepare(context.Background(), requestFor(tool, input))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Provider == nil || prepared.Provider.ProviderID != tool.BindingRef || prepared.Provider.ContractVersion != tool.BindingContractVersion || prepared.Provider.ProviderKind != "executable" {
		t.Fatalf("provider=%+v", prepared.Provider)
	}
	return prepared
}

func requestFor(tool core.LocalToolDefinition, input map[string]any) adapter.Request {
	if input == nil {
		input = map[string]any{}
	}
	payload, _ := json.Marshal(map[string]any{"tool_id": tool.ToolID, "tool_version": tool.ToolVersion, "action_id": tool.Actions[0].ActionID, "input": input})
	return adapter.Request{RunID: "run-1", InstructionID: "ins-1", Target: "local-tool", Operation: "run", Payload: payload, CWD: os.TempDir()}
}

func stringRef(value string) *string { return &value }

func assertFailureCode(t *testing.T, err error, code string) {
	t.Helper()
	var failure *adapter.FailureError
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("error=%v, want code %q", err, code)
	}
}

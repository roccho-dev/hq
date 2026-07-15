package localtool

import (
	"context"
	"encoding/json"
	"testing"

	"hq/internal/core"
	"hq/internal/worker/adapter"
)

func invocableTool() core.LocalToolDefinition {
	return core.LocalToolDefinition{
		Kind:                   "hq.local-tool.v1",
		ToolID:                 "aws",
		ToolVersion:            "2.35.11",
		BindingRef:             "local-tool.aws-restricted",
		BindingContractVersion: "1",
		Invocation: &core.LocalToolInvocation{
			PolicyVersion:          "aws-restricted.v1",
			DeniedOptions:          []string{"--profile", "--endpoint-url", "--ca-bundle", "--no-sign-request", "--cli-input-json", "--cli-input-yaml"},
			DeniedArgumentPrefixes: []string{"file://", "@"},
			MaxArgv:                32,
			MaxArgBytes:            4096,
			Limits:                 core.LocalToolLimits{TimeoutMS: 60000, StdoutBytes: 1 << 20, StderrBytes: 1 << 20},
		},
	}
}

func invocationRequest(tool core.LocalToolDefinition, argv []string) adapter.Request {
	payload, _ := json.Marshal(map[string]any{
		"tool_id": tool.ToolID, "tool_version": tool.ToolVersion,
		"policy_version": tool.Invocation.PolicyVersion, "argv": argv,
	})
	return adapter.Request{RunID: "run-1", InstructionID: "ins-1", Target: "local-tool", Operation: "run", Payload: payload}
}

func TestOneResourcePreparesDistinctAWSSubcommandsWithoutActions(t *testing.T) {
	tool := invocableTool()
	_, registryPath := executableBindingFixture(t, tool.BindingRef, tool.BindingContractVersion, "app.aws-restricted")
	preparer := Preparer{World: &core.JsonlWorld{LocalTools: []core.LocalToolDefinition{tool}}, BindingsPath: registryPath}
	var capability string
	for _, argv := range [][]string{
		{"sts", "get-caller-identity", "--output", "json", "--no-cli-pager"},
		{"apigateway", "get-rest-api", "--rest-api-id", "api-123", "--no-cli-pager"},
		{"iam", "list-roles", "--no-cli-pager"},
	} {
		prepared, err := preparer.Prepare(context.Background(), invocationRequest(tool, argv))
		if err != nil {
			t.Fatalf("argv=%q err=%v", argv, err)
		}
		if prepared.Provider == nil {
			t.Fatalf("argv=%q has no provider", argv)
		}
		if capability == "" {
			capability = prepared.Provider.CapabilityID
		} else if prepared.Provider.CapabilityID != capability {
			t.Fatalf("subcommand changed resource capability: first=%s current=%s", capability, prepared.Provider.CapabilityID)
		}
	}
	if len(tool.Actions) != 0 {
		t.Fatalf("proof resource unexpectedly has per-subcommand actions: %+v", tool.Actions)
	}
}

func TestResourceInvocationRunsLiteralArgvThroughExistingExecutor(t *testing.T) {
	argument := `;&|$()^%PATH%`
	tool := invocableTool()
	tool.ToolID = "helper"
	tool.BindingRef = "local-tool.helper"
	tool.Invocation.PolicyVersion = "helper.v1"
	_, registryPath := executableBindingFixture(t, tool.BindingRef, tool.BindingContractVersion, "app.helper")
	argv := []string{"-test.run=^TestLocalToolHelperProcess$", "--", "echo", argument}
	prepared, err := (Preparer{World: &core.JsonlWorld{LocalTools: []core.LocalToolDefinition{tool}}, BindingsPath: registryPath}).Prepare(context.Background(), invocationRequest(tool, argv))
	if err != nil {
		t.Fatal(err)
	}
	var stdout string
	completion, err := prepared.Adapter.Run(context.Background(), invocationRequest(tool, argv), func(output adapter.Output) error {
		if output.Kind == adapter.OutputStdout {
			stdout += output.Message
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stdout != argument || completion.FinalText != argument {
		t.Fatalf("argv was reparsed: stdout=%q completion=%q", stdout, completion.FinalText)
	}
}

func TestResourceInvocationRejectsPolicyEscapeBeforeProviderPreparation(t *testing.T) {
	tool := invocableTool()
	_, registryPath := executableBindingFixture(t, tool.BindingRef, tool.BindingContractVersion, "app.aws-restricted")
	preparer := Preparer{World: &core.JsonlWorld{LocalTools: []core.LocalToolDefinition{tool}}, BindingsPath: registryPath}
	for _, test := range []struct {
		name string
		argv []string
		code string
	}{
		{"profile", []string{"sts", "get-caller-identity", "--profile=admin"}, "resource_invocation_option_denied"},
		{"endpoint", []string{"sts", "get-caller-identity", "--endpoint-url", "https://example.invalid"}, "resource_invocation_option_denied"},
		{"file", []string{"sts", "get-caller-identity", "file://secret.json"}, "resource_invocation_argument_denied"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := preparer.Prepare(context.Background(), invocationRequest(tool, test.argv))
			assertFailureCode(t, err, test.code)
		})
	}

	request := invocationRequest(tool, []string{"sts", "get-caller-identity"})
	var raw map[string]any
	_ = json.Unmarshal(request.Payload, &raw)
	raw["policy_version"] = "stale.v1"
	request.Payload, _ = json.Marshal(raw)
	_, err := preparer.Prepare(context.Background(), request)
	assertFailureCode(t, err, "resource_invocation_policy_mismatch")
}

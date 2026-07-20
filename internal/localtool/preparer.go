package localtool

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"hq/internal/core"
	"hq/internal/localtoolpolicy"
	"hq/internal/worker/adapter"
	"hq/internal/worker/directexec"
)

type Preparer struct {
	World        *core.JsonlWorld
	BindingsPath string
}

type payload struct {
	ToolID        string                     `json:"tool_id"`
	ToolVersion   string                     `json:"tool_version"`
	ActionID      string                     `json:"action_id,omitempty"`
	Input         map[string]json.RawMessage `json:"input,omitempty"`
	PolicyVersion string                     `json:"policy_version,omitempty"`
	Argv          []string                   `json:"argv,omitempty"`
}

func (p Preparer) Prepare(_ context.Context, request adapter.Request) (adapter.Prepared, error) {
	if err := request.Validate(); err != nil {
		return adapter.Prepared{}, blocked("local_tool_request_invalid", err.Error())
	}
	if request.Target != "local-tool" || request.Operation != "run" {
		return adapter.Prepared{}, blocked("local_tool_request_invalid", "local-tool preparer accepts only canonical local-tool/run requests")
	}
	var value payload
	if err := decodeStrict(request.Payload, &value); err != nil {
		return adapter.Prepared{}, blocked("local_tool_payload_invalid", err.Error())
	}
	finite := value.ActionID != "" || value.Input != nil
	invocation := value.PolicyVersion != "" || value.Argv != nil
	if finite == invocation {
		return adapter.Prepared{}, blocked("local_tool_payload_invalid", "payload must be exactly one finite action or one verified-resource invocation")
	}
	if p.World == nil {
		return adapter.Prepared{}, blocked("local_tool_world_unavailable", "selected world is unavailable")
	}
	tool, ok := p.World.LocalTool(value.ToolID, value.ToolVersion)
	if !ok {
		return adapter.Prepared{}, blocked("local_tool_unknown", fmt.Sprintf("local tool %q version %q is unavailable", value.ToolID, value.ToolVersion))
	}

	var action core.LocalToolAction
	var plan executionPlan
	var dependencies []resolvedDependency
	var capabilityID string
	if finite {
		if value.ActionID == "" || value.Input == nil {
			return adapter.Prepared{}, blocked("local_tool_payload_invalid", "finite action requires action_id and input")
		}
		action, ok = actionByID(tool, value.ActionID)
		if !ok {
			return adapter.Prepared{}, blocked("local_tool_action_unknown", fmt.Sprintf("local tool action %q is unavailable", value.ActionID))
		}
		var dependencyPaths map[string]string
		var err error
		dependencies, dependencyPaths, err = resolveActionDependencies(tool, action, p.BindingsPath)
		if err != nil {
			return adapter.Prepared{}, err
		}
		plan, err = buildPlan(action, value.Input, dependencyPaths)
		if err != nil {
			return adapter.Prepared{}, err
		}
		capabilityID = fmt.Sprintf("local-tool:%s@%s/%s", tool.ToolID, tool.ToolVersion, action.ActionID)
	} else {
		if tool.Invocation == nil {
			return adapter.Prepared{}, blocked("resource_invocation_unavailable", fmt.Sprintf("local tool %q version %q does not permit resource invocation", value.ToolID, value.ToolVersion))
		}
		if err := localtoolpolicy.ValidateDefinition(*tool.Invocation); err != nil {
			return adapter.Prepared{}, blocked("resource_invocation_policy_invalid", err.Error())
		}
		if err := localtoolpolicy.ValidateInvocation(*tool.Invocation, value.PolicyVersion, value.Argv); err != nil {
			var failure *localtoolpolicy.Failure
			if errors.As(err, &failure) {
				return adapter.Prepared{}, blocked(failure.Code, failure.Message)
			}
			return adapter.Prepared{}, blocked("resource_invocation_policy_invalid", err.Error())
		}
		action = invocationAction(*tool.Invocation)
		plan = executionPlan{args: append([]string(nil), value.Argv...)}
		capabilityID = fmt.Sprintf("local-tool:%s@%s/invoke@%s", tool.ToolID, tool.ToolVersion, tool.Invocation.PolicyVersion)
	}

	binding, err := LoadVerifiedBinding(p.BindingsPath, tool.BindingRef, tool.BindingContractVersion)
	if err != nil {
		return adapter.Prepared{}, err
	}
	descriptor := adapter.ProviderDescriptor{
		CapabilityID: capabilityID, ProviderID: binding.BindingRef, ContractVersion: binding.ContractVersion,
		DeploymentID: binding.DeploymentID, ProviderKind: "executable", IntegrityDigest: binding.MaterialDigest,
		ConfigurationDigest: binding.ConfigurationDigest, Dependencies: dependencyDescriptors(dependencies),
	}
	if err := descriptor.Validate(); err != nil {
		return adapter.Prepared{}, blocked("local_tool_provider_invalid", err.Error())
	}
	return adapter.Prepared{
		Adapter:         &preparedAdapter{binding: binding, dependencies: dependencies, action: action, plan: plan},
		Provider:        &descriptor,
		RunViewRequired: action.RunView != nil && action.RunView.Policy == "required",
	}, nil
}

func invocationAction(invocation core.LocalToolInvocation) core.LocalToolAction {
	return core.LocalToolAction{
		ActionID:  "resource.invoke",
		Stdin:     core.LocalToolStdin{Mode: "none"},
		Limits:    invocation.Limits,
		Output:    core.LocalToolOutput{Format: "text"},
		Lifecycle: "one-shot",
		Risk:      "high",
		Approval:  "explicit",
	}
}

type resolvedDependency struct {
	name    string
	binding VerifiedBinding
}

func resolveActionDependencies(tool core.LocalToolDefinition, action core.LocalToolAction, bindingsPath string) ([]resolvedDependency, map[string]string, error) {
	referenced := map[string]bool{}
	for _, argument := range action.Argv {
		if argument.BindingExecutable != nil {
			referenced[*argument.BindingExecutable] = true
		}
	}
	if len(referenced) == 0 {
		return nil, map[string]string{}, nil
	}
	resolved := make([]resolvedDependency, 0, len(referenced))
	paths := make(map[string]string, len(referenced))
	for _, declaration := range tool.Bindings {
		if !referenced[declaration.Name] {
			continue
		}
		binding, err := LoadVerifiedBinding(bindingsPath, declaration.BindingRef, declaration.BindingContractVersion)
		if err != nil {
			return nil, nil, err
		}
		resolved = append(resolved, resolvedDependency{name: declaration.Name, binding: binding})
		paths[declaration.Name] = binding.Executable
		delete(referenced, declaration.Name)
	}
	for name := range referenced {
		return nil, nil, blocked("local_tool_dependency_unknown", fmt.Sprintf("binding executable dependency %q is not declared", name))
	}
	return resolved, paths, nil
}

func dependencyDescriptors(dependencies []resolvedDependency) []adapter.ProviderDependencyDescriptor {
	if len(dependencies) == 0 {
		return nil
	}
	result := make([]adapter.ProviderDependencyDescriptor, 0, len(dependencies))
	for _, dependency := range dependencies {
		binding := dependency.binding
		result = append(result, adapter.ProviderDependencyDescriptor{
			Name: dependency.name, ProviderID: binding.BindingRef, ContractVersion: binding.ContractVersion,
			DeploymentID: binding.DeploymentID, ProviderKind: "executable", IntegrityDigest: binding.MaterialDigest,
			ConfigurationDigest: binding.ConfigurationDigest,
		})
	}
	return result
}

type executionPlan struct {
	args  []string
	stdin []byte
}

func buildPlan(action core.LocalToolAction, input map[string]json.RawMessage, dependencyPaths map[string]string) (executionPlan, error) {
	if input == nil {
		return executionPlan{}, blocked("local_tool_input_invalid", "input must be an object")
	}
	fields := make(map[string]core.LocalToolInput, len(action.Inputs))
	values := make(map[string]string, len(action.Inputs))
	for _, definition := range action.Inputs {
		fields[definition.Name] = definition
		raw, present := input[definition.Name]
		if !present {
			if definition.Required {
				return executionPlan{}, blocked("local_tool_input_missing", fmt.Sprintf("required input %q is missing", definition.Name))
			}
			continue
		}
		value, err := validateInputValue(definition, raw)
		if err != nil {
			return executionPlan{}, blocked("local_tool_input_invalid", fmt.Sprintf("input %q: %v", definition.Name, err))
		}
		values[definition.Name] = value
	}
	for name := range input {
		if _, ok := fields[name]; !ok {
			return executionPlan{}, blocked("local_tool_input_unknown", fmt.Sprintf("input %q is not declared", name))
		}
	}
	args := make([]string, 0, len(action.Argv))
	for _, template := range action.Argv {
		if template.BindingExecutable != nil {
			path, ok := dependencyPaths[*template.BindingExecutable]
			if !ok {
				return executionPlan{}, blocked("local_tool_dependency_unavailable", fmt.Sprintf("binding executable dependency %q is unavailable", *template.BindingExecutable))
			}
			args = append(args, path)
			continue
		}
		if template.Literal != nil {
			args = append(args, *template.Literal)
			continue
		}
		value, ok := values[*template.Field]
		if !ok {
			return executionPlan{}, blocked("local_tool_input_missing", fmt.Sprintf("argv input %q is missing", *template.Field))
		}
		args = append(args, value)
	}
	var stdin []byte
	if action.Stdin.Mode == "field" {
		value, ok := values[action.Stdin.Field]
		if !ok {
			return executionPlan{}, blocked("local_tool_input_missing", fmt.Sprintf("stdin input %q is missing", action.Stdin.Field))
		}
		stdin = []byte(value)
		if len(stdin) > action.Stdin.MaxBytes {
			return executionPlan{}, blocked("stdin_limit_exceeded", fmt.Sprintf("stdin exceeds %d-byte limit", action.Stdin.MaxBytes))
		}
	}
	return executionPlan{args: args, stdin: stdin}, nil
}

func validateInputValue(definition core.LocalToolInput, raw json.RawMessage) (string, error) {
	switch definition.Type {
	case "string", "enum":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", errors.New("must be a string")
		}
		if definition.Type == "enum" {
			matched := false
			for _, allowed := range definition.Enum {
				matched = matched || value == allowed
			}
			if !matched {
				return "", errors.New("is not in the declared enum")
			}
		}
		return value, nil
	case "integer":
		var value int64
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", errors.New("must be an integer")
		}
		return strconv.FormatInt(value, 10), nil
	case "boolean":
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", errors.New("must be a boolean")
		}
		return strconv.FormatBool(value), nil
	default:
		return "", fmt.Errorf("unsupported input type %q", definition.Type)
	}
}

type preparedAdapter struct {
	binding      VerifiedBinding
	dependencies []resolvedDependency
	action       core.LocalToolAction
	plan         executionPlan
}

func (a *preparedAdapter) Run(ctx context.Context, request adapter.Request, emit adapter.Emit) (adapter.Completion, error) {
	if err := a.binding.VerifyExecutable(); err != nil {
		return adapter.Completion{}, err
	}
	for _, dependency := range a.dependencies {
		if err := dependency.binding.VerifyExecutable(); err != nil {
			return adapter.Completion{}, err
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(a.action.Limits.TimeoutMS)*time.Millisecond)
	defer cancel()
	result, runErr := (directexec.Runner{}).Run(runCtx, directexec.Command{
		Path: a.binding.Executable, Args: append([]string(nil), a.plan.args...), Dir: request.CWD,
		Stdin: append([]byte(nil), a.plan.stdin...), StdinLimit: int64(a.action.Stdin.MaxBytes),
		StdoutLimit: int64(a.action.Limits.StdoutBytes), StderrLimit: int64(a.action.Limits.StderrBytes), Env: a.binding.EnvironmentStrings(),
	})
	var output validatedOutput
	if runErr == nil {
		var err error
		output, err = validateOutput(a.action, result)
		if err != nil {
			return adapter.Completion{}, err
		}
	}
	if len(result.Stdout) != 0 && emit != nil {
		if err := emit(adapter.Output{Kind: adapter.OutputStdout, Message: string(result.Stdout), NativeSessionID: output.nativeSessionID}); err != nil {
			return adapter.Completion{}, err
		}
	}
	if len(result.Stderr) != 0 && emit != nil {
		if err := emit(adapter.Output{Kind: adapter.OutputStderr, Message: string(result.Stderr), NativeSessionID: output.nativeSessionID}); err != nil {
			return adapter.Completion{}, err
		}
	}
	if runErr != nil {
		if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(runErr, context.Canceled) {
			return adapter.Completion{}, runErr
		}
		var executionError *directexec.Error
		if errors.As(runErr, &executionError) {
			return adapter.Completion{}, failed(executionError.Code, executionError.Message)
		}
		return adapter.Completion{}, failed("local_tool_process_failed", "local tool process exited unsuccessfully")
	}
	finalText := output.finalText
	if finalText == "" {
		finalText = strings.TrimSpace(string(result.Stdout))
	}
	if finalText == "" {
		finalText = "local tool completed"
	}
	return adapter.Completion{FinalText: finalText, NativeSessionID: output.nativeSessionID}, nil
}

type validatedOutput struct {
	nativeSessionID *string
	finalText       string
}

func validateOutput(action core.LocalToolAction, result directexec.Result) (validatedOutput, error) {
	var stdoutValue any
	switch action.Output.Format {
	case "text":
	case "json":
		if err := decodeStrict(result.Stdout, &stdoutValue); err != nil {
			return validatedOutput{}, failed("local_tool_output_invalid", fmt.Sprintf("stdout is not one JSON value: %v", err))
		}
	case "jsonl":
		values, err := decodeJSONL(result.Stdout)
		if err != nil {
			return validatedOutput{}, failed("local_tool_output_invalid", err.Error())
		}
		if len(values) != 0 {
			stdoutValue = values[len(values)-1]
		}
	default:
		return validatedOutput{}, failed("local_tool_output_invalid", "unsupported output format")
	}
	output := validatedOutput{}
	if action.NativeRefs.Session != nil {
		value, err := selectOutputString(stdoutValue, *action.NativeRefs.Session, "native session", "local_tool_native_reference")
		if err != nil {
			return validatedOutput{}, err
		}
		output.nativeSessionID = &value
	}
	if action.Output.Final != nil {
		value, err := selectOutputString(stdoutValue, *action.Output.Final, "final output", "local_tool_final_output")
		if err != nil {
			return validatedOutput{}, err
		}
		output.finalText = value
	}
	return output, nil
}

func selectOutputString(source any, selector core.LocalToolNativeSelector, label, codePrefix string) (string, error) {
	if selector.Source != "stdout" {
		return "", failed(codePrefix+"_invalid", label+" selector requires structured stdout")
	}
	for _, segment := range selector.Path {
		object, ok := source.(map[string]any)
		if !ok {
			return "", failed(codePrefix+"_invalid", label+" selector traversed a non-object")
		}
		var present bool
		source, present = object[segment]
		if !present {
			return "", failed(codePrefix+"_missing", fmt.Sprintf("%s field %q is missing", label, segment))
		}
	}
	value, ok := source.(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", failed(codePrefix+"_invalid", label+" must be a non-empty string")
	}
	return value, nil
}

func decodeJSONL(data []byte) ([]any, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), len(data)+1)
	var values []any
	line := 0
	for scanner.Scan() {
		line++
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var value any
		if err := decodeStrict([]byte(scanner.Text()), &value); err != nil {
			return nil, fmt.Errorf("stdout JSONL line %d: %v", line, err)
		}
		values = append(values, value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, errors.New("stdout JSONL contains no values")
	}
	return values, nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func actionByID(tool core.LocalToolDefinition, actionID string) (core.LocalToolAction, bool) {
	for _, action := range tool.Actions {
		if action.ActionID == actionID {
			return action, true
		}
	}
	return core.LocalToolAction{}, false
}

func blocked(code, message string) error {
	return &adapter.FailureError{Class: adapter.FailureBlocked, Code: code, Message: message}
}

func failed(code, message string) error {
	return &adapter.FailureError{Class: adapter.FailureFailed, Code: code, Message: message}
}

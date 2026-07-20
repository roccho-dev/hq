package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	current "hq/internal/adapter/current"
	"hq/internal/core"
	"hq/internal/hqprofile"
	"hq/internal/localtool"
	"hq/internal/worker"
	workeradapter "hq/internal/worker/adapter"
)

const (
	runViewProjectionVersion = "hq.run-view.v1"
	defaultRunViewBytes      = 64 << 10
	maxRunViewBytes          = 1 << 20
)

type runViewPreparer interface {
	Prepare(context.Context, workeradapter.Request) (workeradapter.Prepared, error)
}

type runViewEnvironment struct {
	Profile      hqprofile.Profile
	World        *core.JsonlWorld
	Instructions []worker.Instruction
	Results      []worker.ResultRow
}

type runViewFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type runViewFinal struct {
	Text          string `json:"text,omitempty"`
	Path          string `json:"path,omitempty"`
	TextTruncated bool   `json:"text_truncated,omitempty"`
	PathTruncated bool   `json:"path_truncated,omitempty"`
}

type runViewRecord struct {
	Version       string          `json:"version"`
	RunID         string          `json:"run_id"`
	ViewID        string          `json:"view_id"`
	InstructionID string          `json:"instruction_id"`
	Lifecycle     string          `json:"lifecycle"`
	Policy        string          `json:"policy"`
	Available     bool            `json:"available"`
	Final         *runViewFinal   `json:"final,omitempty"`
	Failure       *runViewFailure `json:"failure,omitempty"`
}

type runViewContent struct {
	Lifecycle     string          `json:"lifecycle"`
	Text          string          `json:"text,omitempty"`
	TextTruncated bool            `json:"text_truncated,omitempty"`
	Final         *runViewFinal   `json:"final,omitempty"`
	Failure       *runViewFailure `json:"failure,omitempty"`
}

type runViewOperationReceipt struct {
	Version       string          `json:"version"`
	Operation     string          `json:"operation"`
	RunID         string          `json:"run_id"`
	ViewID        string          `json:"view_id"`
	InstructionID string          `json:"instruction_id"`
	Lifecycle     string          `json:"lifecycle"`
	Policy        string          `json:"policy"`
	Status        string          `json:"status"`
	ViewReference string          `json:"view_reference,omitempty"`
	Content       *runViewContent `json:"content,omitempty"`
	Failure       *runViewFailure `json:"failure,omitempty"`
}

type runViewEvent struct {
	Version    string          `json:"version"`
	RunID      string          `json:"run_id"`
	ViewID     string          `json:"view_id"`
	Seq        int             `json:"seq"`
	RecordedAt time.Time       `json:"recorded_at"`
	Lifecycle  string          `json:"lifecycle"`
	Final      *runViewFinal   `json:"final,omitempty"`
	Failure    *runViewFailure `json:"failure,omitempty"`
}

type runViewSelection struct {
	RunID           string
	ViewID          string
	Policy          string
	Instruction     worker.Instruction
	Detail          worker.RunDetail
	ViewTool        core.LocalToolDefinition
	OpenActionID    string
	NativeSessionID string
	ViewProvider    *worker.ProviderEvidence
	Failure         *runViewFailure
}

type runViewLocalToolPayload struct {
	ToolID      string                     `json:"tool_id"`
	ToolVersion string                     `json:"tool_version"`
	ActionID    string                     `json:"action_id"`
	Input       map[string]json.RawMessage `json:"input"`
}

func runRunView(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeCommandError(stderr, "invalid_arguments", "run-view requires one of list, open, focus, read, tail, or close")
		return 1
	}
	operation := args[0]
	switch operation {
	case "list", "open", "focus", "read", "tail", "close":
	default:
		writeCommandError(stderr, "invalid_arguments", fmt.Sprintf("unknown run-view operation %q", operation))
		return 1
	}

	flags := flag.NewFlagSet("hq-worker run-view "+operation, flag.ContinueOnError)
	flags.SetOutput(stderr)
	profileName := flags.String("profile", "", "selected installed profile name")
	profileRoot := flags.String("profile-root", "", "optional absolute installed profile root")
	runID := flags.String("run", "", "canonical run id")
	limit := flags.Int("limit", 20, "maximum recent runs for list; zero means all")
	maxBytes := flags.Int("bytes", defaultRunViewBytes, "maximum returned final/content bytes")
	follow := flags.Bool("follow", true, "tail until canonical terminal evidence")
	poll := flags.Duration("poll", 250*time.Millisecond, "bounded canonical-log rescan interval")
	timeout := flags.Duration("timeout", 0, "optional maximum operation duration")
	if err := flags.Parse(args[1:]); err != nil {
		return 1
	}
	if flags.NArg() != 0 || strings.TrimSpace(*profileName) == "" || *limit < 0 || *maxBytes <= 0 || *maxBytes > maxRunViewBytes || *poll <= 0 || *timeout < 0 {
		writeCommandError(stderr, "invalid_arguments", "run-view requires --profile, valid non-negative limits, positive --bytes/--poll, and non-negative --timeout")
		return 1
	}
	if operation != "list" && strings.TrimSpace(*runID) == "" {
		writeCommandError(stderr, "invalid_arguments", operation+" requires --run")
		return 1
	}

	environment, err := loadRunViewEnvironment(*profileName, *profileRoot)
	if err != nil {
		writeCommandError(stderr, "view_evidence_invalid", err.Error())
		return 2
	}
	preparer := localtool.Preparer{World: environment.World, BindingsPath: environment.Profile.ExecutableBindingsPath}

	if operation == "list" {
		return listRunViews(context.Background(), stdout, stderr, environment, preparer, *limit, *maxBytes)
	}
	selection, err := selectRunView(*runID, environment)
	if err != nil {
		writeCommandError(stderr, runViewSelectionErrorCode(err), err.Error())
		return 2
	}
	if selection.Failure != nil || selection.Policy == "none" {
		failure := selection.Failure
		if failure == nil {
			failure = &runViewFailure{Code: "view_unavailable", Message: "the canonical run has no selected view policy"}
		}
		return emitRunViewNonGreen(stdout, operation, selection, failure)
	}

	ctx := context.Background()
	cancel := func() {}
	if *timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, *timeout)
	}
	defer cancel()

	switch operation {
	case "read":
		return executeRunViewOperation(ctx, stdout, operation, preparer, selection, environment.Profile, *maxBytes)
	case "tail":
		if _, _, failure := prepareRunViewOperation(ctx, preparer, selection, environment.Profile, "open", *maxBytes, *follow); failure != nil {
			return emitRunViewNonGreen(stdout, operation, selection, failure)
		}
		return tailRunView(ctx, stdout, stderr, environment.Profile.EventsPath, selection, *follow, *poll, *maxBytes)
	case "open", "focus", "close":
		return executeRunViewOperation(ctx, stdout, operation, preparer, selection, environment.Profile, *maxBytes)
	default:
		panic("validated run-view operation was not handled")
	}
}

func runViewSelectionErrorCode(err error) string {
	var observationError *worker.ObservationError
	if errors.As(err, &observationError) && observationError.Code == "run_not_found" {
		return "run_not_found"
	}
	return "view_evidence_invalid"
}

func loadRunViewEnvironment(profileName, profileRoot string) (runViewEnvironment, error) {
	profile, err := hqprofile.Load(profileName, profileRoot)
	if err != nil {
		return runViewEnvironment{}, err
	}
	worldFile, err := os.Open(profile.WorldPath)
	if err != nil {
		return runViewEnvironment{}, err
	}
	defer worldFile.Close()
	world, err := current.LoadSchemaJSONL(worldFile)
	if err != nil {
		return runViewEnvironment{}, err
	}
	instructions, sourceDiagnostics, results, err := loadObservation(profile.AcceptedPath, inputAccepted, profile.EventsPath)
	if err != nil {
		return runViewEnvironment{}, err
	}
	if len(sourceDiagnostics) != 0 {
		return runViewEnvironment{}, fmt.Errorf("canonical accepted instructions contain %d diagnostic(s)", len(sourceDiagnostics))
	}
	return runViewEnvironment{Profile: profile, World: world, Instructions: instructions, Results: results}, nil
}

func listRunViews(ctx context.Context, stdout, stderr io.Writer, environment runViewEnvironment, preparer runViewPreparer, limit, maxBytes int) int {
	ledger, diagnostics := worker.BuildLedger(environment.Instructions, environment.Results)
	if len(diagnostics) != 0 {
		writeCommandError(stderr, "view_evidence_invalid", fmt.Sprintf("canonical run projection contains %d diagnostic(s)", len(diagnostics)))
		return 2
	}
	if limit > 0 && len(ledger) > limit {
		ledger = ledger[:limit]
	}
	for _, row := range ledger {
		selection, err := selectRunView(row.RunID, environment)
		if err != nil {
			writeCommandError(stderr, "view_evidence_invalid", err.Error())
			return 2
		}
		record := runViewRecord{
			Version: runViewProjectionVersion, RunID: selection.RunID, ViewID: selection.ViewID,
			InstructionID: selection.Instruction.ID, Lifecycle: selection.Detail.Run.Status, Policy: selection.Policy,
			Final: boundedFinal(selection.Detail.Final, maxBytes), Failure: selection.Failure,
		}
		if selection.Policy != "none" && selection.Failure == nil {
			_, _, failure := prepareRunViewOperation(ctx, preparer, selection, environment.Profile, "open", maxBytes, false)
			record.Failure = failure
			record.Available = failure == nil
		}
		if err := worker.EncodeJSONLine(stdout, record); err != nil {
			writeCommandError(stderr, "output_failed", err.Error())
			return 1
		}
	}
	return 0
}

func selectRunView(runID string, environment runViewEnvironment) (runViewSelection, error) {
	detail, diagnostics, err := worker.BuildRunDetail(runID, environment.Instructions, environment.Results)
	if err != nil {
		return runViewSelection{}, err
	}
	selection := runViewSelection{RunID: runID, ViewID: "hq-" + runID, Detail: detail, Policy: "none"}
	if len(diagnostics) != 0 {
		selection.Failure = &runViewFailure{Code: "view_evidence_invalid", Message: fmt.Sprintf("canonical run projection contains %d diagnostic(s)", len(diagnostics))}
	}
	instruction, ok := instructionByID(environment.Instructions, detail.Run.InstructionID)
	if !ok {
		return runViewSelection{}, fmt.Errorf("canonical instruction %q is unavailable", detail.Run.InstructionID)
	}
	selection.Instruction = instruction
	if viewEvidence := latestRunViewEvidence(detail.Events); viewEvidence != nil {
		selection.NativeSessionID = viewEvidence.NativeSessionID
		provider := viewEvidence.Provider
		selection.ViewProvider = &provider
	}
	if instruction.Target != "local-tool" || instruction.Op != "run" {
		return selection, nil
	}
	var payload runViewLocalToolPayload
	if err := json.Unmarshal(instruction.Payload, &payload); err != nil {
		selection.Failure = &runViewFailure{Code: "view_evidence_invalid", Message: "canonical local-tool payload is invalid"}
		return selection, nil
	}
	tool, ok := environment.World.LocalTool(payload.ToolID, payload.ToolVersion)
	if !ok {
		selection.Failure = &runViewFailure{Code: "view_world_mismatch", Message: "canonical run tool is absent from the selected world"}
		return selection, nil
	}
	action, ok := localToolActionByID(tool, payload.ActionID)
	if !ok {
		selection.Failure = &runViewFailure{Code: "view_world_mismatch", Message: "canonical run action is absent from the selected world"}
		return selection, nil
	}
	if action.RunView == nil {
		selection.Policy = instructionOptionalViewPolicy(instruction)
		if selection.Policy == "optional" {
			selection.Failure = &runViewFailure{Code: "view_unavailable", Message: "optional view policy has no selected provider plan"}
		}
		return selection, nil
	}
	selection.Policy = action.RunView.Policy
	if selection.Policy != "required" && selection.Policy != "optional" {
		selection.Failure = &runViewFailure{Code: "view_policy_invalid", Message: fmt.Sprintf("unsupported view policy %q", selection.Policy)}
		return selection, nil
	}
	viewTool, ok := environment.World.LocalTool(action.RunView.ToolID, action.RunView.ToolVersion)
	if !ok {
		selection.Failure = &runViewFailure{Code: "view_world_mismatch", Message: "selected view tool is absent from the selected world"}
		return selection, nil
	}
	if _, ok := localToolActionByID(viewTool, action.RunView.ActionID); !ok {
		selection.Failure = &runViewFailure{Code: "view_world_mismatch", Message: "selected view open action is absent from the selected world"}
		return selection, nil
	}
	selection.ViewTool = viewTool
	selection.OpenActionID = action.RunView.ActionID
	return selection, nil
}

func instructionOptionalViewPolicy(instruction worker.Instruction) string {
	if len(instruction.Policy) == 0 {
		return "none"
	}
	var policy struct {
		View string `json:"view"`
	}
	if err := json.Unmarshal(instruction.Policy, &policy); err == nil && policy.View == "optional" {
		return "optional"
	}
	return "none"
}

func instructionByID(instructions []worker.Instruction, instructionID string) (worker.Instruction, bool) {
	for _, instruction := range instructions {
		if instruction.ID == instructionID {
			return instruction, true
		}
	}
	return worker.Instruction{}, false
}

func localToolActionByID(tool core.LocalToolDefinition, actionID string) (core.LocalToolAction, bool) {
	for _, action := range tool.Actions {
		if action.ActionID == actionID {
			return action, true
		}
	}
	return core.LocalToolAction{}, false
}

func latestRunViewEvidence(events []worker.ResultRow) *worker.RunViewEvidence {
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].View != nil {
			view := *events[index].View
			return &view
		}
	}
	return nil
}

func runViewOperationActionID(openActionID, operation string) string {
	if operation == "open" {
		return openActionID
	}
	if openActionID == "open" {
		return operation
	}
	for _, separator := range []string{".", "-", "_", "/"} {
		suffix := separator + "open"
		if strings.HasSuffix(openActionID, suffix) {
			return strings.TrimSuffix(openActionID, suffix) + separator + operation
		}
	}
	return ""
}

func prepareRunViewOperation(ctx context.Context, preparer runViewPreparer, selection runViewSelection, profile hqprofile.Profile, operation string, maxBytes int, follow bool) (workeradapter.Request, workeradapter.Prepared, *runViewFailure) {
	if selection.Policy == "none" || selection.OpenActionID == "" {
		return workeradapter.Request{}, workeradapter.Prepared{}, &runViewFailure{Code: "view_unavailable", Message: "the selected run has no finite view provider plan"}
	}
	actionID := runViewOperationActionID(selection.OpenActionID, operation)
	if actionID == "" {
		return workeradapter.Request{}, workeradapter.Prepared{}, &runViewFailure{Code: "view_operation_unavailable", Message: fmt.Sprintf("the selected view plan does not define %s", operation)}
	}
	action, ok := localToolActionByID(selection.ViewTool, actionID)
	if !ok {
		return workeradapter.Request{}, workeradapter.Prepared{}, &runViewFailure{Code: "view_operation_unavailable", Message: fmt.Sprintf("the selected view plan does not define %s", operation)}
	}
	input, failure := buildRunViewOperationInput(action, selection, profile.EventsPath, maxBytes, follow)
	if failure != nil {
		return workeradapter.Request{}, workeradapter.Prepared{}, failure
	}
	payload, err := json.Marshal(runViewLocalToolPayload{
		ToolID: selection.ViewTool.ToolID, ToolVersion: selection.ViewTool.ToolVersion, ActionID: actionID, Input: input,
	})
	if err != nil {
		return workeradapter.Request{}, workeradapter.Prepared{}, &runViewFailure{Code: "view_contract_invalid", Message: err.Error()}
	}
	request := workeradapter.Request{
		RunID: selection.RunID, InstructionID: selection.Instruction.ID, Target: "local-tool", Operation: "run",
		Payload: payload, CWD: profile.WorkspaceRoot,
	}
	prepared, err := preparer.Prepare(ctx, request)
	if err != nil {
		return request, workeradapter.Prepared{}, &runViewFailure{Code: "view_unavailable", Message: err.Error()}
	}
	if prepared.Adapter == nil || prepared.Provider == nil {
		return request, workeradapter.Prepared{}, &runViewFailure{Code: "view_unavailable", Message: "verified view provider evidence is unavailable"}
	}
	if prepared.RunViewRequired {
		return request, workeradapter.Prepared{}, &runViewFailure{Code: "view_contract_invalid", Message: "view operations cannot recursively require another view"}
	}
	if _, usesNativeReference := input["native_session_id"]; usesNativeReference {
		if selection.ViewProvider == nil || !sameRunViewProvider(selection.ViewProvider, prepared.Provider) {
			return request, workeradapter.Prepared{}, &runViewFailure{
				Code: "view_reference_stale", Message: "the native view reference belongs to a different verified provider",
			}
		}
	}
	return request, prepared, nil
}

func sameRunViewProvider(canonical *worker.ProviderEvidence, current *workeradapter.ProviderDescriptor) bool {
	if canonical == nil || current == nil ||
		canonical.ProviderID != current.ProviderID ||
		canonical.ContractVersion != current.ContractVersion ||
		canonical.DeploymentID != current.DeploymentID ||
		canonical.ProviderKind != current.ProviderKind ||
		canonical.IntegrityDigest != current.IntegrityDigest ||
		canonical.ConfigurationDigest != current.ConfigurationDigest ||
		len(canonical.Dependencies) != len(current.Dependencies) {
		return false
	}
	for index := range canonical.Dependencies {
		left := canonical.Dependencies[index]
		right := current.Dependencies[index]
		if left.Name != right.Name ||
			left.ProviderID != right.ProviderID ||
			left.ContractVersion != right.ContractVersion ||
			left.DeploymentID != right.DeploymentID ||
			left.ProviderKind != right.ProviderKind ||
			left.IntegrityDigest != right.IntegrityDigest ||
			left.ConfigurationDigest != right.ConfigurationDigest {
			return false
		}
	}
	return true
}

func buildRunViewOperationInput(action core.LocalToolAction, selection runViewSelection, eventsPath string, maxBytes int, follow bool) (map[string]json.RawMessage, *runViewFailure) {
	values := map[string]any{
		"view_id": selection.ViewID, "run_id": selection.RunID, "events_path": eventsPath,
		"native_session_id": selection.NativeSessionID, "max_bytes": maxBytes, "follow": follow,
	}
	types := map[string]string{
		"view_id": "string", "run_id": "string", "events_path": "string", "native_session_id": "string",
		"max_bytes": "integer", "follow": "boolean",
	}
	input := make(map[string]json.RawMessage)
	for _, definition := range action.Inputs {
		expectedType, known := types[definition.Name]
		if !known {
			if definition.Required {
				return nil, &runViewFailure{Code: "view_contract_invalid", Message: fmt.Sprintf("view action requires unsupported input %q", definition.Name)}
			}
			continue
		}
		if definition.Type != expectedType {
			return nil, &runViewFailure{Code: "view_contract_invalid", Message: fmt.Sprintf("view action input %q must have type %s", definition.Name, expectedType)}
		}
		value := values[definition.Name]
		if definition.Name == "native_session_id" && strings.TrimSpace(selection.NativeSessionID) == "" {
			if definition.Required {
				return nil, &runViewFailure{Code: "view_reference_stale", Message: "the view action requires a current native view reference"}
			}
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, &runViewFailure{Code: "view_contract_invalid", Message: err.Error()}
		}
		input[definition.Name] = encoded
	}
	return input, nil
}

func executeRunViewOperation(ctx context.Context, stdout io.Writer, operation string, preparer runViewPreparer, selection runViewSelection, profile hqprofile.Profile, maxBytes int) int {
	request, prepared, failure := prepareRunViewOperation(ctx, preparer, selection, profile, operation, maxBytes, false)
	if failure != nil {
		return emitRunViewNonGreen(stdout, operation, selection, failure)
	}
	completion, err := prepared.Adapter.Run(ctx, request, nil)
	if err != nil {
		return emitRunViewNonGreen(stdout, operation, selection, &runViewFailure{Code: "view_provider_failed", Message: err.Error()})
	}
	viewReference := selection.NativeSessionID
	if completion.NativeSessionID != nil && strings.TrimSpace(*completion.NativeSessionID) != "" {
		viewReference = *completion.NativeSessionID
	}
	if operation == "open" && strings.TrimSpace(viewReference) == "" {
		return emitRunViewNonGreen(stdout, operation, selection, &runViewFailure{Code: "view_provider_failed", Message: "open returned no native view reference"})
	}
	receipt := runViewOperationReceipt{
		Version: runViewProjectionVersion, Operation: operation, RunID: selection.RunID, ViewID: selection.ViewID,
		InstructionID: selection.Instruction.ID, Lifecycle: selection.Detail.Run.Status, Policy: selection.Policy,
		Status: operation + "ed", ViewReference: viewReference,
	}
	if operation == "read" {
		content, truncated := boundedString(completion.FinalText, maxBytes)
		receipt.Status = "read"
		receipt.Content = &runViewContent{
			Lifecycle: selection.Detail.Run.Status, Text: content, TextTruncated: truncated,
			Final: boundedFinal(selection.Detail.Final, maxBytes),
		}
		if selection.Detail.Error != nil {
			receipt.Content.Failure = &runViewFailure{
				Code: selection.Detail.Error.Code, Message: boundedText(selection.Detail.Error.Message, maxBytes),
			}
		}
	}
	if operation == "focus" {
		receipt.Status = "focused"
	}
	if operation == "close" {
		receipt.Status = "closed"
		receipt.ViewReference = ""
	}
	if err := worker.EncodeJSONLine(stdout, receipt); err != nil {
		return 1
	}
	return 0
}

func tailRunView(ctx context.Context, stdout, stderr io.Writer, eventsPath string, selection runViewSelection, follow bool, poll time.Duration, maxBytes int) int {
	err := worker.FollowRun(ctx, eventsPath, selection.RunID, follow, poll, func(row worker.ResultRow) error {
		event, ok := projectedRunViewEvent(row, selection.ViewID, maxBytes)
		if !ok {
			return nil
		}
		return worker.EncodeJSONLine(stdout, event)
	})
	if err == nil {
		return 0
	}
	failure := &runViewFailure{Code: "view_tail_failed", Message: err.Error()}
	if errors.Is(err, context.DeadlineExceeded) {
		failure = &runViewFailure{Code: "view_timeout", Message: "tail reached its configured timeout before terminal evidence"}
	}
	if errors.Is(err, context.Canceled) {
		failure = &runViewFailure{Code: "view_cancelled", Message: "tail was cancelled"}
	}
	writeCommandError(stderr, failure.Code, failure.Message)
	return 2
}

func projectedRunViewEvent(row worker.ResultRow, viewID string, maxBytes int) (runViewEvent, bool) {
	event := runViewEvent{
		Version: runViewProjectionVersion, RunID: row.RunID, ViewID: viewID, Seq: row.Seq, RecordedAt: row.RecordedAt,
	}
	switch row.Kind {
	case worker.ResultAccepted:
		event.Lifecycle = worker.StatusQueued
	case worker.ResultStarted:
		event.Lifecycle = worker.StatusRunning
	case worker.ResultCompleted:
		event.Lifecycle = worker.StatusCompleted
		event.Final = boundedFinal(row.Final, maxBytes)
	case worker.ResultFailed, worker.ResultBlocked, worker.ResultTimeout, worker.ResultCancelled:
		event.Lifecycle = row.Kind
		if row.Error != nil {
			event.Failure = &runViewFailure{Code: row.Error.Code, Message: boundedText(row.Error.Message, maxBytes)}
		}
	default:
		return runViewEvent{}, false
	}
	return event, true
}

func emitRunViewNonGreen(stdout io.Writer, operation string, selection runViewSelection, failure *runViewFailure) int {
	receipt := runViewOperationReceipt{
		Version: runViewProjectionVersion, Operation: operation, RunID: selection.RunID, ViewID: selection.ViewID,
		InstructionID: selection.Instruction.ID, Lifecycle: selection.Detail.Run.Status, Policy: selection.Policy,
		Status: "non_green", Failure: failure,
	}
	if err := worker.EncodeJSONLine(stdout, receipt); err != nil {
		return 1
	}
	return 2
}

func boundedFinal(final *worker.FinalResult, maxBytes int) *runViewFinal {
	if final == nil {
		return nil
	}
	text, textTruncated := boundedString(final.Text, maxBytes)
	path, pathTruncated := boundedString(final.Path, maxBytes)
	return &runViewFinal{Text: text, Path: path, TextTruncated: textTruncated, PathTruncated: pathTruncated}
}

func boundedText(value string, maxBytes int) string {
	bounded, _ := boundedString(value, maxBytes)
	return bounded
}

func boundedString(value string, maxBytes int) (string, bool) {
	if len(value) <= maxBytes {
		return value, false
	}
	bounded := []byte(value[:maxBytes])
	for len(bounded) != 0 && !utf8.Valid(bounded) {
		bounded = bounded[:len(bounded)-1]
	}
	return string(bounded), true
}

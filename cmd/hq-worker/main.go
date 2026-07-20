package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"hq/internal/worker"
	"hq/internal/workeraccept"
	"hq/internal/workerclaim"
)

const (
	defaultInstructionPath = ".hq/queue/instructions.jsonl"
	defaultEventPath       = ".hq/events/events.jsonl"
	inputInstructionV1     = "instruction.v1"
	inputAccepted          = "accepted.instruction"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		switch args[0] {
		case "list":
			return runList(args[1:], stdout, stderr)
		case "show":
			return runShow(args[1:], stdout, stderr)
		case "tail":
			return runTail(args[1:], stdout, stderr)
		case "view":
			return runView(args[1:], stdout, stderr)
		}
	}
	return runWorker(args, stdout, stderr)
}

func runWorker(args []string, stdout, stderr io.Writer) (exitCode int) {
	flags := flag.NewFlagSet("hq-worker", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", "worker input JSONL path")
	inputFormat := flags.String("input-format", inputInstructionV1, "instruction.v1 or accepted.instruction")
	events := flags.String("events", "events.jsonl", "append-only worker evidence JSONL path")
	approvals := flags.String("approvals", "", "optional worker.approval.v1 JSONL path")
	workspace := flags.String("workspace", ".", "workspace root for bounded cwd policy and single-writer claim")
	workerID := flags.String("worker-id", "", "worker identity recorded in the project-local claim")
	dryRun := flags.Bool("dry-run", false, "emit machine-readable plans without writing events or starting adapters")
	replay := flags.Bool("replay", false, "explicitly allow a new run for an instruction id with prior run evidence")
	recoverClaim := flags.String("recover-claim", "", "explicitly recover this exact stale claim id and exit")
	recoverReason := flags.String("recover-reason", "", "operator reason recorded for explicit stale-claim recovery")
	staleAfter := flags.Duration("stale-after", 24*time.Hour, "minimum claim age before explicit recovery")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "invalid_arguments", "worker processing accepts flags only")
		return 1
	}
	if *recoverClaim != "" {
		receipt, err := workerclaim.Recover(*workspace, *recoverClaim, *recoverReason, time.Now(), *staleAfter)
		if err != nil {
			writeCommandError(stderr, "claim_recovery_failed", err.Error())
			return 2
		}
		if err := worker.EncodeJSONLine(stdout, receipt); err != nil {
			writeCommandError(stderr, "output_failed", err.Error())
			return 1
		}
		return 0
	}
	if *input == "" {
		writeCommandError(stderr, "invalid_arguments", "--input is required")
		return 1
	}
	if *staleAfter <= 0 {
		writeCommandError(stderr, "invalid_arguments", "--stale-after must be positive")
		return 1
	}

	var claim *workerclaim.Claim
	if !*dryRun {
		id := strings.TrimSpace(*workerID)
		if id == "" {
			id = defaultWorkerID()
		}
		acquired, err := workerclaim.Acquire(*workspace, id, time.Now())
		if err != nil {
			writeWorkerClaimError(stderr, err)
			return 2
		}
		claim = acquired
		defer func() {
			if claim == nil {
				return
			}
			if err := claim.Release(); err != nil {
				writeCommandError(stderr, "claim_release_failed", err.Error())
				exitCode = 1
			}
		}()
	}

	rows, err := readWorkerRows(*input, *inputFormat)
	if err != nil {
		writeCommandError(stderr, "input_failed", err.Error())
		return 1
	}
	engine := worker.NewEngine(*workspace)
	if *dryRun {
		blocked, err := engine.DryRun(rows, stdout)
		if err != nil {
			writeCommandError(stderr, "dry_run_failed", err.Error())
			return 1
		}
		if blocked != 0 {
			return 2
		}
		return 0
	}
	prior, err := worker.LoadEventFile(*events)
	if err != nil {
		writeCommandError(stderr, "evidence_invalid", err.Error())
		return 1
	}
	approvalStore, err := worker.LoadApprovalFile(*approvals)
	if err != nil {
		writeCommandError(stderr, "approval_invalid", err.Error())
		return 1
	}
	registry, err := newRuntimeRegistry()
	if err != nil {
		writeCommandError(stderr, "registry_invalid", err.Error())
		return 1
	}
	runner := worker.NewRunner(*workspace, registry, approvalStore)
	log := worker.NewEventLog(*events)
	emitted, unsuccessful, err := runner.Process(context.Background(), rows, prior, *replay, log)
	if err != nil {
		writeCommandError(stderr, "worker_failed", err.Error())
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	for _, entry := range emitted {
		var value any
		switch {
		case entry.Validation != nil:
			value = entry.Validation
		case entry.Result != nil:
			value = entry.Result
		case entry.Policy != nil:
			value = entry.Policy
		default:
			writeCommandError(stderr, "worker_failed", "empty emitted log entry")
			return 1
		}
		if err := encoder.Encode(value); err != nil {
			writeCommandError(stderr, "output_failed", err.Error())
			return 1
		}
	}
	if unsuccessful != 0 {
		return 2
	}
	return 0
}

func runList(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hq-worker list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", defaultInstructionPath, "worker input JSONL path")
	inputFormat := flags.String("input-format", inputInstructionV1, "instruction.v1 or accepted.instruction")
	events := flags.String("events", defaultEventPath, "canonical result.v1 event JSONL path")
	jsonOutput := flags.Bool("json", false, "emit one worker.ledger.v1 JSON object per line")
	limit := flags.Int("limit", 20, "maximum recent runs; zero means all")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || *limit < 0 {
		writeCommandError(stderr, "invalid_arguments", "list accepts flags only and --limit must be non-negative")
		return 1
	}
	instructions, sourceDiagnostics, results, err := loadObservation(*input, *inputFormat, *events)
	if err != nil {
		writeCommandError(stderr, "evidence_invalid", err.Error())
		return 2
	}
	ledger, projectionDiagnostics := worker.BuildLedger(instructions, results)
	diagnostics := append(sourceDiagnostics, projectionDiagnostics...)
	if *limit > 0 && len(ledger) > *limit {
		ledger = ledger[:*limit]
	}
	if *jsonOutput {
		for _, row := range ledger {
			if err := worker.EncodeJSONLine(stdout, row); err != nil {
				writeCommandError(stderr, "output_failed", err.Error())
				return 1
			}
		}
	} else {
		printLedgerText(stdout, ledger)
	}
	if len(diagnostics) != 0 {
		writeDiagnostics(stderr, diagnostics)
		return 2
	}
	return 0
}

func runShow(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hq-worker show", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", defaultInstructionPath, "worker input JSONL path")
	inputFormat := flags.String("input-format", inputInstructionV1, "instruction.v1 or accepted.instruction")
	events := flags.String("events", defaultEventPath, "canonical result.v1 event JSONL path")
	runID := flags.String("run", "", "run id to reconstruct")
	jsonOutput := flags.Bool("json", false, "emit worker.run-detail.v1 JSON")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || strings.TrimSpace(*runID) == "" {
		writeCommandError(stderr, "invalid_arguments", "show requires --run and accepts flags only")
		return 1
	}
	instructions, sourceDiagnostics, results, err := loadObservation(*input, *inputFormat, *events)
	if err != nil {
		writeCommandError(stderr, "evidence_invalid", err.Error())
		return 2
	}
	detail, projectionDiagnostics, err := worker.BuildRunDetail(*runID, instructions, results)
	if err != nil {
		writeObservationError(stderr, err)
		return 2
	}
	if *jsonOutput {
		if err := worker.EncodeJSONLine(stdout, detail); err != nil {
			writeCommandError(stderr, "output_failed", err.Error())
			return 1
		}
	} else {
		printRunText(stdout, detail)
	}
	diagnostics := append(sourceDiagnostics, projectionDiagnostics...)
	if len(diagnostics) != 0 {
		writeDiagnostics(stderr, diagnostics)
		return 2
	}
	return 0
}

func runTail(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hq-worker tail", flag.ContinueOnError)
	flags.SetOutput(stderr)
	events := flags.String("events", defaultEventPath, "canonical result.v1 event JSONL path")
	runID := flags.String("run", "", "run id to follow")
	jsonOutput := flags.Bool("json", false, "emit canonical result.v1 JSON rows")
	follow := flags.Bool("follow", true, "wait for newly appended rows until terminal state")
	poll := flags.Duration("poll", 250*time.Millisecond, "bounded durable-log rescan interval")
	timeout := flags.Duration("timeout", 0, "optional maximum follow duration")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || strings.TrimSpace(*runID) == "" || *poll <= 0 || *timeout < 0 {
		writeCommandError(stderr, "invalid_arguments", "tail requires --run, positive --poll, and non-negative --timeout")
		return 1
	}
	ctx := context.Background()
	cancel := func() {}
	if *timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, *timeout)
	}
	defer cancel()
	err := worker.FollowRun(ctx, *events, *runID, *follow, *poll, func(row worker.ResultRow) error {
		if *jsonOutput {
			return worker.EncodeJSONLine(stdout, row)
		}
		printEventText(stdout, row)
		return nil
	})
	if err == nil {
		return 0
	}
	if errors.Is(err, context.DeadlineExceeded) {
		writeCommandError(stderr, "tail_timeout", "tail reached its configured timeout before terminal state")
		return 2
	}
	if errors.Is(err, context.Canceled) {
		writeCommandError(stderr, "tail_cancelled", "tail was cancelled")
		return 2
	}
	writeObservationError(stderr, err)
	return 2
}

func runView(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hq-worker view", flag.ContinueOnError)
	flags.SetOutput(stderr)
	events := flags.String("events", defaultEventPath, "canonical result.v1 event JSONL path")
	runID := flags.String("run", "", "run id to reconstruct and follow")
	follow := flags.Bool("follow", true, "wait for newly appended rows until terminal state")
	hold := flags.Bool("hold", false, "retain the terminal view until the pane is explicitly closed")
	poll := flags.Duration("poll", 250*time.Millisecond, "bounded durable-log rescan interval")
	timeout := flags.Duration("timeout", 0, "optional maximum follow duration")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || strings.TrimSpace(*runID) == "" || *poll <= 0 || *timeout < 0 {
		writeCommandError(stderr, "invalid_arguments", "view requires --run, positive --poll, and non-negative --timeout")
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	cancel := func() {}
	if *timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, *timeout)
	}
	defer cancel()
	fmt.Fprintf(stdout, "HQ run %s\n\n", *runID)
	err := worker.FollowRun(ctx, *events, *runID, *follow, *poll, func(row worker.ResultRow) error {
		printViewEvent(stdout, row)
		return nil
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			writeCommandError(stderr, "view_timeout", "view reached its configured timeout before terminal state")
			return 2
		}
		if errors.Is(err, context.Canceled) {
			return 0
		}
		writeObservationError(stderr, err)
		return 2
	}
	if !*hold {
		return 0
	}
	fmt.Fprintln(stdout, "\nView retained. Close this pane to dismiss it.")
	<-ctx.Done()
	return 0
}

func readWorkerRows(inputPath, inputFormat string) ([]worker.ReadRow, error) {
	file, err := os.Open(inputPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	switch inputFormat {
	case inputInstructionV1:
		return worker.ReadInstructions(inputPath, file)
	case inputAccepted:
		return workeraccept.Read(inputPath, file)
	default:
		return nil, fmt.Errorf("unknown input format %q", inputFormat)
	}
}

func loadObservation(inputPath, inputFormat, eventPath string) ([]worker.Instruction, []worker.Diagnostic, []worker.ResultRow, error) {
	var instructions []worker.Instruction
	var diagnostics []worker.Diagnostic
	if inputFormat == inputInstructionV1 {
		loaded, sourceDiagnostics, err := worker.LoadInstructionsForObservation(inputPath)
		if err != nil {
			return nil, nil, nil, err
		}
		instructions = loaded
		diagnostics = sourceDiagnostics
	} else {
		rows, err := readWorkerRows(inputPath, inputFormat)
		if err != nil {
			return nil, nil, nil, err
		}
		validation := worker.DefaultContract().ValidateRows(rows)
		for index, row := range rows {
			if len(validation[index]) != 0 {
				diagnostics = append(diagnostics, validation[index]...)
				continue
			}
			instructions = append(instructions, row.Instruction)
		}
	}
	data, err := worker.LoadEventFile(eventPath)
	if err != nil {
		return nil, nil, nil, err
	}
	return instructions, diagnostics, data.Results, nil
}

func defaultWorkerID() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s:%d", hostname, os.Getpid())
}

func writeWorkerClaimError(w io.Writer, err error) {
	var conflict *workerclaim.ConflictError
	if errors.As(err, &conflict) {
		_ = worker.EncodeJSONLine(w, struct {
			Version    string                 `json:"version"`
			Code       string                 `json:"code"`
			Message    string                 `json:"message"`
			Inspection workerclaim.Inspection `json:"inspection"`
		}{Version: "worker.error.v1", Code: "workspace_claimed", Message: conflict.Error(), Inspection: conflict.Inspection})
		return
	}
	writeCommandError(w, "claim_failed", err.Error())
}

func printLedgerText(w io.Writer, ledger []worker.LedgerRow) {
	if len(ledger) == 0 {
		fmt.Fprintln(w, "no runs")
		return
	}
	fmt.Fprintln(w, "RUN_ID\tTARGET\tOP\tSTATUS\tLAST_EVENT\tNATIVE_SESSION\tSUMMARY")
	for _, row := range ledger {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row.RunID, row.Target, row.Op, row.Status, row.LastEventAt.Format(time.RFC3339Nano), row.NativeSessionID, row.Summary)
	}
}

func printRunText(w io.Writer, detail worker.RunDetail) {
	fmt.Fprintf(w, "run: %s\n", detail.Run.RunID)
	fmt.Fprintf(w, "instruction: %s\n", detail.Run.InstructionID)
	fmt.Fprintf(w, "target/op: %s/%s\n", detail.Run.Target, detail.Run.Op)
	fmt.Fprintf(w, "status: %s\n", detail.Run.Status)
	fmt.Fprintf(w, "cwd: %s\n", detail.Run.CWD)
	fmt.Fprintf(w, "started: %s\n", detail.Run.StartedAt.Format(time.RFC3339Nano))
	fmt.Fprintf(w, "last event: %s (%s)\n", detail.Run.LastEventAt.Format(time.RFC3339Nano), detail.Run.LastKind)
	fmt.Fprintf(w, "request: %s\n", detail.Instruction.Summary)
	if detail.Run.NativeSessionID != "" {
		fmt.Fprintf(w, "native session: %s\n", detail.Run.NativeSessionID)
	}
	if detail.Final != nil {
		if detail.Final.Text != "" {
			fmt.Fprintf(w, "final: %s\n", detail.Final.Text)
		}
		if detail.Final.Path != "" {
			fmt.Fprintf(w, "final path: %s\n", detail.Final.Path)
		}
	}
	if detail.Error != nil {
		fmt.Fprintf(w, "error: %s: %s\n", detail.Error.Code, detail.Error.Message)
	}
	if detail.AttachHint != "" {
		fmt.Fprintf(w, "hint: %s\n", detail.AttachHint)
	}
	fmt.Fprintln(w, "events:")
	for _, row := range detail.Events {
		printEventText(w, row)
	}
}

func printEventText(w io.Writer, row worker.ResultRow) {
	detail := ""
	switch {
	case row.Message != nil:
		detail = *row.Message
	case row.Final != nil && row.Final.Text != "":
		detail = row.Final.Text
	case row.Final != nil:
		detail = row.Final.Path
	case row.Error != nil:
		detail = row.Error.Code + ": " + row.Error.Message
	}
	detail = strings.Join(strings.Fields(detail), " ")
	fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", row.RecordedAt.Format(time.RFC3339Nano), row.Seq, row.Kind, detail)
}

func printViewEvent(w io.Writer, row worker.ResultRow) {
	at := row.RecordedAt.Format(time.RFC3339)
	switch row.Kind {
	case worker.ResultAccepted:
		fmt.Fprintf(w, "[%s] queued\n", at)
	case worker.ResultStarted:
		fmt.Fprintf(w, "[%s] running\n", at)
	case worker.ResultCompleted:
		fmt.Fprintf(w, "[%s] completed\n", at)
		if row.Final != nil && row.Final.Text != "" {
			fmt.Fprintf(w, "\n%s\n", row.Final.Text)
		}
		if row.Final != nil && row.Final.Path != "" {
			fmt.Fprintf(w, "\nartifact: %s\n", row.Final.Path)
		}
	case worker.ResultFailed, worker.ResultBlocked, worker.ResultTimeout, worker.ResultCancelled:
		fmt.Fprintf(w, "[%s] %s\n", at, row.Kind)
		if row.Error != nil {
			fmt.Fprintf(w, "\n%s: %s\n", row.Error.Code, row.Error.Message)
		}
	}
}

func writeObservationError(w io.Writer, err error) {
	var observationError *worker.ObservationError
	if errors.As(err, &observationError) {
		writeCommandError(w, observationError.Code, observationError.Message)
		return
	}
	writeCommandError(w, "observation_failed", err.Error())
}

func writeCommandError(w io.Writer, code, message string) {
	_ = worker.EncodeJSONLine(w, struct {
		Version string `json:"version"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Version: "worker.error.v1", Code: code, Message: message})
}

func writeDiagnostics(w io.Writer, diagnostics []worker.Diagnostic) {
	for _, diagnostic := range diagnostics {
		_ = worker.EncodeJSONLine(w, struct {
			Version    string            `json:"version"`
			Diagnostic worker.Diagnostic `json:"diagnostic"`
		}{Version: "worker.diagnostic.v1", Diagnostic: diagnostic})
	}
}

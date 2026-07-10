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

	"hq/internal/worker"
)

const (
	defaultInstructionPath = ".hq/queue/instructions.jsonl"
	defaultEventPath       = ".hq/events/events.jsonl"
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
		}
	}
	return runWorker(args, stdout, stderr)
}

func runWorker(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hq-worker", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", "instruction JSONL path")
	events := flags.String("events", "events.jsonl", "append-only worker evidence JSONL path")
	workspace := flags.String("workspace", ".", "workspace root for bounded cwd policy")
	dryRun := flags.Bool("dry-run", false, "emit machine-readable plans without writing events or starting adapters")
	replay := flags.Bool("replay", false, "explicitly allow a new run for an instruction id with prior run evidence")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if *input == "" {
		fmt.Fprintln(stderr, "error: --input is required")
		return 1
	}
	f, err := os.Open(*input)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	rows, err := worker.ReadInstructions(*input, f)
	_ = f.Close()
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	engine := worker.NewEngine(*workspace)
	if *dryRun {
		blocked, err := engine.DryRun(rows, stdout)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if blocked != 0 {
			return 2
		}
		return 0
	}
	prior, err := worker.LoadEventFile(*events)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	log := worker.NewEventLog(*events)
	blocked := 0
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	for _, entry := range engine.EvaluateNormal(rows, prior, *replay) {
		if err := log.Append(entry); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if entry.Validation != nil {
			blocked++
			if err := encoder.Encode(entry.Validation); err != nil {
				fmt.Fprintln(stderr, "error:", err)
				return 1
			}
		} else if entry.Result != nil {
			if entry.Result.Kind == worker.ResultBlocked {
				blocked++
			}
			if err := encoder.Encode(entry.Result); err != nil {
				fmt.Fprintln(stderr, "error:", err)
				return 1
			}
		}
	}
	if blocked != 0 {
		return 2
	}
	return 0
}

func runList(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hq-worker list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", defaultInstructionPath, "canonical instruction.v1 JSONL path")
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
	instructions, sourceDiagnostics, results, err := loadObservation(*input, *events)
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
	input := flags.String("input", defaultInstructionPath, "canonical instruction.v1 JSONL path")
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
	instructions, sourceDiagnostics, results, err := loadObservation(*input, *events)
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

func loadObservation(inputPath, eventPath string) ([]worker.Instruction, []worker.Diagnostic, []worker.ResultRow, error) {
	instructions, diagnostics, err := worker.LoadInstructionsForObservation(inputPath)
	if err != nil {
		return nil, nil, nil, err
	}
	data, err := worker.LoadEventFile(eventPath)
	if err != nil {
		return nil, nil, nil, err
	}
	return instructions, diagnostics, data.Results, nil
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

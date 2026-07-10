package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"hq/internal/worker"
)

func main() {
	var (
		input     = flag.String("input", "", "instruction JSONL path")
		events    = flag.String("events", "events.jsonl", "append-only worker evidence JSONL path")
		workspace = flag.String("workspace", ".", "workspace root for bounded cwd policy")
		dryRun    = flag.Bool("dry-run", false, "emit machine-readable plans without writing events or starting adapters")
		replay    = flag.Bool("replay", false, "explicitly allow a new run for an instruction id with prior run evidence")
	)
	flag.Parse()
	if *input == "" {
		fatal("--input is required")
	}
	f, err := os.Open(*input)
	if err != nil {
		fatal(err.Error())
	}
	rows, err := worker.ReadInstructions(*input, f)
	_ = f.Close()
	if err != nil {
		fatal(err.Error())
	}
	engine := worker.NewEngine(*workspace)
	if *dryRun {
		blocked, err := engine.DryRun(rows, os.Stdout)
		if err != nil {
			fatal(err.Error())
		}
		if blocked != 0 {
			os.Exit(2)
		}
		return
	}
	prior, err := worker.LoadEventFile(*events)
	if err != nil {
		fatal(err.Error())
	}
	log := worker.NewEventLog(*events)
	blocked := 0
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	for _, entry := range engine.EvaluateNormal(rows, prior, *replay) {
		if err := log.Append(entry); err != nil {
			fatal(err.Error())
		}
		if entry.Validation != nil {
			blocked++
			if err := enc.Encode(entry.Validation); err != nil {
				fatal(err.Error())
			}
		} else if entry.Result != nil {
			if entry.Result.Kind == worker.ResultBlocked {
				blocked++
			}
			if err := enc.Encode(entry.Result); err != nil {
				fatal(err.Error())
			}
		}
	}
	if blocked != 0 {
		os.Exit(2)
	}
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "error:", message)
	os.Exit(1)
}

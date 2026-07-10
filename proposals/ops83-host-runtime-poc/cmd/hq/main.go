package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/roccho-dev/ops/packages/hq/internal/hostopen"
	"github.com/roccho-dev/ops/packages/hq/internal/lsp"
	"github.com/roccho-dev/ops/packages/hq/internal/profile"
	"github.com/roccho-dev/ops/packages/hq/internal/runner"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "lsp":
		p := mustProfile("lsp", os.Args[2:])
		if err := lsp.New(p).Serve(os.Stdin, os.Stdout); err != nil {
			fail("lsp_failed", err)
		}
	case "run":
		p, once := mustRunProfile(os.Args[2:])
		if !once {
			fail("invalid_arguments", fmt.Errorf("hq run currently requires --once"))
		}
		result, err := runner.RunOnce(p, hostopen.New())
		if err != nil {
			fail("run_failed", err)
		}
		_ = json.NewEncoder(os.Stdout).Encode(result)
	case "doctor":
		p := mustProfile("doctor", os.Args[2:])
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"kind": "hq.doctor.v1", "ok": true, "profile": p.Name,
			"catalog": p.CatalogPath, "queue": p.QueuePath, "receipts": p.ReceiptPath,
		})
	default:
		usage()
		os.Exit(2)
	}
}

func mustProfile(command string, args []string) *profile.Profile {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	name := flags.String("profile", "", "hq profile name")
	if err := flags.Parse(args); err != nil || *name == "" || flags.NArg() != 0 {
		fail("invalid_arguments", fmt.Errorf("usage: hq %s --profile <name>", command))
	}
	p, err := profile.Load(os.Getenv("HQ_LOCAL_ROOT"), *name)
	if err != nil {
		fail("profile_invalid", err)
	}
	return p
}

func mustRunProfile(args []string) (*profile.Profile, bool) {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	name := flags.String("profile", "", "hq profile name")
	once := flags.Bool("once", false, "process one unreceipted command")
	if err := flags.Parse(args); err != nil || *name == "" || flags.NArg() != 0 {
		fail("invalid_arguments", fmt.Errorf("usage: hq run --profile <name> --once"))
	}
	p, err := profile.Load(os.Getenv("HQ_LOCAL_ROOT"), *name)
	if err != nil {
		fail("profile_invalid", err)
	}
	return p, *once
}

func fail(code string, err error) {
	_ = json.NewEncoder(os.Stderr).Encode(map[string]any{
		"kind": "hq.error.v1", "code": code, "message": err.Error(),
	})
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: hq lsp --profile <name>")
	fmt.Fprintln(os.Stderr, "       hq run --profile <name> --once")
	fmt.Fprintln(os.Stderr, "       hq doctor --profile <name>")
}

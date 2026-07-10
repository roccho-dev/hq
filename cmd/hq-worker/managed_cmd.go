package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"hq/internal/hqprofile"
	"hq/internal/worker"
	"hq/internal/workerclaim"
	"hq/internal/workerservice"
)

func init() {
	if len(os.Args) < 2 {
		return
	}
	var code int
	switch os.Args[1] {
	case "serve":
		code = runManagedServe(os.Args[2:])
	case "health":
		code = runManagedHealth(os.Args[2:])
	case "recover":
		code = runManagedRecover(os.Args[2:])
	default:
		return
	}
	os.Exit(code)
}

func managedProfileFlags(name string, args []string) (hqprofile.Profile, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profileName := flags.String("profile", "", "installed hq profile name")
	profileRoot := flags.String("profile-root", "", "absolute profile root override for tests/diagnostics")
	if err := flags.Parse(args); err != nil {
		return hqprofile.Profile{}, err
	}
	if flags.NArg() != 0 || *profileName == "" {
		return hqprofile.Profile{}, fmt.Errorf("--profile is required and positional arguments are not accepted")
	}
	return hqprofile.Load(*profileName, *profileRoot)
}

func runManagedServe(args []string) int {
	flags := flag.NewFlagSet("hq-worker serve", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profileName := flags.String("profile", "", "installed hq profile name")
	profileRoot := flags.String("profile-root", "", "absolute profile root override for tests/diagnostics")
	workerID := flags.String("worker-id", "", "managed worker identity")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *profileName == "" {
		writeCommandError(os.Stderr, "invalid_arguments", "usage: hq-worker serve --profile <name>")
		return 2
	}
	profile, err := hqprofile.Load(*profileName, *profileRoot)
	if err != nil {
		writeCommandError(os.Stderr, "profile_invalid", err.Error())
		return 2
	}
	id := *workerID
	if id == "" {
		id = defaultWorkerID()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := workerservice.Serve(ctx, profile, id, os.Stdout); err != nil {
		writeWorkerClaimError(os.Stderr, err)
		return 2
	}
	return 0
}

func runManagedHealth(args []string) int {
	profile, err := managedProfileFlags("hq-worker health", args)
	if err != nil {
		writeCommandError(os.Stderr, "profile_invalid", err.Error())
		return 2
	}
	report := workerservice.HealthCheck(profile, time.Now())
	if err := worker.EncodeJSONLine(os.Stdout, report); err != nil {
		writeCommandError(os.Stderr, "output_failed", err.Error())
		return 1
	}
	if !report.Ready {
		return 2
	}
	return 0
}

func runManagedRecover(args []string) int {
	flags := flag.NewFlagSet("hq-worker recover", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profileName := flags.String("profile", "", "installed hq profile name")
	profileRoot := flags.String("profile-root", "", "absolute profile root override for tests/diagnostics")
	claimID := flags.String("claim", "", "exact stale claim id")
	reason := flags.String("reason", "", "operator reconciliation reason")
	staleAfter := flags.Duration("stale-after", 30*time.Second, "minimum heartbeat/claim age")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *profileName == "" || *claimID == "" || *reason == "" {
		writeCommandError(os.Stderr, "invalid_arguments", "profile, claim, and reason are required")
		return 2
	}
	profile, err := hqprofile.Load(*profileName, *profileRoot)
	if err != nil {
		writeCommandError(os.Stderr, "profile_invalid", err.Error())
		return 2
	}
	receipt, err := workerclaim.RecoverManaged(profile.WorkspaceRoot, *claimID, *reason, time.Now(), *staleAfter)
	if err != nil {
		writeCommandError(os.Stderr, "claim_recovery_failed", err.Error())
		return 2
	}
	if err := worker.EncodeJSONLine(os.Stdout, receipt); err != nil {
		writeCommandError(os.Stderr, "output_failed", err.Error())
		return 1
	}
	return 0
}

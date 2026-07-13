package main

import (
	"context"
	"errors"
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
	case "stop":
		code = runManagedStop(os.Args[2:])
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
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stopSignals()
	ctx, stopControl, observations := workerservice.WithStopControl(signalCtx, profile)
	defer stopControl()
	serveErr := workerservice.Serve(ctx, profile, id, os.Stdout)
	var observation workerservice.StopObservation
	select {
	case value, ok := <-observations:
		if ok {
			observation = value
		}
	default:
	}
	if serveErr != nil {
		writeWorkerClaimError(os.Stderr, serveErr)
		return 2
	}
	if observation.Err != nil {
		writeCommandError(os.Stderr, "stop_control_invalid", observation.Err.Error())
		return 2
	}
	if observation.Request != nil {
		receipt, err := workerservice.AcknowledgeStop(profile, *observation.Request, time.Now())
		if err != nil {
			writeCommandError(os.Stderr, "stop_acknowledgement_failed", err.Error())
			return 2
		}
		if err := worker.EncodeJSONLine(os.Stdout, receipt); err != nil {
			writeCommandError(os.Stderr, "output_failed", err.Error())
			return 1
		}
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

func runManagedStop(args []string) int {
	flags := flag.NewFlagSet("hq-worker stop", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profileName := flags.String("profile", "", "installed hq profile name")
	profileRoot := flags.String("profile-root", "", "absolute profile root override for tests/diagnostics")
	timeout := flags.Duration("timeout", 30*time.Second, "maximum time to wait for worker acknowledgement and claim release")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *profileName == "" || *timeout <= 0 {
		writeCommandError(os.Stderr, "invalid_arguments", "usage: hq-worker stop --profile <name> [--timeout <duration>]")
		return 2
	}
	profile, err := hqprofile.Load(*profileName, *profileRoot)
	if err != nil {
		writeCommandError(os.Stderr, "profile_invalid", err.Error())
		return 2
	}
	request, err := workerservice.RequestStop(profile, time.Now())
	if err != nil {
		writeCommandError(os.Stderr, "stop_request_failed", err.Error())
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	receipt, err := workerservice.WaitStopped(ctx, profile, request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			writeCommandError(os.Stderr, "stop_timeout", err.Error())
		} else {
			writeCommandError(os.Stderr, "stop_failed", err.Error())
		}
		return 2
	}
	if err := worker.EncodeJSONLine(os.Stdout, receipt); err != nil {
		writeCommandError(os.Stderr, "output_failed", err.Error())
		return 1
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

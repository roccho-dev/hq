package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"hq/internal/hqprofile"
	"hq/internal/worker"
	"hq/internal/workeraccept"
)

func runApprove(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hq approve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	profileName := flags.String("profile", "", "installed hq profile name")
	profileRoot := flags.String("profile-root", "", "absolute profile root override for tests/diagnostics")
	instructionID := flags.String("instruction", "", "exact accepted instruction id")
	approvedBy := flags.String("approved-by", "", "operator identity recorded in worker.approval.v1")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || strings.TrimSpace(*profileName) == "" || strings.TrimSpace(*instructionID) == "" || strings.TrimSpace(*approvedBy) == "" {
		fmt.Fprintln(stderr, "usage: hq approve --profile <name> --instruction <id> --approved-by <identity> [--profile-root <absolute-path>]")
		return 2
	}
	profile, err := hqprofile.Load(*profileName, *profileRoot)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	file, err := os.Open(profile.AcceptedPath)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	rows, readErr := workeraccept.Read(profile.AcceptedPath, file)
	closeErr := file.Close()
	if readErr != nil {
		fmt.Fprintln(stderr, "error:", readErr)
		return 1
	}
	if closeErr != nil {
		fmt.Fprintln(stderr, "error:", closeErr)
		return 1
	}
	var selected *worker.ReadRow
	for index := range rows {
		row := &rows[index]
		if row.ParseError != nil || row.Instruction.ID != *instructionID {
			continue
		}
		if selected != nil {
			fmt.Fprintf(stderr, "error: accepted instruction id %q is duplicated\n", *instructionID)
			return 1
		}
		selected = row
	}
	if selected == nil {
		fmt.Fprintf(stderr, "error: accepted instruction id %q was not found\n", *instructionID)
		return 1
	}
	if diagnostics := worker.DefaultContract().Validate(*selected); len(diagnostics) != 0 {
		fmt.Fprintf(stderr, "error: accepted instruction is invalid: %s: %s\n", diagnostics[0].Code, diagnostics[0].Message)
		return 1
	}
	if !worker.IsVerifiedResourceInvocation(selected.Instruction) {
		fmt.Fprintln(stderr, "error: hq approve accepts only verified-resource invocations; finite operations retain submit-derived approval")
		return 1
	}
	digest, err := worker.InstructionDigest(selected.Instruction)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	record := worker.ApprovalRecord{
		Version: worker.ApprovalVersionV1, InstructionID: selected.Instruction.ID,
		Approved: true, ApprovedBy: strings.TrimSpace(*approvedBy), InstructionDigest: digest,
	}
	if _, err := worker.AppendWorkspaceApproval(profile.WorkspaceRoot, record); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if err := worker.EncodeJSONLine(stdout, record); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

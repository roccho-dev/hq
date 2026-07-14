package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"hq/internal/core"
	"hq/internal/hqprofile"
	"hq/internal/selectedworld"
)

const (
	selectedWorldInspectionKind    = "hq.selectedWorldInspection.v1"
	profileSelectionInspectionKind = "hq.profileSelectionInspection.v1"
)

type selectedWorldInspection struct {
	Kind  string        `json:"kind"`
	World core.WorldRef `json:"world"`
}

type profileSelectionInspection struct {
	Kind         string        `json:"kind"`
	Profile      string        `json:"profile"`
	DeploymentID string        `json:"deployment_id"`
	WorldPath    string        `json:"world_path"`
	World        core.WorldRef `json:"world"`
}

func runWorldInspect(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hq world inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("path", "", "absolute selected-world JSONL path")
	jsonOutput := flags.Bool("json", false, "emit one versioned JSON record")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *path == "" || !*jsonOutput {
		fmt.Fprintln(stderr, "usage: hq world inspect --path <absolute-world-jsonl> --json")
		return 2
	}
	selection, err := selectedworld.Load(*path)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if err := encodeInspection(stdout, selectedWorldInspection{Kind: selectedWorldInspectionKind, World: selection.Ref}); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func runProfileInspect(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hq profile inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	name := flags.String("profile", "", "installed hq profile name")
	root := flags.String("profile-root", "", "absolute profile root")
	jsonOutput := flags.Bool("json", false, "emit one versioned JSON record")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *name == "" || *root == "" || !*jsonOutput {
		fmt.Fprintln(stderr, "usage: hq profile inspect --profile <name> --profile-root <absolute-root> --json")
		return 2
	}
	profile, err := hqprofile.Load(*name, *root)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	selection, err := selectedworld.Load(profile.WorldPath)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	report := profileSelectionInspection{
		Kind: profileSelectionInspectionKind, Profile: profile.Name,
		DeploymentID: profile.DeploymentID, WorldPath: profile.WorldPath, World: selection.Ref,
	}
	if err := encodeInspection(stdout, report); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func encodeInspection(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

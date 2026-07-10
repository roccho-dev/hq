package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"hq/internal/hqlsp"
	"hq/internal/hqprofile"
)

func runSubcommand(args []string, stdin io.Reader, stdout, stderr io.Writer) (bool, int) {
	if len(args) == 0 || args[0] != "lsp" { return false, 0 }
	flags := flag.NewFlagSet("hq lsp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	profileName := flags.String("profile", "", "installed hq profile name")
	profileRoot := flags.String("profile-root", "", "absolute profile root override for tests/diagnostics")
	if err := flags.Parse(args[1:]); err != nil { return true, 2 }
	if flags.NArg() != 0 || *profileName == "" {
		fmt.Fprintln(stderr, "usage: hq lsp --profile <name> [--profile-root <absolute-path>]")
		return true, 2
	}
	profile, err := hqprofile.Load(*profileName, *profileRoot)
	if err != nil { fmt.Fprintln(stderr, "error:", err); return true, 1 }
	server, err := hqlsp.New(profile)
	if err != nil { fmt.Fprintln(stderr, "error:", err); return true, 1 }
	if err := server.Serve(stdin, stdout); err != nil { fmt.Fprintln(stderr, "error:", err); return true, 1 }
	return true, 0
}

func maybeRunSubcommand() {
	matched, code := runSubcommand(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	if !matched { return }
	os.Exit(code)
}

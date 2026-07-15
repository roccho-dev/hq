package main

import (
	"flag"
	"fmt"
	"hq/internal/hqlsp"
	"hq/internal/hqprofile"
	"io"
	"os"
)

func init() {
	matched, code := runSubcommand(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	if matched {
		os.Exit(code)
	}
}
func runSubcommand(args []string, stdin io.Reader, stdout, stderr io.Writer) (bool, int) {
	if len(args) >= 2 && args[0] == "world" && args[1] == "inspect" {
		return true, runWorldInspect(args[2:], stdout, stderr)
	}
	if len(args) >= 2 && args[0] == "profile" && args[1] == "inspect" {
		return true, runProfileInspect(args[2:], stdout, stderr)
	}
	if len(args) >= 1 && args[0] == "approve" {
		return true, runApprove(args[1:], stdout, stderr)
	}
	if len(args) == 0 || args[0] != "lsp" {
		return false, 0
	}
	flags := flag.NewFlagSet("hq lsp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	name := flags.String("profile", "", "installed hq profile name")
	root := flags.String("profile-root", "", "absolute profile root override for tests/diagnostics")
	if err := flags.Parse(args[1:]); err != nil {
		return true, 2
	}
	if flags.NArg() != 0 || *name == "" {
		fmt.Fprintln(stderr, "usage: hq lsp --profile <name> [--profile-root <absolute-path>]")
		return true, 2
	}
	p, err := hqprofile.Load(*name, *root)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return true, 1
	}
	s, err := hqlsp.New(p)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return true, 1
	}
	if err := s.Serve(stdin, stdout); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return true, 1
	}
	return true, 0
}

package main

import (
	"os"
	"path/filepath"
	"strings"
)

// The canonical sh adapter deliberately starts with an empty environment. The
// deterministic lane-L proof binary therefore derives its own adjacent artifact
// log path when invoked under its proof-sh filename. Version probes remain
// side-effect free. This is fixture-only and adds no product logging behavior.
func init() {
	if strings.TrimSpace(os.Getenv("HQ_PROOF_PROVIDER_LOG")) != "" || providerName(os.Args[0]) != "sh" || isVersionProbe(os.Args[1:]) {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		return
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "provider-invocations.jsonl"))
	_ = os.Setenv("HQ_PROOF_PROVIDER_LOG", path)
}

func isVersionProbe(args []string) bool {
	return len(args) > 0 && (args[0] == "--version" || args[0] == "version")
}

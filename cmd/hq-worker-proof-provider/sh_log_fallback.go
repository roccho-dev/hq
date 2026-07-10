package main

import (
	"os"
	"path/filepath"
	"strings"
)

// The canonical sh adapter deliberately starts with an empty environment. The
// deterministic lane-L proof binary therefore derives its own adjacent artifact
// log path when invoked under its proof-sh filename. This is fixture-only and
// does not add environment or logging behavior to product execution.
func init() {
	if strings.TrimSpace(os.Getenv("HQ_PROOF_PROVIDER_LOG")) != "" || providerName(os.Args[0]) != "sh" {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		return
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "provider-invocations.jsonl"))
	_ = os.Setenv("HQ_PROOF_PROVIDER_LOG", path)
}

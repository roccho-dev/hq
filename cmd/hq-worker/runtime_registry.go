package main

import (
	"os"
	"strings"

	"hq/internal/worker/adapter"
	"hq/internal/worker/agentadapter"
)

func newRuntimeRegistry() (*adapter.Registry, error) {
	providerRunner := agentadapter.OSRunner{}
	registrations := []adapter.Registration{{Target: "sh", Adapter: agentadapter.Sh{Runner: agentadapter.DirectOSRunner{}}}}
	registrations = append(registrations, agentadapter.Registrations(agentadapter.Config{
		Runner:     providerRunner,
		HerdrPath:  executablePath("HQ_HERDR_PATH", "herdr"),
		CodexPath:  executablePath("HQ_CODEX_PATH", "codex"),
		ClaudePath: executablePath("HQ_CLAUDE_PATH", "claude"),
	})...)
	return adapter.NewRegistry(registrations...)
}

func executablePath(environmentKey, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(environmentKey)); value != "" {
		return value
	}
	return fallback
}

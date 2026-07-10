package main

import (
	"os"
	"strings"

	"hq/internal/worker/adapter"
	"hq/internal/worker/agentadapter"
)

func init() {
	if err := adapter.InstallRuntimeDefaults(runtimeRegistrations); err != nil {
		panic(err)
	}
}

func runtimeRegistrations() []adapter.Registration {
	runner := agentadapter.OSRunner{}
	registrations := []adapter.Registration{{Target: "sh", Adapter: agentadapter.Sh{Runner: runner}}}
	registrations = append(registrations, agentadapter.Registrations(agentadapter.Config{
		Runner:     runner,
		HerdrPath:  executablePath("HQ_HERDR_PATH", "herdr"),
		CodexPath:  executablePath("HQ_CODEX_PATH", "codex"),
		ClaudePath: executablePath("HQ_CLAUDE_PATH", "claude"),
	})...)
	return registrations
}

func executablePath(environmentKey, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(environmentKey)); value != "" {
		return value
	}
	return fallback
}

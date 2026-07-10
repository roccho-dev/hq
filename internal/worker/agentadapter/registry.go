package agentadapter

import "hq/internal/worker/adapter"

type Config struct {
	Runner     Runner
	HerdrPath  string
	CodexPath  string
	ClaudePath string
}

func Registrations(config Config) []adapter.Registration {
	runner := config.Runner
	if runner == nil {
		runner = OSRunner{}
	}
	herdrPath := config.HerdrPath
	if herdrPath == "" {
		herdrPath = "herdr"
	}
	codexPath := config.CodexPath
	if codexPath == "" {
		codexPath = "codex"
	}
	claudePath := config.ClaudePath
	if claudePath == "" {
		claudePath = "claude"
	}
	return []adapter.Registration{
		{Target: "herdr", Adapter: Herdr{Runner: runner, Path: herdrPath}},
		{Target: "codex", Adapter: Codex{Runner: runner, Path: codexPath}},
		{Target: "claude", Adapter: Claude{Runner: runner, Path: claudePath}},
	}
}

func NewRegistry(config Config) (*adapter.Registry, error) {
	return adapter.NewRegistry(Registrations(config)...)
}

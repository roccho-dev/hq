package main

import (
	"reflect"
	"testing"

	"hq/internal/worker/adapter"
)

func TestRuntimeRegistryContainsEveryCanonicalTarget(t *testing.T) {
	registry, err := adapter.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "codex", "herdr", "sh"}
	if got := registry.Snapshot().Targets; !reflect.DeepEqual(got, want) {
		t.Fatalf("targets=%v want=%v", got, want)
	}
	for _, target := range want {
		if _, err := registry.Resolve(target); err != nil {
			t.Fatalf("resolve %s: %v", target, err)
		}
	}
}

func TestExecutablePathUsesExplicitOverrideOnly(t *testing.T) {
	t.Setenv("HQ_CODEX_PATH", "/exact/codex")
	if got := executablePath("HQ_CODEX_PATH", "codex"); got != "/exact/codex" {
		t.Fatalf("override=%q", got)
	}
	t.Setenv("HQ_CODEX_PATH", "   ")
	if got := executablePath("HQ_CODEX_PATH", "codex"); got != "codex" {
		t.Fatalf("fallback=%q", got)
	}
}

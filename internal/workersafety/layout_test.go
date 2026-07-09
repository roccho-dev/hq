package workersafety

import (
	"path/filepath"
	"testing"
)

func TestNewLayoutUsesOneProjectLocalConvention(t *testing.T) {
	projectRoot := t.TempDir()
	layout, err := NewLayout(projectRoot)
	if err != nil {
		t.Fatal(err)
	}

	artifactRoot := filepath.Join(projectRoot, ArtifactDirectory)
	want := map[string]string{
		"artifact": layout.ArtifactRoot,
		"queue":    layout.InstructionQueue,
		"events":   layout.EventLog,
		"sessions": layout.SessionLedger,
		"outputs":  layout.OutputsDir,
		"proofs":   layout.ProofsDir,
	}
	expected := map[string]string{
		"artifact": artifactRoot,
		"queue":    filepath.Join(artifactRoot, "queue", "instructions.jsonl"),
		"events":   filepath.Join(artifactRoot, "events", "events.jsonl"),
		"sessions": filepath.Join(artifactRoot, "sessions", "sessions.jsonl"),
		"outputs":  filepath.Join(artifactRoot, "outputs"),
		"proofs":   filepath.Join(artifactRoot, "proofs"),
	}
	for key, got := range want {
		if got != expected[key] {
			t.Fatalf("%s path: got %q want %q", key, got, expected[key])
		}
	}
	if err := layout.Validate(); err != nil {
		t.Fatalf("canonical layout must validate: %v", err)
	}
}

func TestLayoutRejectsDriftAndTraversal(t *testing.T) {
	layout, err := NewLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	layout.EventLog = filepath.Join(layout.ProjectRoot, "unrelated", "events.jsonl")
	if err := layout.Validate(); err == nil {
		t.Fatal("layout drift must fail")
	}

	layout, err = NewLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, badRunID := range []string{"", ".", "..", "../escape", `a/b`, `a\\b`} {
		if _, err := layout.FinalOutputPath(badRunID); err == nil {
			t.Fatalf("FinalOutputPath(%q) must fail", badRunID)
		}
		if _, err := layout.ProofPath(badRunID); err == nil {
			t.Fatalf("ProofPath(%q) must fail", badRunID)
		}
	}
}

func TestLayoutReturnsStableRunPaths(t *testing.T) {
	layout, err := NewLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	finalPath, err := layout.FinalOutputPath("run-001")
	if err != nil {
		t.Fatal(err)
	}
	proofPath, err := layout.ProofPath("run-001")
	if err != nil {
		t.Fatal(err)
	}
	if finalPath != filepath.Join(layout.OutputsDir, "run-001", "final.json") {
		t.Fatalf("unexpected final path: %q", finalPath)
	}
	if proofPath != filepath.Join(layout.ProofsDir, "run-001.jsonl") {
		t.Fatalf("unexpected proof path: %q", proofPath)
	}
}

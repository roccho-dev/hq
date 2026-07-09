package workersafety

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const ArtifactDirectory = ".hq"

// Layout is the single project-local convention for worker-owned durable data.
type Layout struct {
	ProjectRoot      string
	ArtifactRoot     string
	InstructionQueue string
	EventLog         string
	SessionLedger    string
	OutputsDir       string
	ProofsDir        string
}

// NewLayout resolves one project-local layout. It performs no filesystem I/O.
func NewLayout(projectRoot string) (Layout, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return Layout{}, errors.New("project root is required")
	}

	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return Layout{}, fmt.Errorf("resolve project root: %w", err)
	}
	root = filepath.Clean(root)
	artifactRoot := filepath.Join(root, ArtifactDirectory)

	layout := Layout{
		ProjectRoot:      root,
		ArtifactRoot:     artifactRoot,
		InstructionQueue: filepath.Join(artifactRoot, "queue", "instructions.jsonl"),
		EventLog:         filepath.Join(artifactRoot, "events", "events.jsonl"),
		SessionLedger:    filepath.Join(artifactRoot, "sessions", "sessions.jsonl"),
		OutputsDir:       filepath.Join(artifactRoot, "outputs"),
		ProofsDir:        filepath.Join(artifactRoot, "proofs"),
	}
	if err := layout.Validate(); err != nil {
		return Layout{}, err
	}
	return layout, nil
}

// Validate rejects drift from the canonical layout and any path escape.
func (l Layout) Validate() error {
	if strings.TrimSpace(l.ProjectRoot) == "" {
		return errors.New("project root is required")
	}

	root := filepath.Clean(l.ProjectRoot)
	expectedArtifactRoot := filepath.Join(root, ArtifactDirectory)
	expected := map[string]string{
		"artifact root":     expectedArtifactRoot,
		"instruction queue": filepath.Join(expectedArtifactRoot, "queue", "instructions.jsonl"),
		"event log":         filepath.Join(expectedArtifactRoot, "events", "events.jsonl"),
		"session ledger":    filepath.Join(expectedArtifactRoot, "sessions", "sessions.jsonl"),
		"outputs directory": filepath.Join(expectedArtifactRoot, "outputs"),
		"proofs directory":  filepath.Join(expectedArtifactRoot, "proofs"),
	}
	actual := map[string]string{
		"artifact root":     l.ArtifactRoot,
		"instruction queue": l.InstructionQueue,
		"event log":         l.EventLog,
		"session ledger":    l.SessionLedger,
		"outputs directory": l.OutputsDir,
		"proofs directory":  l.ProofsDir,
	}

	for name, want := range expected {
		got := filepath.Clean(actual[name])
		if got != want {
			return fmt.Errorf("%s drift: got %q want %q", name, got, want)
		}
		if !pathWithin(expectedArtifactRoot, got) {
			return fmt.Errorf("%s escapes artifact root: %q", name, got)
		}
	}
	return nil
}

// FinalOutputPath returns a stable final-output path for one run.
func (l Layout) FinalOutputPath(runID string) (string, error) {
	if err := validateSegment("run id", runID); err != nil {
		return "", err
	}
	path := filepath.Join(l.OutputsDir, runID, "final.json")
	if !pathWithin(l.OutputsDir, path) {
		return "", errors.New("final output path escapes outputs directory")
	}
	return path, nil
}

// ProofPath returns a stable proof path for one run.
func (l Layout) ProofPath(runID string) (string, error) {
	if err := validateSegment("run id", runID); err != nil {
		return "", err
	}
	path := filepath.Join(l.ProofsDir, runID+".jsonl")
	if !pathWithin(l.ProofsDir, path) {
		return "", errors.New("proof path escapes proofs directory")
	}
	return path, nil
}

func validateSegment(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if value == "." || value == ".." || filepath.Base(value) != value || strings.ContainsAny(value, `/\\`) {
		return fmt.Errorf("%s must be one path segment", name)
	}
	return nil
}

func pathWithin(base, candidate string) bool {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

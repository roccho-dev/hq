package hqprofile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadStrictExactProfile(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	profiles := filepath.Join(root, "profiles")
	if err := os.MkdirAll(filepath.Join(workspace, ".hq", "events"), 0o700); err != nil { t.Fatal(err) }
	if err := os.MkdirAll(profiles, 0o700); err != nil { t.Fatal(err) }
	world := filepath.Join(root, "world.jsonl")
	accepted := filepath.Join(root, "accepted.jsonl")
	caps := filepath.Join(root, "capabilities.json")
	for path, data := range map[string]string{world: "{}\n", accepted: "", caps: "{}\n"} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil { t.Fatal(err) }
	}
	profile := Profile{
		Kind: Kind, Name: "local", DeploymentID: "dep-1", WorldPath: world,
		AcceptedPath: accepted, WorkspaceRoot: workspace,
		EventsPath: filepath.Join(workspace, ".hq", "events", "events.jsonl"),
		CapabilitiesPath: caps, PollIntervalMS: 50, HealthTimeoutMS: 500,
	}
	encoded, _ := json.Marshal(profile)
	if err := os.WriteFile(filepath.Join(profiles, "local.json"), encoded, 0o600); err != nil { t.Fatal(err) }
	loaded, err := Load("local", profiles)
	if err != nil { t.Fatal(err) }
	if loaded.DeploymentID != "dep-1" || loaded.AcceptedPath != accepted { t.Fatalf("loaded=%+v", loaded) }
}

func TestProfileRejectsUnknownFieldsAndRelativePaths(t *testing.T) {
	_, err := Decode(strings.NewReader(`{"kind":"hq.profile.v1","name":"local","unknown":true}`))
	if err == nil { t.Fatal("unknown field must fail") }
	profile := Profile{Kind: Kind, Name: "local", DeploymentID: "dep", WorldPath: "world", AcceptedPath: "queue", WorkspaceRoot: "workspace", EventsPath: "events", CapabilitiesPath: "caps", PollIntervalMS: 50, HealthTimeoutMS: 500}
	if err := profile.Validate(); err == nil { t.Fatal("relative paths must fail") }
}

func TestProfileNameCannotEscapeRoot(t *testing.T) {
	if _, err := Load("../local", t.TempDir()); err == nil { t.Fatal("profile traversal must fail") }
}

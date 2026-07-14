package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hq/internal/hqprofile"
	"hq/internal/selectedworld"
	"hq/internal/workerclaim"
	"hq/internal/workerservice"
)

func TestManagedHealthCommandEmitsMatchedV2SelectionWithoutWrites(t *testing.T) {
	root := t.TempDir()
	profileRoot := filepath.Join(root, "profiles")
	workspace := filepath.Join(root, "workspace")
	eventsDir := filepath.Join(workspace, ".hq", "events")
	if err := os.MkdirAll(profileRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(eventsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(root, "world.jsonl")
	acceptedPath := filepath.Join(root, "accepted.jsonl")
	world := `{"kind":"hq.world.v1","world_id":"world.worker-command"}` + "\n" +
		`{"key":"reason","type":"string","required":true}` + "\n"
	if err := os.WriteFile(worldPath, []byte(world), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(acceptedPath, []byte("sentinel\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := hqprofile.Profile{
		Kind: hqprofile.Kind, Name: "local", DeploymentID: "deployment-health",
		WorldPath: worldPath, AcceptedPath: acceptedPath, WorkspaceRoot: workspace,
		EventsPath: filepath.Join(eventsDir, "events.jsonl"), PollIntervalMS: 50, HealthTimeoutMS: 5000,
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileRoot, "local.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	selection, err := selectedworld.Load(worldPath)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := workerclaim.Acquire(workspace, "worker-command", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Release()
	if _, err := claim.WriteHeartbeat(profile.DeploymentID, profile.Name, workerservice.StateReady, selection.Ref, time.Now()); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runManagedHealthTo([]string{"--profile", "local", "--profile-root", profileRoot}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 || strings.Count(stdout.String(), "\n") != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var report workerservice.Health
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &report); err != nil {
		t.Fatal(err)
	}
	if report.Kind != workerservice.HealthKind || !report.Ready || report.Profile != profile.Name || report.DeploymentID != profile.DeploymentID || report.SelectedWorld == nil || *report.SelectedWorld != selection.Ref {
		t.Fatalf("health report=%+v selection=%+v", report, selection.Ref)
	}
	if got, err := os.ReadFile(acceptedPath); err != nil || string(got) != "sentinel\n" {
		t.Fatalf("health changed accepted evidence: %q err=%v", got, err)
	}
	if _, err := os.Stat(profile.EventsPath); !os.IsNotExist(err) {
		t.Fatalf("health created event/history evidence: %v", err)
	}
}

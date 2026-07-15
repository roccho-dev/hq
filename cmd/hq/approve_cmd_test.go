package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"hq/internal/hqprofile"
	"hq/internal/worker"
)

func approvalCommandFixture(t *testing.T, payload map[string]any) (string, string) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, ".hq", "events"), 0o700); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(root, "world.jsonl")
	if err := os.WriteFile(worldPath, []byte(`{"kind":"hq.world.v1","world_id":"world.approval-test"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	acceptedPath := filepath.Join(root, "accepted.jsonl")
	accepted := map[string]any{
		"kind": "accepted.instruction", "queue": "instruction.jsonl",
		"instruction": map[string]any{
			"id": "ins-approve-001", "version": "instruction.v1", "op": "run", "target": "local-tool",
			"payload": payload, "created_at": "2026-07-15T07:00:00Z",
		},
	}
	encodedAccepted, _ := json.Marshal(accepted)
	if err := os.WriteFile(acceptedPath, append(encodedAccepted, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := hqprofile.Profile{
		Kind: hqprofile.Kind, Name: "local", DeploymentID: "dep-approve-test", WorldPath: worldPath,
		AcceptedPath: acceptedPath, WorkspaceRoot: workspace,
		EventsPath: filepath.Join(workspace, ".hq", "events", "events.jsonl"),
		PollIntervalMS: 20, HealthTimeoutMS: 500,
	}
	profileRoot := filepath.Join(root, "profiles")
	if err := os.MkdirAll(profileRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	encodedProfile, _ := json.Marshal(profile)
	if err := os.WriteFile(filepath.Join(profileRoot, "local.json"), encodedProfile, 0o600); err != nil {
		t.Fatal(err)
	}
	return profileRoot, workspace
}

func TestApproveCommandAppendsOneIdempotentResourceApprovalAcross32ConcurrentCalls(t *testing.T) {
	profileRoot, workspace := approvalCommandFixture(t, map[string]any{
		"tool_id": "aws", "tool_version": "2.35.11", "policy_version": "aws-restricted.v1",
		"argv": []string{"sts", "get-caller-identity"},
	})
	args := []string{"--profile", "local", "--profile-root", profileRoot, "--instruction", "ins-approve-001", "--approved-by", "owner@example"}
	type result struct {
		record worker.ApprovalRecord
		err    error
	}
	const attempts = 32
	start := make(chan struct{})
	results := make(chan result, attempts)
	var ready sync.WaitGroup
	ready.Add(attempts)
	for attempt := 0; attempt < attempts; attempt++ {
		go func() {
			ready.Done()
			<-start
			var stdout, stderr bytes.Buffer
			if code := runApprove(args, &stdout, &stderr); code != 0 {
				results <- result{err: fmt.Errorf("code=%d stderr=%s", code, stderr.String())}
				return
			}
			var record worker.ApprovalRecord
			if err := json.Unmarshal(stdout.Bytes(), &record); err != nil {
				results <- result{err: fmt.Errorf("stdout=%q: %w", stdout.String(), err)}
				return
			}
			results <- result{record: record}
		}()
	}
	ready.Wait()
	close(start)
	for attempt := 0; attempt < attempts; attempt++ {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.record.InstructionID != "ins-approve-001" || result.record.ApprovedBy != "owner@example" || !strings.HasPrefix(result.record.InstructionDigest, "sha256:") {
			t.Fatalf("record=%+v", result.record)
		}
	}
	data, err := os.ReadFile(worker.WorkspaceApprovalPath(workspace))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(strings.TrimSpace(string(data)), "\n") + 1; lines != 1 {
		t.Fatalf("approval ledger contains %d rows: %s", lines, data)
	}
	store, err := worker.LoadWorkspaceApprovals(workspace)
	if err != nil || store.ApprovalFor("ins-approve-001") == nil {
		t.Fatalf("approval ledger unreadable: store=%+v err=%v", store, err)
	}
}

func TestApproveCommandRejectsFiniteAction(t *testing.T) {
	profileRoot, _ := approvalCommandFixture(t, map[string]any{
		"tool_id": "dummy", "tool_version": "1", "action_id": "proof", "input": map[string]any{},
	})
	var stdout, stderr bytes.Buffer
	code := runApprove([]string{"--profile", "local", "--profile-root", profileRoot, "--instruction", "ins-approve-001", "--approved-by", "owner@example"}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "only verified-resource invocations") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

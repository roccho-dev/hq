package workerclaim

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestClaimHelperProcess(t *testing.T) {
	if os.Getenv("HQ_WORKER_CLAIM_HELPER") != "1" {
		return
	}
	root := os.Getenv("HQ_WORKER_CLAIM_ROOT")
	ready := os.Getenv("HQ_WORKER_CLAIM_READY")
	release := os.Getenv("HQ_WORKER_CLAIM_RELEASE")
	claim, err := Acquire(root, "helper", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ready, []byte(claim.Owner().ClaimID), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(release); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("release signal timeout")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := claim.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestTwoProcessesCannotClaimOneWorkspace(t *testing.T) {
	root := t.TempDir()
	ready := filepath.Join(t.TempDir(), "ready")
	release := filepath.Join(t.TempDir(), "release")
	command := exec.Command(os.Args[0], "-test.run=^TestClaimHelperProcess$")
	command.Env = append(os.Environ(),
		"HQ_WORKER_CLAIM_HELPER=1",
		"HQ_WORKER_CLAIM_ROOT="+root,
		"HQ_WORKER_CLAIM_READY="+ready,
		"HQ_WORKER_CLAIM_RELEASE="+release,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.WriteFile(release, []byte("release"), 0o600)
		_ = command.Wait()
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper did not acquire claim")
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, err := Acquire(root, "second", time.Now())
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected machine-readable conflict, got %v", err)
	}
	if conflict.Inspection.Owner == nil || conflict.Inspection.Owner.WorkerID != "helper" || !conflict.Inspection.Exists {
		t.Fatalf("conflict evidence=%+v", conflict.Inspection)
	}
}

func TestDifferentWorkspaceRootsRunIndependently(t *testing.T) {
	first, err := Acquire(t.TempDir(), "first", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := Acquire(t.TempDir(), "second", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
}

func TestExplicitStaleRecoveryKeepsOtherEvidence(t *testing.T) {
	root := t.TempDir()
	events := filepath.Join(root, ".hq", "events", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(events), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(events, []byte("durable-evidence\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-2 * time.Hour)
	claim, err := Acquire(root, "crashed-worker", started)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := Recover(root, claim.Owner().ClaimID, "operator verified crashed process", time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ClaimID != claim.Owner().ClaimID || receipt.Kind != RecoveryKind {
		t.Fatalf("receipt=%+v", receipt)
	}
	content, err := os.ReadFile(events)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "durable-evidence\n" {
		t.Fatalf("recovery changed event evidence: %q", content)
	}
	proof := filepath.Join(root, ".hq", "proofs", recoveryLogName)
	file, err := os.Open(proof)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var recorded RecoveryReceipt
	if err := json.NewDecoder(bufio.NewReader(file)).Decode(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded.ClaimID != receipt.ClaimID {
		t.Fatalf("recorded=%+v receipt=%+v", recorded, receipt)
	}
	replacement, err := Acquire(root, "replacement", time.Now())
	if err != nil {
		t.Fatalf("valid recovery did not free claim: %v", err)
	}
	defer replacement.Release()
}

func TestRecoveryRejectsFreshOrMismatchedClaim(t *testing.T) {
	root := t.TempDir()
	claim, err := Acquire(root, "live", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Release()
	if _, err := Recover(root, "other", "wrong id", time.Now().Add(2*time.Hour), time.Hour); err == nil {
		t.Fatal("mismatched claim id must fail")
	}
	if _, err := Recover(root, claim.Owner().ClaimID, "too early", time.Now(), time.Hour); err == nil {
		t.Fatal("fresh claim recovery must fail")
	}
}

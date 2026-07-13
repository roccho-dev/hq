package workerservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hq/internal/hqprofile"
	"hq/internal/workerclaim"
)

func TestServeStopsThroughExactProfileControl(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, ".hq", "events"), 0o700); err != nil {
		t.Fatal(err)
	}
	acceptedPath := filepath.Join(root, "accepted.jsonl")
	if err := os.WriteFile(acceptedPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	profile := hqprofile.Profile{
		Name: "selected", DeploymentID: "dep-stop-test", WorkspaceRoot: workspace,
		AcceptedPath: acceptedPath, EventsPath: filepath.Join(workspace, ".hq", "events", "events.jsonl"),
		PollIntervalMS: 10, HealthTimeoutMS: 500,
	}
	var output bytes.Buffer
	controlled, cancelControl, observations := WithStopControl(context.Background(), profile)
	defer cancelControl()
	done := make(chan error, 1)
	go func() { done <- Serve(controlled, profile, "worker-stop-service-test", &output) }()

	owner := waitFreshOwner(t, profile, 2*time.Second)
	request, err := RequestStop(profile, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if request.ClaimID != owner.ClaimID || request.WorkerID != owner.WorkerID {
		t.Fatalf("request=%+v owner=%+v", request, owner)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("managed service did not stop through exact-profile control")
	}
	var observation StopObservation
	select {
	case observation = <-observations:
		if observation.Err != nil || observation.Request == nil || observation.Request.RequestID != request.RequestID {
			t.Fatalf("observation=%+v", observation)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("missing service stop observation")
	}
	receipt, err := AcknowledgeStop(profile, *observation.Request, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelWait()
	observedReceipt, err := WaitStopped(waitCtx, profile, request)
	if err != nil {
		t.Fatal(err)
	}
	if observedReceipt != receipt {
		t.Fatalf("observed receipt=%+v want=%+v", observedReceipt, receipt)
	}
	if inspection, err := workerclaim.Inspect(workspace, time.Now(), 0); err != nil || inspection.Exists {
		t.Fatalf("claim remained after graceful stop: inspection=%+v err=%v", inspection, err)
	}
	if _, err := workerclaim.ReadHeartbeat(workspace); !os.IsNotExist(err) {
		t.Fatalf("heartbeat remained after graceful stop: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("lifecycle output=%q", output.String())
	}
	var final Lifecycle
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &final); err != nil {
		t.Fatal(err)
	}
	if final.State != "stopped" || final.ClaimID != owner.ClaimID {
		t.Fatalf("final lifecycle=%+v", final)
	}
}

func TestClaimDisappearanceWithoutWorkerAcknowledgementIsNonGreen(t *testing.T) {
	profile, claim := claimedControlProfile(t)
	request, err := RequestStop(profile, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := workerclaim.RemoveHeartbeat(profile.WorkspaceRoot); err != nil {
		t.Fatal(err)
	}
	if err := claim.Release(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	if _, err := WaitStopped(ctx, profile, request); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("claim disappearance without worker acknowledgement error=%v", err)
	}
}

func TestAcknowledgeRejectsLingeringHeartbeat(t *testing.T) {
	profile, claim := claimedControlProfile(t)
	request, err := RequestStop(profile, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := claim.Release(); err != nil {
		t.Fatal(err)
	}
	defer workerclaim.RemoveHeartbeat(profile.WorkspaceRoot)
	if _, err := AcknowledgeStop(profile, request, time.Now()); err == nil {
		t.Fatal("graceful-stop acknowledgement accepted a lingering heartbeat")
	}
}

func TestStopControlDoesNotCancelWithoutRequest(t *testing.T) {
	profile, claim := claimedControlProfile(t)
	defer claim.Release()
	defer workerclaim.RemoveHeartbeat(profile.WorkspaceRoot)
	parent, cancelParent := context.WithCancel(context.Background())
	controlled, cancelControl, observations := WithStopControl(parent, profile)
	select {
	case <-controlled.Done():
		t.Fatal("managed context cancelled without a stop request")
	case <-time.After(75 * time.Millisecond):
	}
	cancelParent()
	cancelControl()
	select {
	case observation, ok := <-observations:
		if ok && (observation.Err != nil || observation.Request != nil) {
			t.Fatalf("unexpected observation=%+v", observation)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop monitor did not terminate after parent cancellation")
	}
}

func TestRequestStopRejectsAbsentOrMismatchedManagedWorker(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := hqprofile.Profile{
		Name: "selected", DeploymentID: "dep-stop-test", WorkspaceRoot: workspace,
		PollIntervalMS: 10, HealthTimeoutMS: 500,
	}
	if _, err := RequestStop(profile, time.Now()); err == nil {
		t.Fatal("absent worker was represented as a graceful stop target")
	}
	claim, err := workerclaim.Acquire(workspace, "worker-stop-test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Release()
	if _, err := claim.WriteHeartbeat("wrong-deployment", profile.Name, StateReady, time.Now()); err != nil {
		t.Fatal(err)
	}
	defer workerclaim.RemoveHeartbeat(workspace)
	if _, err := RequestStop(profile, time.Now()); err == nil {
		t.Fatal("mismatched deployment heartbeat was accepted for graceful stop")
	}
}

func claimedControlProfile(t *testing.T) (hqprofile.Profile, *workerclaim.Claim) {
	t.Helper()
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := hqprofile.Profile{
		Name: "selected", DeploymentID: "dep-stop-test", WorkspaceRoot: workspace,
		PollIntervalMS: 10, HealthTimeoutMS: 500,
	}
	claim, err := workerclaim.Acquire(workspace, "worker-stop-test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := claim.WriteHeartbeat(profile.DeploymentID, profile.Name, StateReady, time.Now()); err != nil {
		t.Fatal(err)
	}
	return profile, claim
}

func waitFreshOwner(t *testing.T, profile hqprofile.Profile, timeout time.Duration) workerclaim.Owner {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		inspection, err := workerclaim.Inspect(profile.WorkspaceRoot, time.Now(), 0)
		if err == nil && inspection.Owner != nil {
			_, fresh, heartbeatErr := workerclaim.HeartbeatFresh(profile.WorkspaceRoot, inspection.Owner.ClaimID, time.Now(), profile.HealthTimeout())
			if heartbeatErr == nil && fresh {
				return *inspection.Owner
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("managed service did not publish a fresh claim and heartbeat")
	return workerclaim.Owner{}
}

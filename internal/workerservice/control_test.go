package workerservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hq/internal/core"
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
	worldPath := filepath.Join(root, "world.jsonl")
	world := `{"kind":"hq.world.v1","world_id":"world.stop-control-test"}` + "\n" +
		`{"key":"reason","type":"string"}` + "\n"
	if err := os.WriteFile(worldPath, []byte(world), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := hqprofile.Profile{
		Name: "selected", DeploymentID: "dep-stop-test", WorkspaceRoot: workspace,
		WorldPath: worldPath, AcceptedPath: acceptedPath,
		EventsPath:     filepath.Join(workspace, ".hq", "events", "events.jsonl"),
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

func TestStopControlAtomicReplacementSupportsConcurrentReaders(t *testing.T) {
	request := StopRequest{
		Kind: StopRequestKind, RequestID: "request-atomic-read", ClaimID: "claim-atomic-read",
		WorkerID: "worker-atomic-read", Workspace: t.TempDir(), Profile: "selected",
		DeploymentID: "deployment-atomic-read", RequestedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
	receipt := StopReceipt{
		Kind: StopReceiptKind, RequestID: request.RequestID, ClaimID: request.ClaimID,
		WorkerID: request.WorkerID, Workspace: request.Workspace, Profile: request.Profile,
		DeploymentID: request.DeploymentID, RequestedAt: request.RequestedAt,
		StoppedAt: request.RequestedAt.Add(time.Second),
	}
	tests := []struct {
		name  string
		value any
		read  func(string) error
	}{
		{name: "request", value: request, read: func(path string) error {
			observed, err := readStopRequest(path)
			if err == nil && observed != request {
				return fmt.Errorf("atomic stop request changed: got %+v want %+v", observed, request)
			}
			return err
		}},
		{name: "receipt", value: receipt, read: func(path string) error {
			observed, err := readStopReceipt(path)
			if err == nil && observed != receipt {
				return fmt.Errorf("atomic stop receipt changed: got %+v want %+v", observed, receipt)
			}
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "claim-stop."+test.name+".json")
			if err := writeJSONAtomic(path, test.value); err != nil {
				t.Fatal(err)
			}
			started := make(chan struct{})
			stop := make(chan struct{})
			done := make(chan struct{})
			readErr := make(chan error, 1)
			go func() {
				defer close(done)
				first := true
				for {
					if err := test.read(path); err != nil {
						readErr <- err
						return
					}
					if first {
						close(started)
						first = false
					}
					select {
					case <-stop:
						return
					default:
					}
				}
			}()
			<-started
			for range 250 {
				if err := writeJSONAtomic(path, test.value); err != nil {
					close(stop)
					<-done
					t.Fatal(err)
				}
			}
			close(stop)
			<-done
			select {
			case err := <-readErr:
				t.Fatal(err)
			default:
			}
		})
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
	if _, err := claim.WriteHeartbeat("wrong-deployment", profile.Name, StateReady, controlTestWorldRef(), time.Now()); err != nil {
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
	if _, err := claim.WriteHeartbeat(profile.DeploymentID, profile.Name, StateReady, controlTestWorldRef(), time.Now()); err != nil {
		t.Fatal(err)
	}
	return profile, claim
}

func controlTestWorldRef() core.WorldRef {
	return core.WorldRef{WorldID: "world.control-test", Digest: "sha256:" + strings.Repeat("0", 64)}
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

package workerservice

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hq/internal/hqprofile"
	"hq/internal/workerclaim"
	"hq/internal/workersafety"
)

const StopRequestKind = "hq.workerStopRequest.v1"
const StopReceiptKind = "hq.workerStopReceipt.v1"

// StopRequest is local control evidence bound to one exact managed-worker claim.
// It is not queue, instruction, result, or deployment authority.
type StopRequest struct {
	Kind         string    `json:"kind"`
	RequestID    string    `json:"request_id"`
	ClaimID      string    `json:"claim_id"`
	WorkerID     string    `json:"worker_id"`
	Workspace    string    `json:"workspace"`
	Profile      string    `json:"profile"`
	DeploymentID string    `json:"deployment_id"`
	RequestedAt  time.Time `json:"requested_at"`
}

type StopReceipt struct {
	Kind         string    `json:"kind"`
	RequestID    string    `json:"request_id"`
	ClaimID      string    `json:"claim_id"`
	WorkerID     string    `json:"worker_id"`
	Workspace    string    `json:"workspace"`
	Profile      string    `json:"profile"`
	DeploymentID string    `json:"deployment_id"`
	RequestedAt  time.Time `json:"requested_at"`
	StoppedAt    time.Time `json:"stopped_at"`
}

type StopObservation struct {
	Request *StopRequest
	Err     error
}

// WithStopControl derives a context cancelled only by the parent or by a valid
// stop request for the exact current claim/profile/deployment.
func WithStopControl(parent context.Context, profile hqprofile.Profile) (context.Context, context.CancelFunc, <-chan StopObservation) {
	ctx, cancel := context.WithCancel(parent)
	observations := make(chan StopObservation, 1)
	go func() {
		defer close(observations)
		ticker := time.NewTicker(controlPollInterval(profile))
		defer ticker.Stop()
		for {
			request, found, err := currentStopRequest(profile)
			if err != nil {
				observations <- StopObservation{Err: err}
				cancel()
				return
			}
			if found {
				observations <- StopObservation{Request: &request}
				cancel()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return ctx, cancel, observations
}

// RequestStop writes one atomic request for the exact fresh managed-worker
// claim. An absent or stale worker is never represented as graceful success.
func RequestStop(profile hqprofile.Profile, now time.Time) (StopRequest, error) {
	if err := validateControlProfile(profile); err != nil {
		return StopRequest{}, err
	}
	inspection, err := workerclaim.Inspect(profile.WorkspaceRoot, now, profile.HealthTimeout())
	if err != nil {
		return StopRequest{}, err
	}
	if !inspection.Exists || inspection.Owner == nil {
		return StopRequest{}, errors.New("managed worker claim is absent")
	}
	owner := *inspection.Owner
	heartbeat, fresh, err := workerclaim.HeartbeatFresh(profile.WorkspaceRoot, owner.ClaimID, now, profile.HealthTimeout())
	if err != nil {
		return StopRequest{}, err
	}
	if !fresh {
		return StopRequest{}, errors.New("managed worker heartbeat is missing or stale")
	}
	if heartbeat.Profile != profile.Name || heartbeat.DeploymentID != profile.DeploymentID || heartbeat.WorkerID != owner.WorkerID || heartbeat.Workspace != owner.Workspace {
		return StopRequest{}, errors.New("managed worker heartbeat does not match the selected profile and claim")
	}
	request := StopRequest{
		Kind: StopRequestKind, RequestID: "stop-" + owner.ClaimID, ClaimID: owner.ClaimID,
		WorkerID: owner.WorkerID, Workspace: owner.Workspace, Profile: profile.Name,
		DeploymentID: profile.DeploymentID, RequestedAt: now.UTC(),
	}
	path, err := stopControlPath(owner.Workspace, owner.ClaimID, "request")
	if err != nil {
		return StopRequest{}, err
	}
	if existing, readErr := readStopRequest(path); readErr == nil {
		if sameStopTarget(existing, request) {
			return existing, nil
		}
		return StopRequest{}, errors.New("a conflicting stop request already exists for the current claim")
	} else if !os.IsNotExist(readErr) {
		return StopRequest{}, readErr
	}
	if err := writeJSONAtomic(path, request); err != nil {
		return StopRequest{}, err
	}
	return request, nil
}

// AcknowledgeStop is called by the managed worker itself after Serve returned
// normally and released its exact claim. A caller cannot mint graceful success
// merely by observing that a process or claim disappeared.
func AcknowledgeStop(profile hqprofile.Profile, request StopRequest, now time.Time) (StopReceipt, error) {
	if err := validateControlProfile(profile); err != nil {
		return StopReceipt{}, err
	}
	if err := validateStopRequest(request); err != nil {
		return StopReceipt{}, err
	}
	if request.Profile != profile.Name || request.DeploymentID != profile.DeploymentID || request.Workspace != profile.WorkspaceRoot {
		return StopReceipt{}, errors.New("stop request does not match the selected profile")
	}
	path, err := stopControlPath(request.Workspace, request.ClaimID, "request")
	if err != nil {
		return StopReceipt{}, err
	}
	stored, err := readStopRequest(path)
	if err != nil {
		return StopReceipt{}, err
	}
	if stored != request {
		return StopReceipt{}, errors.New("stop request changed before acknowledgement")
	}
	inspection, err := workerclaim.Inspect(profile.WorkspaceRoot, now, 0)
	if err != nil {
		return StopReceipt{}, err
	}
	if inspection.Exists {
		return StopReceipt{}, errors.New("managed worker claim still exists; refusing graceful-stop acknowledgement")
	}
	receipt := StopReceipt{
		Kind: StopReceiptKind, RequestID: request.RequestID, ClaimID: request.ClaimID,
		WorkerID: request.WorkerID, Workspace: request.Workspace, Profile: request.Profile,
		DeploymentID: request.DeploymentID, RequestedAt: request.RequestedAt, StoppedAt: now.UTC(),
	}
	receiptPath, err := stopControlPath(request.Workspace, request.ClaimID, "receipt")
	if err != nil {
		return StopReceipt{}, err
	}
	if err := writeJSONAtomic(receiptPath, receipt); err != nil {
		return StopReceipt{}, err
	}
	return receipt, nil
}

// WaitStopped requires both worker-written acknowledgement and exact claim
// release. Claim disappearance without acknowledgement is a crash/non-green.
func WaitStopped(ctx context.Context, profile hqprofile.Profile, request StopRequest) (StopReceipt, error) {
	if err := validateControlProfile(profile); err != nil {
		return StopReceipt{}, err
	}
	if err := validateStopRequest(request); err != nil {
		return StopReceipt{}, err
	}
	receiptPath, err := stopControlPath(request.Workspace, request.ClaimID, "receipt")
	if err != nil {
		return StopReceipt{}, err
	}
	ticker := time.NewTicker(controlPollInterval(profile))
	defer ticker.Stop()
	for {
		receipt, receiptErr := readStopReceipt(receiptPath)
		if receiptErr == nil {
			if !receiptMatchesRequest(receipt, request) {
				return StopReceipt{}, errors.New("graceful-stop receipt does not match the exact request")
			}
			inspection, inspectErr := workerclaim.Inspect(profile.WorkspaceRoot, time.Now(), 0)
			if inspectErr != nil {
				return StopReceipt{}, inspectErr
			}
			if !inspection.Exists {
				return receipt, nil
			}
			if inspection.Owner == nil || inspection.Owner.ClaimID != request.ClaimID {
				return StopReceipt{}, errors.New("managed worker claim changed before the selected profile became stopped")
			}
		} else if !os.IsNotExist(receiptErr) {
			return StopReceipt{}, receiptErr
		}
		inspection, inspectErr := workerclaim.Inspect(profile.WorkspaceRoot, time.Now(), 0)
		if inspectErr != nil {
			return StopReceipt{}, inspectErr
		}
		if inspection.Exists && (inspection.Owner == nil || inspection.Owner.ClaimID != request.ClaimID) {
			return StopReceipt{}, errors.New("managed worker claim changed before graceful-stop acknowledgement")
		}
		select {
		case <-ctx.Done():
			return StopReceipt{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func currentStopRequest(profile hqprofile.Profile) (StopRequest, bool, error) {
	if err := validateControlProfile(profile); err != nil {
		return StopRequest{}, false, err
	}
	inspection, err := workerclaim.Inspect(profile.WorkspaceRoot, time.Now(), 0)
	if err != nil {
		return StopRequest{}, false, err
	}
	if !inspection.Exists || inspection.Owner == nil {
		return StopRequest{}, false, nil
	}
	owner := *inspection.Owner
	path, err := stopControlPath(owner.Workspace, owner.ClaimID, "request")
	if err != nil {
		return StopRequest{}, false, err
	}
	request, err := readStopRequest(path)
	if os.IsNotExist(err) {
		return StopRequest{}, false, nil
	}
	if err != nil {
		return StopRequest{}, false, err
	}
	if request.ClaimID != owner.ClaimID || request.WorkerID != owner.WorkerID || request.Workspace != owner.Workspace || request.Profile != profile.Name || request.DeploymentID != profile.DeploymentID {
		return StopRequest{}, false, errors.New("stop request does not match the current managed-worker claim/profile/deployment")
	}
	return request, true, nil
}

func validateControlProfile(profile hqprofile.Profile) error {
	if strings.TrimSpace(profile.Name) == "" || strings.TrimSpace(profile.DeploymentID) == "" || strings.TrimSpace(profile.WorkspaceRoot) == "" {
		return errors.New("profile name, deployment id, and workspace root are required for worker control")
	}
	if profile.HealthTimeout() <= 0 {
		return errors.New("profile health timeout must be positive")
	}
	return nil
}

func validateStopRequest(request StopRequest) error {
	if request.Kind != StopRequestKind || !safeControlSegment(request.RequestID) || !safeControlSegment(request.ClaimID) || strings.TrimSpace(request.WorkerID) == "" || strings.TrimSpace(request.Workspace) == "" || strings.TrimSpace(request.Profile) == "" || strings.TrimSpace(request.DeploymentID) == "" || request.RequestedAt.IsZero() {
		return errors.New("invalid managed-worker stop request")
	}
	return nil
}

func validateStopReceipt(receipt StopReceipt) error {
	if receipt.Kind != StopReceiptKind || !safeControlSegment(receipt.RequestID) || !safeControlSegment(receipt.ClaimID) || strings.TrimSpace(receipt.WorkerID) == "" || strings.TrimSpace(receipt.Workspace) == "" || strings.TrimSpace(receipt.Profile) == "" || strings.TrimSpace(receipt.DeploymentID) == "" || receipt.RequestedAt.IsZero() || receipt.StoppedAt.IsZero() || receipt.StoppedAt.Before(receipt.RequestedAt) {
		return errors.New("invalid managed-worker stop receipt")
	}
	return nil
}

func sameStopTarget(left, right StopRequest) bool {
	return left.Kind == StopRequestKind && left.RequestID == right.RequestID && left.ClaimID == right.ClaimID && left.WorkerID == right.WorkerID && left.Workspace == right.Workspace && left.Profile == right.Profile && left.DeploymentID == right.DeploymentID
}

func receiptMatchesRequest(receipt StopReceipt, request StopRequest) bool {
	return receipt.Kind == StopReceiptKind && receipt.RequestID == request.RequestID && receipt.ClaimID == request.ClaimID && receipt.WorkerID == request.WorkerID && receipt.Workspace == request.Workspace && receipt.Profile == request.Profile && receipt.DeploymentID == request.DeploymentID && receipt.RequestedAt.Equal(request.RequestedAt)
}

func stopControlPath(projectRoot, claimID, suffix string) (string, error) {
	if !safeControlSegment(claimID) || (suffix != "request" && suffix != "receipt") {
		return "", errors.New("invalid worker stop control path")
	}
	layout, err := workersafety.NewLayout(projectRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(layout.ArtifactRoot, "worker", "stop", claimID+"."+suffix+".json"), nil
}

func readStopRequest(path string) (StopRequest, error) {
	var request StopRequest
	if err := readStrictJSON(path, &request); err != nil {
		return StopRequest{}, err
	}
	if err := validateStopRequest(request); err != nil {
		return StopRequest{}, err
	}
	return request, nil
}

func readStopReceipt(path string) (StopReceipt, error) {
	var receipt StopReceipt
	if err := readStrictJSON(path, &receipt); err != nil {
		return StopReceipt{}, err
	}
	if err := validateStopReceipt(receipt); err != nil {
		return StopReceipt{}, err
	}
	return receipt, nil
}

func readStrictJSON(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(bufio.NewReader(file))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values in worker stop control record")
		}
		return err
	}
	return nil
}

func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".hq-worker-stop-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetEscapeHTML(false)
	writeErr := encoder.Encode(value)
	if writeErr == nil {
		writeErr = temporary.Sync()
	}
	closeErr := temporary.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temporaryPath, path)
}

func controlPollInterval(profile hqprofile.Profile) time.Duration {
	interval := profile.PollInterval()
	if interval <= 0 || interval > 250*time.Millisecond {
		return 100 * time.Millisecond
	}
	return interval
}

func safeControlSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

package workerclaim

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hq/internal/atomicfile"
	"hq/internal/workersafety"
)

const HeartbeatKind = "worker.heartbeat.v1"
const heartbeatName = "heartbeat.json"

type Heartbeat struct {
	Kind         string    `json:"kind"`
	ClaimID      string    `json:"claim_id"`
	WorkerID     string    `json:"worker_id"`
	Workspace    string    `json:"workspace"`
	DeploymentID string    `json:"deployment_id"`
	Profile      string    `json:"profile"`
	State        string    `json:"state"`
	ObservedAt   time.Time `json:"observed_at"`
}

func (c *Claim) WriteHeartbeat(deploymentID, profile, state string, now time.Time) (Heartbeat, error) {
	if c == nil || c.path == "" {
		return Heartbeat{}, errors.New("claim is not initialized")
	}
	if strings.TrimSpace(deploymentID) == "" || strings.TrimSpace(profile) == "" || strings.TrimSpace(state) == "" {
		return Heartbeat{}, errors.New("heartbeat deployment, profile, and state are required")
	}
	current, err := readOwner(c.path)
	if err != nil {
		return Heartbeat{}, err
	}
	if current.ClaimID != c.owner.ClaimID || current.WorkerID != c.owner.WorkerID || current.Workspace != c.owner.Workspace {
		return Heartbeat{}, errors.New("claim identity changed; refusing heartbeat")
	}
	record := Heartbeat{Kind: HeartbeatKind, ClaimID: current.ClaimID, WorkerID: current.WorkerID, Workspace: current.Workspace, DeploymentID: deploymentID, Profile: profile, State: state, ObservedAt: now.UTC()}
	path := filepath.Join(filepath.Dir(c.path), heartbeatName)
	if err := atomicfile.WriteJSON(path, record); err != nil {
		return Heartbeat{}, err
	}
	return record, nil
}

func ReadHeartbeat(projectRoot string) (Heartbeat, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return Heartbeat{}, err
	}
	layout, err := workersafety.NewLayout(root)
	if err != nil {
		return Heartbeat{}, err
	}
	path := filepath.Join(layout.ArtifactRoot, "worker", heartbeatName)
	data, err := readHeartbeatFile(path)
	if err != nil {
		return Heartbeat{}, err
	}
	return decodeHeartbeat(data)
}

func decodeHeartbeat(data []byte) (Heartbeat, error) {
	var record Heartbeat
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return Heartbeat{}, err
	}
	if record.Kind != HeartbeatKind || strings.TrimSpace(record.ClaimID) == "" || strings.TrimSpace(record.WorkerID) == "" || strings.TrimSpace(record.Workspace) == "" || strings.TrimSpace(record.DeploymentID) == "" || strings.TrimSpace(record.Profile) == "" || strings.TrimSpace(record.State) == "" || record.ObservedAt.IsZero() {
		return Heartbeat{}, errors.New("invalid worker heartbeat")
	}
	return record, nil
}

func HeartbeatFresh(projectRoot, expectedClaimID string, now time.Time, staleAfter time.Duration) (Heartbeat, bool, error) {
	if staleAfter <= 0 {
		return Heartbeat{}, false, errors.New("stale-after must be positive")
	}
	record, err := ReadHeartbeat(projectRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return Heartbeat{}, false, nil
		}
		return Heartbeat{}, false, err
	}
	if record.ClaimID != expectedClaimID {
		return record, false, fmt.Errorf("heartbeat claim id mismatch")
	}
	return record, now.UTC().Before(record.ObservedAt.Add(staleAfter)), nil
}

func RemoveHeartbeat(projectRoot string) error {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return err
	}
	layout, err := workersafety.NewLayout(root)
	if err != nil {
		return err
	}
	err = atomicfile.Remove(filepath.Join(layout.ArtifactRoot, "worker", heartbeatName))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func RecoverManaged(projectRoot, expectedClaimID, reason string, now time.Time, staleAfter time.Duration) (RecoveryReceipt, error) {
	_, fresh, err := HeartbeatFresh(projectRoot, expectedClaimID, now, staleAfter)
	if err != nil {
		return RecoveryReceipt{}, err
	}
	if fresh {
		return RecoveryReceipt{}, errors.New("managed worker heartbeat is fresh; refusing recovery")
	}
	receipt, err := Recover(projectRoot, expectedClaimID, reason, now, staleAfter)
	if err != nil {
		return RecoveryReceipt{}, err
	}
	_ = RemoveHeartbeat(projectRoot)
	return receipt, nil
}

// Package workerclaim provides one atomic project-local execution claim for
// hq-worker processes. It is local-only and deliberately does not implement a
// distributed lock or automatic claim stealing.
package workerclaim

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hq/internal/atomicfile"
	"hq/internal/workersafety"
)

const (
	OwnerKind       = "worker.claim.v1"
	InspectionKind  = "worker.claimInspection.v1"
	RecoveryKind    = "worker.claimRecovery.v1"
	claimRelative   = "worker/claim.json"
	recoveryLogName = "worker-claim-recovery.jsonl"
)

type Owner struct {
	Kind      string    `json:"kind"`
	ClaimID   string    `json:"claim_id"`
	WorkerID  string    `json:"worker_id"`
	ProcessID int       `json:"process_id"`
	Workspace string    `json:"workspace"`
	StartedAt time.Time `json:"started_at"`
}

type Inspection struct {
	Kind           string `json:"kind"`
	Exists         bool   `json:"exists"`
	StaleCandidate bool   `json:"stale_candidate"`
	ClaimPath      string `json:"claim_path"`
	Owner          *Owner `json:"owner,omitempty"`
	ReadError      string `json:"read_error,omitempty"`
}

type RecoveryReceipt struct {
	Kind        string    `json:"kind"`
	ClaimID     string    `json:"claim_id"`
	WorkerID    string    `json:"worker_id"`
	Workspace   string    `json:"workspace"`
	RecoveredAt time.Time `json:"recovered_at"`
	Reason      string    `json:"reason"`
}

type ConflictError struct {
	Inspection Inspection
}

func (e *ConflictError) Error() string {
	if e == nil {
		return "worker claim conflict"
	}
	if e.Inspection.Owner != nil {
		return fmt.Sprintf("workspace already claimed by %s (%s)", e.Inspection.Owner.WorkerID, e.Inspection.Owner.ClaimID)
	}
	return "workspace claim exists and cannot be safely inspected"
}

type Claim struct {
	path  string
	owner Owner
}

func (c *Claim) Owner() Owner {
	if c == nil {
		return Owner{}
	}
	return c.owner
}

func (c *Claim) Path() string {
	if c == nil {
		return ""
	}
	return c.path
}

// Acquire publishes a complete owner record with an exclusive hard link so
// separate processes on the same local filesystem cannot both become owner.
func Acquire(projectRoot, workerID string, now time.Time) (*Claim, error) {
	if strings.TrimSpace(workerID) == "" {
		return nil, errors.New("worker id is required")
	}
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return nil, err
	}
	layout, err := workersafety.NewLayout(root)
	if err != nil {
		return nil, err
	}
	claimPath := filepath.Join(layout.ArtifactRoot, filepath.FromSlash(claimRelative))
	if err := os.MkdirAll(filepath.Dir(claimPath), 0o700); err != nil {
		return nil, fmt.Errorf("create worker claim directory: %w", err)
	}
	claimID, err := randomID()
	if err != nil {
		return nil, err
	}
	owner := Owner{
		Kind: OwnerKind, ClaimID: claimID, WorkerID: workerID, ProcessID: os.Getpid(),
		Workspace: layout.ProjectRoot, StartedAt: now.UTC(),
	}
	file, err := os.CreateTemp(filepath.Dir(claimPath), ".hq-worker-claim-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("acquire worker claim: %w", err)
	}
	temporaryPath := file.Name()
	defer os.Remove(temporaryPath)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure worker claim: %w", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	writeErr := encoder.Encode(owner)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return nil, fmt.Errorf("write worker claim: %w", writeErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close worker claim: %w", closeErr)
	}
	if err := os.Link(temporaryPath, claimPath); os.IsExist(err) {
		inspection, inspectErr := Inspect(root, now, 0)
		if inspectErr != nil {
			inspection = Inspection{Kind: InspectionKind, Exists: true, ClaimPath: claimPath, ReadError: inspectErr.Error()}
		}
		return nil, &ConflictError{Inspection: inspection}
	} else if err != nil {
		return nil, fmt.Errorf("publish worker claim: %w", err)
	}
	return &Claim{path: claimPath, owner: owner}, nil
}

// Release removes only the exact claim acquired by this instance.
func (c *Claim) Release() error {
	if c == nil || c.path == "" {
		return errors.New("claim is not initialized")
	}
	current, err := readOwner(c.path)
	if err != nil {
		return fmt.Errorf("inspect claim before release: %w", err)
	}
	if current.ClaimID != c.owner.ClaimID || current.WorkerID != c.owner.WorkerID || current.Workspace != c.owner.Workspace {
		return errors.New("claim identity changed; refusing release")
	}
	if err := atomicfile.Remove(c.path); err != nil {
		return fmt.Errorf("release worker claim: %w", err)
	}
	c.path = ""
	return nil
}

func Inspect(projectRoot string, now time.Time, staleAfter time.Duration) (Inspection, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return Inspection{}, err
	}
	layout, err := workersafety.NewLayout(root)
	if err != nil {
		return Inspection{}, err
	}
	path := filepath.Join(layout.ArtifactRoot, filepath.FromSlash(claimRelative))
	owner, err := readOwner(path)
	if os.IsNotExist(err) {
		return Inspection{Kind: InspectionKind, ClaimPath: path}, nil
	}
	if err != nil {
		return Inspection{Kind: InspectionKind, Exists: true, ClaimPath: path, ReadError: err.Error()}, err
	}
	stale := staleAfter > 0 && !owner.StartedAt.IsZero() && !now.UTC().Before(owner.StartedAt.Add(staleAfter))
	return Inspection{Kind: InspectionKind, Exists: true, StaleCandidate: stale, ClaimPath: path, Owner: &owner}, nil
}

// Recover is explicit, digest-like by exact claim id, and only removes a stale
// candidate. It appends an audit receipt under .hq/proofs before removing the
// claim and never touches queue, result, session, or output evidence.
func Recover(projectRoot, expectedClaimID, reason string, now time.Time, staleAfter time.Duration) (RecoveryReceipt, error) {
	if strings.TrimSpace(expectedClaimID) == "" {
		return RecoveryReceipt{}, errors.New("expected claim id is required")
	}
	if strings.TrimSpace(reason) == "" {
		return RecoveryReceipt{}, errors.New("recovery reason is required")
	}
	if staleAfter <= 0 {
		return RecoveryReceipt{}, errors.New("stale-after must be positive")
	}
	inspection, err := Inspect(projectRoot, now, staleAfter)
	if err != nil {
		return RecoveryReceipt{}, err
	}
	if !inspection.Exists || inspection.Owner == nil {
		return RecoveryReceipt{}, errors.New("no worker claim exists")
	}
	if inspection.Owner.ClaimID != expectedClaimID {
		return RecoveryReceipt{}, errors.New("claim id mismatch; refusing recovery")
	}
	if !inspection.StaleCandidate {
		return RecoveryReceipt{}, errors.New("claim is not a stale candidate; refusing recovery")
	}
	receipt := RecoveryReceipt{
		Kind: RecoveryKind, ClaimID: inspection.Owner.ClaimID, WorkerID: inspection.Owner.WorkerID,
		Workspace: inspection.Owner.Workspace, RecoveredAt: now.UTC(), Reason: reason,
	}
	layout, err := workersafety.NewLayout(inspection.Owner.Workspace)
	if err != nil {
		return RecoveryReceipt{}, err
	}
	if err := appendJSONL(filepath.Join(layout.ProofsDir, recoveryLogName), receipt); err != nil {
		return RecoveryReceipt{}, err
	}
	current, err := readOwner(inspection.ClaimPath)
	if err != nil {
		return RecoveryReceipt{}, err
	}
	if current.ClaimID != expectedClaimID {
		return RecoveryReceipt{}, errors.New("claim changed during recovery; refusing removal")
	}
	if err := atomicfile.Remove(inspection.ClaimPath); err != nil {
		return RecoveryReceipt{}, fmt.Errorf("remove recovered worker claim: %w", err)
	}
	return receipt, nil
}

func canonicalProjectRoot(projectRoot string) (string, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return "", errors.New("project root is required")
	}
	absolute, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve project root identity: %w", err)
	}
	return filepath.Clean(resolved), nil
}

func readOwner(path string) (Owner, error) {
	data, err := atomicfile.Read(path)
	if err != nil {
		return Owner{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var owner Owner
	if err := decoder.Decode(&owner); err != nil {
		return Owner{}, err
	}
	if owner.Kind != OwnerKind || strings.TrimSpace(owner.ClaimID) == "" || strings.TrimSpace(owner.WorkerID) == "" ||
		strings.TrimSpace(owner.Workspace) == "" || owner.ProcessID <= 0 || owner.StartedAt.IsZero() {
		return Owner{}, errors.New("invalid worker claim owner record")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Owner{}, errors.New("multiple JSON values in worker claim")
		}
		return Owner{}, err
	}
	return owner, nil
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate claim id: %w", err)
	}
	return "claim-" + hex.EncodeToString(value), nil
}

func appendJSONL(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	return file.Sync()
}

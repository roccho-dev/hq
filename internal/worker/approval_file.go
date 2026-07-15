package worker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"hq/internal/atomicfile"
)

const WorkspaceApprovalFilename = "approvals.jsonl"

func WorkspaceApprovalPath(workspaceRoot string) string {
	return filepath.Join(filepath.Clean(workspaceRoot), ".hq", WorkspaceApprovalFilename)
}

func LoadWorkspaceApprovals(workspaceRoot string) (ApprovalStore, error) {
	_, store, err := readWorkspaceApprovalLedger(WorkspaceApprovalPath(workspaceRoot))
	return store, err
}

func MergeApprovalStores(stores ...ApprovalStore) (ApprovalStore, error) {
	merged := EmptyApprovalStore()
	for _, store := range stores {
		for instructionID, record := range store.byInstruction {
			if existing, ok := merged.byInstruction[instructionID]; ok {
				if existing != record {
					return ApprovalStore{}, fmt.Errorf("conflicting approvals for instruction_id %q", instructionID)
				}
				continue
			}
			merged.byInstruction[instructionID] = record
		}
	}
	return merged, nil
}

// AppendWorkspaceApproval adds one exact approval record to the hq-owned
// workspace ledger. The short-lived workspace lock serializes read-check-write
// across processes. Existing rows are never changed: the complete old ledger
// plus at most one new row is published through atomic replacement so readers
// observe either the old complete ledger or the new complete ledger.
func AppendWorkspaceApproval(workspaceRoot string, record ApprovalRecord) (appended bool, err error) {
	if strings.TrimSpace(workspaceRoot) == "" || !filepath.IsAbs(workspaceRoot) {
		return false, errors.New("workspace root must be a non-empty absolute path")
	}
	if err := record.Validate(); err != nil {
		return false, err
	}
	lock, err := acquireWorkspaceApprovalLock(workspaceRoot)
	if err != nil {
		return false, err
	}
	defer func() {
		if releaseErr := lock.release(); releaseErr != nil {
			err = errors.Join(err, fmt.Errorf("release approval lock: %w", releaseErr))
		}
	}()

	path := WorkspaceApprovalPath(workspaceRoot)
	currentBytes, existing, err := readWorkspaceApprovalLedger(path)
	if err != nil {
		return false, err
	}
	if current, ok := existing.byInstruction[record.InstructionID]; ok {
		if current == record {
			return false, nil
		}
		return false, fmt.Errorf("instruction_id %q already has a different approval", record.InstructionID)
	}
	if err := replaceWorkspaceApprovalLedger(path, currentBytes, record); err != nil {
		return false, err
	}
	return true, nil
}

func readWorkspaceApprovalLedger(path string) ([]byte, ApprovalStore, error) {
	file, err := atomicfile.OpenRead(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, EmptyApprovalStore(), nil
	}
	if err != nil {
		return nil, ApprovalStore{}, err
	}
	data, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil {
		return nil, ApprovalStore{}, readErr
	}
	if closeErr != nil {
		return nil, ApprovalStore{}, closeErr
	}
	store, err := LoadApprovals(bytes.NewReader(data))
	if err != nil {
		return nil, ApprovalStore{}, err
	}
	return data, store, nil
}

func replaceWorkspaceApprovalLedger(path string, current []byte, record ApprovalRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".hq-approvals-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	writeErr := error(nil)
	if len(current) != 0 {
		_, writeErr = bytes.NewReader(current).WriteTo(temporary)
		if writeErr == nil && current[len(current)-1] != '\n' {
			_, writeErr = temporary.Write([]byte{'\n'})
		}
	}
	if writeErr == nil {
		_, writeErr = bytes.NewReader(encoded).WriteTo(temporary)
	}
	if writeErr == nil {
		_, writeErr = temporary.Write([]byte{'\n'})
	}
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
	return atomicfile.Replace(temporaryPath, path)
}

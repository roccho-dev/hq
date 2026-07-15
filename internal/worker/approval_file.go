package worker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const WorkspaceApprovalFilename = "approvals.jsonl"

func WorkspaceApprovalPath(workspaceRoot string) string {
	return filepath.Join(filepath.Clean(workspaceRoot), ".hq", WorkspaceApprovalFilename)
}

func LoadWorkspaceApprovals(workspaceRoot string) (ApprovalStore, error) {
	path := WorkspaceApprovalPath(workspaceRoot)
	store, err := LoadApprovalFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return EmptyApprovalStore(), nil
	}
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
// workspace ledger. An identical existing record is an idempotent no-op;
// conflicting reuse of an instruction identity fails closed.
func AppendWorkspaceApproval(workspaceRoot string, record ApprovalRecord) (bool, error) {
	if strings.TrimSpace(workspaceRoot) == "" || !filepath.IsAbs(workspaceRoot) {
		return false, errors.New("workspace root must be a non-empty absolute path")
	}
	if err := record.Validate(); err != nil {
		return false, err
	}
	existing, err := LoadWorkspaceApprovals(workspaceRoot)
	if err != nil {
		return false, err
	}
	if current, ok := existing.byInstruction[record.InstructionID]; ok {
		if current == record {
			return false, nil
		}
		return false, fmt.Errorf("instruction_id %q already has a different approval", record.InstructionID)
	}
	path := WorkspaceApprovalPath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return false, err
	}
	encoded = append(encoded, '\n')
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	if _, err := bytes.NewReader(encoded).WriteTo(file); err != nil {
		_ = file.Close()
		return false, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return false, err
	}
	if err := file.Close(); err != nil {
		return false, err
	}
	return true, nil
}

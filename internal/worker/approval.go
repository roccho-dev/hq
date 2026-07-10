package worker

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"hq/internal/workersafety"
)

const ApprovalVersionV1 = "worker.approval.v1"

type ApprovalRecord struct {
	Version           string `json:"version"`
	InstructionID     string `json:"instruction_id"`
	Approved          bool   `json:"approved"`
	ApprovedBy        string `json:"approved_by"`
	InstructionDigest string `json:"instruction_digest"`
}

type ApprovalStore struct {
	byInstruction map[string]ApprovalRecord
}

func EmptyApprovalStore() ApprovalStore {
	return ApprovalStore{byInstruction: map[string]ApprovalRecord{}}
}

func LoadApprovalFile(path string) (ApprovalStore, error) {
	if strings.TrimSpace(path) == "" {
		return EmptyApprovalStore(), nil
	}
	file, err := os.Open(path)
	if err != nil {
		return ApprovalStore{}, err
	}
	defer file.Close()
	return LoadApprovals(file)
}

func LoadApprovals(r io.Reader) (ApprovalStore, error) {
	store := EmptyApprovalStore()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxJSONLLineBytes)
	line := 0
	for scanner.Scan() {
		line++
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var record ApprovalRecord
		if err := decodeStrict(scanner.Bytes(), &record); err != nil {
			return ApprovalStore{}, fmt.Errorf("approval line %d: %w", line, err)
		}
		if err := record.Validate(); err != nil {
			return ApprovalStore{}, fmt.Errorf("approval line %d: %w", line, err)
		}
		if _, exists := store.byInstruction[record.InstructionID]; exists {
			return ApprovalStore{}, fmt.Errorf("approval line %d: duplicate instruction_id %q", line, record.InstructionID)
		}
		store.byInstruction[record.InstructionID] = record
	}
	if err := scanner.Err(); err != nil {
		return ApprovalStore{}, err
	}
	return store, nil
}

func (r ApprovalRecord) Validate() error {
	if r.Version != ApprovalVersionV1 {
		return fmt.Errorf("version must be %q", ApprovalVersionV1)
	}
	if strings.TrimSpace(r.InstructionID) == "" {
		return errors.New("instruction_id is required")
	}
	if !r.Approved {
		return errors.New("approval record must set approved=true")
	}
	if strings.TrimSpace(r.ApprovedBy) == "" {
		return errors.New("approved_by is required")
	}
	hexDigest := strings.TrimPrefix(r.InstructionDigest, "sha256:")
	if !strings.HasPrefix(r.InstructionDigest, "sha256:") || len(hexDigest) != sha256.Size*2 || hexDigest != strings.ToLower(hexDigest) {
		return errors.New("instruction_digest must be sha256:<64 lowercase hex chars>")
	}
	if _, err := hex.DecodeString(hexDigest); err != nil {
		return errors.New("instruction_digest contains invalid hex")
	}
	return nil
}

func (s ApprovalStore) ApprovalFor(instructionID string) *workersafety.Approval {
	record, exists := s.byInstruction[instructionID]
	if !exists {
		return nil
	}
	return &workersafety.Approval{
		Approved: true, ApprovedBy: record.ApprovedBy, InstructionDigest: record.InstructionDigest,
	}
}

// InstructionDigest binds approval to all canonical instruction.v1 fields.
func InstructionDigest(instruction Instruction) (string, error) {
	encoded, err := json.Marshal(instruction)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

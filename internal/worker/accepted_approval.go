package worker

import (
	"fmt"
	"strings"
)

// ApprovalsForAccepted preserves the existing one-submit execution behavior for
// finite operations. Verified-resource invocations are intentionally omitted:
// their accepted row is queue admission only and requires a separate exact
// worker.approval.v1 record before dispatch.
func ApprovalsForAccepted(rows []ReadRow, approvedBy string) (ApprovalStore, error) {
	if strings.TrimSpace(approvedBy) == "" {
		return ApprovalStore{}, fmt.Errorf("approved_by is required")
	}
	store := EmptyApprovalStore()
	validation := DefaultContract().ValidateRows(rows)
	for i, row := range rows {
		if len(validation[i]) != 0 || IsVerifiedResourceInvocation(row.Instruction) {
			continue
		}
		digest, err := InstructionDigest(row.Instruction)
		if err != nil {
			return ApprovalStore{}, err
		}
		if _, ok := store.byInstruction[row.Instruction.ID]; ok {
			return ApprovalStore{}, fmt.Errorf("duplicate accepted instruction id %q", row.Instruction.ID)
		}
		store.byInstruction[row.Instruction.ID] = ApprovalRecord{Version: ApprovalVersionV1, InstructionID: row.Instruction.ID, Approved: true, ApprovedBy: approvedBy, InstructionDigest: digest}
	}
	return store, nil
}

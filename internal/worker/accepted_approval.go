package worker

import (
	"fmt"
	"strings"
)

// ApprovalsForAccepted turns the explicit accepted.instruction boundary into
// exact digest-bound approval for the managed product path. Durable policy
// evidence still records the decision before dispatch.
func ApprovalsForAccepted(rows []ReadRow, approvedBy string) (ApprovalStore, error) {
	if strings.TrimSpace(approvedBy) == "" { return ApprovalStore{}, fmt.Errorf("approved_by is required") }
	store := EmptyApprovalStore()
	validation := DefaultContract().ValidateRows(rows)
	for index, row := range rows {
		if len(validation[index]) != 0 { continue }
		digest, err := InstructionDigest(row.Instruction)
		if err != nil { return ApprovalStore{}, err }
		if _, exists := store.byInstruction[row.Instruction.ID]; exists { return ApprovalStore{}, fmt.Errorf("duplicate accepted instruction id %q", row.Instruction.ID) }
		store.byInstruction[row.Instruction.ID] = ApprovalRecord{Version: ApprovalVersionV1, InstructionID: row.Instruction.ID, Approved: true, ApprovedBy: approvedBy, InstructionDigest: digest}
	}
	return store, nil
}

package worker

import (
	"path/filepath"
	"strings"
	"testing"
)

func testApprovalRecord() ApprovalRecord {
	return ApprovalRecord{
		Version: ApprovalVersionV1, InstructionID: "ins-resource-001", Approved: true,
		ApprovedBy: "owner@example", InstructionDigest: "sha256:" + strings.Repeat("a", 64),
	}
}

func TestWorkspaceApprovalAppendLoadAndIdempotence(t *testing.T) {
	root := t.TempDir()
	record := testApprovalRecord()
	appended, err := AppendWorkspaceApproval(root, record)
	if err != nil || !appended {
		t.Fatalf("appended=%v err=%v", appended, err)
	}
	if path := WorkspaceApprovalPath(root); path != filepath.Join(root, ".hq", WorkspaceApprovalFilename) {
		t.Fatalf("path=%q", path)
	}
	store, err := LoadWorkspaceApprovals(root)
	if err != nil {
		t.Fatal(err)
	}
	approval := store.ApprovalFor(record.InstructionID)
	if approval == nil || approval.ApprovedBy != record.ApprovedBy || approval.InstructionDigest != record.InstructionDigest {
		t.Fatalf("approval=%+v", approval)
	}
	appended, err = AppendWorkspaceApproval(root, record)
	if err != nil || appended {
		t.Fatalf("idempotent append=%v err=%v", appended, err)
	}
}

func TestWorkspaceApprovalConflictFailsClosed(t *testing.T) {
	root := t.TempDir()
	record := testApprovalRecord()
	if _, err := AppendWorkspaceApproval(root, record); err != nil {
		t.Fatal(err)
	}
	conflict := record
	conflict.ApprovedBy = "other@example"
	if _, err := AppendWorkspaceApproval(root, conflict); err == nil {
		t.Fatal("conflicting approval was accepted")
	}
}

func TestMergeApprovalStoresRejectsConflictingAuthority(t *testing.T) {
	left := EmptyApprovalStore()
	right := EmptyApprovalStore()
	record := testApprovalRecord()
	left.byInstruction[record.InstructionID] = record
	other := record
	other.InstructionDigest = "sha256:" + strings.Repeat("b", 64)
	right.byInstruction[other.InstructionID] = other
	if _, err := MergeApprovalStores(left, right); err == nil {
		t.Fatal("conflicting stores merged")
	}
}

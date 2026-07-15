package worker

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestIdenticalWorkspaceApprovalIsIdempotentAcross32ConcurrentWriters(t *testing.T) {
	root := t.TempDir()
	record := testApprovalRecord()
	results := runConcurrentApprovalAppendsAt(t, root, 32, func(int) ApprovalRecord { return record })
	appended := 0
	for _, result := range results {
		if result.err != nil {
			t.Fatalf("identical concurrent approval failed: %v", result.err)
		}
		if result.appended {
			appended++
		}
	}
	if appended != 1 {
		t.Fatalf("appended=%d want=1", appended)
	}
	assertReadableApprovalLedger(t, root, record.InstructionID, 1)
}

func TestDistinctWorkspaceApprovalsConflictAcross32ConcurrentWriters(t *testing.T) {
	root := t.TempDir()
	record := testApprovalRecord()
	results := runConcurrentApprovalAppendsAt(t, root, 32, func(index int) ApprovalRecord {
		candidate := record
		candidate.ApprovedBy = fmt.Sprintf("owner-%02d@example", index)
		return candidate
	})
	succeeded := 0
	conflicted := 0
	for _, result := range results {
		switch {
		case result.err == nil && result.appended:
			succeeded++
		case result.err != nil && strings.Contains(result.err.Error(), "already has a different approval"):
			conflicted++
		default:
			t.Fatalf("unexpected concurrent result: appended=%v err=%v", result.appended, result.err)
		}
	}
	if succeeded != 1 || conflicted != 31 {
		t.Fatalf("succeeded=%d conflicted=%d", succeeded, conflicted)
	}
	assertReadableApprovalLedger(t, root, record.InstructionID, 1)
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

type approvalAppendResult struct {
	appended bool
	err      error
}

func runConcurrentApprovalAppendsAt(t *testing.T, root string, count int, record func(int) ApprovalRecord) []approvalAppendResult {
	t.Helper()
	start := make(chan struct{})
	results := make(chan approvalAppendResult, count)
	var ready sync.WaitGroup
	ready.Add(count)
	for index := 0; index < count; index++ {
		go func(index int) {
			ready.Done()
			<-start
			appended, err := AppendWorkspaceApproval(root, record(index))
			results <- approvalAppendResult{appended: appended, err: err}
		}(index)
	}
	ready.Wait()
	close(start)
	out := make([]approvalAppendResult, 0, count)
	for index := 0; index < count; index++ {
		out = append(out, <-results)
	}
	return out
}

func assertReadableApprovalLedger(t *testing.T, root, instructionID string, wantLines int) {
	t.Helper()
	store, err := LoadWorkspaceApprovals(root)
	if err != nil {
		t.Fatalf("approval ledger is unreadable: %v", err)
	}
	if store.ApprovalFor(instructionID) == nil {
		t.Fatalf("approval %q is missing", instructionID)
	}
	data, err := os.ReadFile(WorkspaceApprovalPath(root))
	if err != nil {
		t.Fatal(err)
	}
	trimmed := bytes.TrimSpace(data)
	lines := 0
	if len(trimmed) != 0 {
		lines = bytes.Count(trimmed, []byte{'\n'}) + 1
	}
	if lines != wantLines {
		t.Fatalf("approval ledger lines=%d want=%d data=%q", lines, wantLines, data)
	}
}

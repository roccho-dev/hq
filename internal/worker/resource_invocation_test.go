package worker

import (
	"context"
	"strings"
	"testing"

	"hq/internal/worker/adapter"
	"hq/internal/workersafety"
)

func resourceInvocationRow(t *testing.T) ReadRow {
	t.Helper()
	raw := `{"id":"ins-resource-001","version":"instruction.v1","op":"run","target":"local-tool","payload":{"tool_id":"aws","tool_version":"2.35.11","policy_version":"aws-restricted.v1","argv":["sts","get-caller-identity","--output","json"]},"created_at":"2026-07-15T07:00:00Z"}`
	rows, err := ReadInstructions("queue.jsonl", strings.NewReader(raw))
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	return rows[0]
}

func TestResourceInvocationIsHeldThenResumesSameRunAfterExactApproval(t *testing.T) {
	row := resourceInvocationRow(t)
	fake := &runnerFakeAdapter{}
	registry, err := adapter.NewRegistry(adapter.Registration{Target: "local-tool", Adapter: fake})
	if err != nil {
		t.Fatal(err)
	}

	first := NewRunner(t.TempDir(), registry, EmptyApprovalStore())
	firstSink := &memoryAppender{}
	firstEntries, unsuccessful, err := first.Process(context.Background(), []ReadRow{row}, LogData{}, false, firstSink)
	if err != nil {
		t.Fatal(err)
	}
	if unsuccessful != 0 || fake.calls != 0 {
		t.Fatalf("held invocation was treated as failure or dispatched: unsuccessful=%d calls=%d", unsuccessful, fake.calls)
	}
	if len(firstEntries) != 2 || firstEntries[0].Policy == nil || firstEntries[0].Policy.Status != workersafety.PolicyApprovalRequired || firstEntries[1].Result == nil || firstEntries[1].Result.Kind != ResultAccepted {
		t.Fatalf("held lifecycle=%+v", firstEntries)
	}
	acceptedRunID := firstEntries[1].Result.RunID
	prior := entriesAsLogData(firstEntries)

	poll := NewRunner(t.TempDir(), registry, EmptyApprovalStore())
	pollSink := &memoryAppender{}
	pollEntries, unsuccessful, err := poll.Process(context.Background(), []ReadRow{row}, prior, false, pollSink)
	if err != nil {
		t.Fatal(err)
	}
	if unsuccessful != 0 || fake.calls != 0 || len(pollEntries) != 0 {
		t.Fatalf("unchanged held polling appended or dispatched: entries=%+v unsuccessful=%d calls=%d", pollEntries, unsuccessful, fake.calls)
	}

	approved := NewRunner(t.TempDir(), registry, approvedRunnerFixture(t, row.Instruction))
	approvedSink := &memoryAppender{}
	approvedEntries, unsuccessful, err := approved.Process(context.Background(), []ReadRow{row}, prior, false, approvedSink)
	if err != nil {
		t.Fatal(err)
	}
	if unsuccessful != 0 || fake.calls != 1 {
		t.Fatalf("approved invocation did not dispatch exactly once: unsuccessful=%d calls=%d", unsuccessful, fake.calls)
	}
	if len(approvedEntries) != 4 || approvedEntries[0].Policy == nil || approvedEntries[0].Policy.Status != workersafety.PolicyAllowed || approvedEntries[1].Result == nil || approvedEntries[1].Result.Kind != ResultStarted || approvedEntries[3].Result == nil || approvedEntries[3].Result.Kind != ResultCompleted {
		t.Fatalf("approved lifecycle=%+v", approvedEntries)
	}
	if approvedEntries[1].Result.RunID != acceptedRunID {
		t.Fatalf("approval created a different run: accepted=%s started=%s", acceptedRunID, approvedEntries[1].Result.RunID)
	}
}

func TestResourceInvocationStaleApprovalRemainsTerminallyBlocked(t *testing.T) {
	row := resourceInvocationRow(t)
	record := `{"version":"worker.approval.v1","instruction_id":"ins-resource-001","approved":true,"approved_by":"owner","instruction_digest":"sha256:` + strings.Repeat("0", 64) + `"}`
	store, err := LoadApprovals(strings.NewReader(record))
	if err != nil {
		t.Fatal(err)
	}
	fake := &runnerFakeAdapter{}
	registry, err := adapter.NewRegistry(adapter.Registration{Target: "local-tool", Adapter: fake})
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(t.TempDir(), registry, store)
	sink := &memoryAppender{}
	entries, unsuccessful, err := runner.Process(context.Background(), []ReadRow{row}, LogData{}, false, sink)
	if err != nil {
		t.Fatal(err)
	}
	if unsuccessful != 1 || fake.calls != 0 || len(entries) != 3 || entries[2].Result == nil || entries[2].Result.Error == nil || entries[2].Result.Error.Code != "approval_digest_mismatch" {
		t.Fatalf("entries=%+v unsuccessful=%d calls=%d", entries, unsuccessful, fake.calls)
	}
}

func TestResourceInvocationPayloadUnionFailsClosed(t *testing.T) {
	base := `{"id":"ins-union-001","version":"instruction.v1","op":"run","target":"local-tool","payload":%s,"created_at":"2026-07-15T07:00:00Z"}`
	for name, payload := range map[string]string{
		"neither": `{"tool_id":"aws","tool_version":"2.35.11"}`,
		"both": `{"tool_id":"aws","tool_version":"2.35.11","action_id":"x","input":{},"policy_version":"v1","argv":["sts"]}`,
		"missing argv": `{"tool_id":"aws","tool_version":"2.35.11","policy_version":"v1"}`,
		"empty arg": `{"tool_id":"aws","tool_version":"2.35.11","policy_version":"v1","argv":[""]}`,
	} {
		t.Run(name, func(t *testing.T) {
			rows, err := ReadInstructions("queue.jsonl", strings.NewReader(sprintf(base, payload)))
			if err != nil || len(rows) != 1 {
				t.Fatalf("rows=%+v err=%v", rows, err)
			}
			if diagnostics := DefaultContract().Validate(rows[0]); len(diagnostics) == 0 {
				t.Fatalf("invalid payload accepted: %s", payload)
			}
		})
	}
}

func entriesAsLogData(entries []LogEntry) LogData {
	var data LogData
	for _, entry := range entries {
		if entry.Policy != nil {
			data.Policies = append(data.Policies, *entry.Policy)
		}
		if entry.Result != nil {
			data.Results = append(data.Results, *entry.Result)
		}
		if entry.Validation != nil {
			data.Validations = append(data.Validations, *entry.Validation)
		}
	}
	return data
}

func sprintf(format string, value string) string {
	return strings.Replace(format, "%s", value, 1)
}

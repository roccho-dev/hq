package worker

import (
	"strings"
	"testing"
)

func TestSubmitDerivedApprovalExcludesVerifiedResourceInvocation(t *testing.T) {
	input := strings.Join([]string{
		`{"id":"ins-host-001","version":"instruction.v1","op":"run","target":"host","payload":{"capability":"host.open","path":"/tmp"},"created_at":"2026-07-15T07:00:00Z"}`,
		`{"id":"ins-resource-001","version":"instruction.v1","op":"run","target":"local-tool","payload":{"tool_id":"aws","tool_version":"2.35.11","policy_version":"aws-restricted.v1","argv":["sts","get-caller-identity"]},"created_at":"2026-07-15T07:00:01Z"}`,
	}, "\n")
	rows, err := ReadInstructions("accepted.jsonl", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	store, err := ApprovalsForAccepted(rows, "hq.submit:dep-test")
	if err != nil {
		t.Fatal(err)
	}
	if store.ApprovalFor("ins-host-001") == nil {
		t.Fatal("finite operation lost submit-derived approval")
	}
	if store.ApprovalFor("ins-resource-001") != nil {
		t.Fatal("resource invocation received submit-derived execution authority")
	}
}

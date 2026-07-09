package workersafety

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type policyFixture struct {
	Name    string        `json:"name"`
	Request PolicyRequest `json:"request"`
	Want    struct {
		Status      PolicyStatus `json:"status"`
		Reason      string       `json:"reason"`
		MayDispatch bool         `json:"may_dispatch"`
	} `json:"want"`
}

func basePolicyRequest() PolicyRequest {
	return PolicyRequest{
		InstructionID:     "instruction-001",
		RunID:             "run-001",
		Target:            "sh",
		Operation:         "read.file",
		InstructionDigest: "sha256:request",
		Risk:              RiskSafe,
	}
}

func TestEvaluatePolicyFixtureMatrix(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "..", "spec", "fixtures", "worker-safety", "policy.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count++
		var fixture policyFixture
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatalf("fixture line %d: %v", count, err)
		}
		t.Run(fixture.Name, func(t *testing.T) {
			decision := EvaluatePolicy(fixture.Request)
			if decision.Status != fixture.Want.Status || decision.MayDispatch != fixture.Want.MayDispatch || decision.Reason != fixture.Want.Reason {
				t.Fatalf("decision = %#v", decision)
			}
			if !decision.EvidenceOnly || decision.Kind != PolicyEventKind {
				t.Fatalf("decision must be evidence-only JSONL event: %#v", decision)
			}
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("policy fixture must contain cases")
	}
}

func TestPolicyDecisionIsJSONLReadyAndCarriesApprovalActor(t *testing.T) {
	request := basePolicyRequest()
	request.Risk = RiskDangerous
	request.Policy.RequireApproval = true
	request.Approval = &Approval{Approved: true, ApprovedBy: "owner", InstructionDigest: request.InstructionDigest}
	decision := EvaluatePolicy(request)

	encoded, err := json.Marshal(decision)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip map[string]any
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip["kind"] != PolicyEventKind || roundTrip["status"] != string(PolicyAllowed) {
		t.Fatalf("unexpected event: %s", encoded)
	}
	if roundTrip["approved_by"] != "owner" || roundTrip["instruction_digest"] != request.InstructionDigest || roundTrip["may_dispatch"] != true {
		t.Fatalf("approval evidence missing: %s", encoded)
	}
}

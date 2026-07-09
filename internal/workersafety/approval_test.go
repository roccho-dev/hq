package workersafety

import (
	"encoding/json"
	"testing"
)

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

func TestEvaluatePolicyFailClosedMatrix(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*PolicyRequest)
		status   PolicyStatus
		dispatch bool
		reason   string
	}{
		{
			name: "safe auto run",
			mutate: func(request *PolicyRequest) {
				request.Policy.AllowAuto = true
			},
			status: PolicyAllowed, dispatch: true, reason: "safe_auto_allowed",
		},
		{
			name:   "safe default deny",
			mutate: func(request *PolicyRequest) {},
			status: PolicyBlocked, dispatch: false, reason: "default_deny",
		},
		{
			name: "dangerous cannot auto run",
			mutate: func(request *PolicyRequest) {
				request.Risk = RiskDangerous
				request.Operation = "delete.workspace"
				request.Policy.AllowAuto = true
			},
			status: PolicyBlocked, dispatch: false, reason: "dangerous_requires_approval",
		},
		{
			name: "dangerous waits for approval",
			mutate: func(request *PolicyRequest) {
				request.Risk = RiskDangerous
				request.Operation = "delete.workspace"
				request.Policy.RequireApproval = true
			},
			status: PolicyApprovalRequired, dispatch: false, reason: "approval_required",
		},
		{
			name: "dangerous exact approval",
			mutate: func(request *PolicyRequest) {
				request.Risk = RiskDangerous
				request.Operation = "delete.workspace"
				request.Policy.RequireApproval = true
				request.Approval = &Approval{Approved: true, ApprovedBy: "owner", InstructionDigest: request.InstructionDigest}
			},
			status: PolicyAllowed, dispatch: true, reason: "explicit_approval",
		},
		{
			name: "stale approval blocked",
			mutate: func(request *PolicyRequest) {
				request.Risk = RiskDangerous
				request.Operation = "delete.workspace"
				request.Policy.RequireApproval = true
				request.Approval = &Approval{Approved: true, ApprovedBy: "owner", InstructionDigest: "sha256:old"}
			},
			status: PolicyBlocked, dispatch: false, reason: "approval_digest_mismatch",
		},
		{
			name: "policy conflict blocked",
			mutate: func(request *PolicyRequest) {
				request.Policy.AllowAuto = true
				request.Policy.RequireApproval = true
			},
			status: PolicyBlocked, dispatch: false, reason: "policy_conflict",
		},
		{
			name: "explicit block",
			mutate: func(request *PolicyRequest) {
				request.Policy.Block = true
			},
			status: PolicyBlocked, dispatch: false, reason: "policy_blocked",
		},
		{
			name: "unknown risk blocked",
			mutate: func(request *PolicyRequest) {
				request.Risk = Risk("unknown")
				request.Policy.AllowAuto = true
			},
			status: PolicyBlocked, dispatch: false, reason: "unknown_risk",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := basePolicyRequest()
			test.mutate(&request)
			decision := EvaluatePolicy(request)
			if decision.Status != test.status || decision.MayDispatch != test.dispatch || decision.Reason != test.reason {
				t.Fatalf("decision = %#v", decision)
			}
			if !decision.EvidenceOnly || decision.Kind != PolicyEventKind {
				t.Fatalf("decision must be evidence-only JSONL event: %#v", decision)
			}
		})
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

func TestInvalidRequestCannotDispatch(t *testing.T) {
	for _, mutate := range []func(*PolicyRequest){
		func(request *PolicyRequest) { request.RunID = "" },
		func(request *PolicyRequest) { request.InstructionDigest = "" },
	} {
		request := basePolicyRequest()
		request.Policy.AllowAuto = true
		mutate(&request)
		decision := EvaluatePolicy(request)
		if decision.MayDispatch || decision.Status != PolicyBlocked || decision.Reason != "invalid_request" {
			t.Fatalf("invalid request was not blocked: %#v", decision)
		}
	}
}

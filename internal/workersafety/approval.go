package workersafety

import (
	"strings"
	"unicode"
)

const PolicyEventKind = "worker.policy.v1"

type Risk string

const (
	RiskSafe      Risk = "safe"
	RiskDangerous Risk = "dangerous"
)

type PolicyStatus string

const (
	PolicyAllowed          PolicyStatus = "allowed"
	PolicyApprovalRequired PolicyStatus = "approval_required"
	PolicyBlocked          PolicyStatus = "blocked"
)

// ExecutionPolicy is explicit. Zero value means default deny.
type ExecutionPolicy struct {
	AllowAuto       bool `json:"allow_auto"`
	RequireApproval bool `json:"require_approval"`
	Block           bool `json:"block,omitempty"`
}

// Approval is bound to one exact instruction digest so stale approval cannot
// authorize a changed request.
type Approval struct {
	Approved          bool   `json:"approved"`
	ApprovedBy        string `json:"approved_by"`
	InstructionDigest string `json:"instruction_digest"`
}

type PolicyRequest struct {
	InstructionID     string          `json:"instruction_id"`
	RunID             string          `json:"run_id"`
	Target            string          `json:"target"`
	Operation         string          `json:"operation"`
	InstructionDigest string          `json:"instruction_digest"`
	Risk              Risk            `json:"risk"`
	Policy            ExecutionPolicy `json:"policy"`
	Approval          *Approval       `json:"approval,omitempty"`
}

// PolicyDecision is an evidence-only JSONL-ready event. Adapter dispatch is
// permitted only when MayDispatch is true.
type PolicyDecision struct {
	Kind              string       `json:"kind"`
	InstructionID     string       `json:"instruction_id"`
	InstructionDigest string       `json:"instruction_digest"`
	RunID             string       `json:"run_id"`
	Target            string       `json:"target"`
	Operation         string       `json:"operation"`
	Risk              Risk         `json:"risk"`
	Status            PolicyStatus `json:"status"`
	Reason            string       `json:"reason"`
	MayDispatch       bool         `json:"may_dispatch"`
	ApprovedBy        string       `json:"approved_by,omitempty"`
	EvidenceOnly      bool         `json:"evidence_only"`
}

// EvaluatePolicy is fail-closed and performs no adapter or filesystem effect.
func EvaluatePolicy(request PolicyRequest) PolicyDecision {
	decision := PolicyDecision{
		Kind:              PolicyEventKind,
		InstructionID:     request.InstructionID,
		InstructionDigest: request.InstructionDigest,
		RunID:             request.RunID,
		Target:            request.Target,
		Operation:         request.Operation,
		Risk:              request.Risk,
		Status:            PolicyBlocked,
		Reason:            "default_deny",
		MayDispatch:       false,
		EvidenceOnly:      true,
	}

	if strings.TrimSpace(request.InstructionID) == "" || strings.TrimSpace(request.RunID) == "" ||
		strings.TrimSpace(request.Target) == "" || strings.TrimSpace(request.Operation) == "" ||
		strings.TrimSpace(request.InstructionDigest) == "" {
		decision.Reason = "invalid_request"
		return decision
	}
	if request.Risk != RiskSafe && request.Risk != RiskDangerous {
		decision.Reason = "unknown_risk"
		return decision
	}
	decision.Risk = effectiveRisk(request.Target, request.Operation, request.Risk)

	modes := 0
	if request.Policy.AllowAuto {
		modes++
	}
	if request.Policy.RequireApproval {
		modes++
	}
	if request.Policy.Block {
		modes++
	}
	if modes > 1 {
		decision.Reason = "policy_conflict"
		return decision
	}
	if request.Policy.Block {
		decision.Reason = "policy_blocked"
		return decision
	}

	if decision.Risk == RiskDangerous && !request.Policy.RequireApproval {
		decision.Reason = "dangerous_requires_approval"
		return decision
	}

	if request.Policy.RequireApproval {
		return evaluateApproval(request, decision)
	}
	if request.Policy.AllowAuto && decision.Risk == RiskSafe {
		decision.Status = PolicyAllowed
		decision.Reason = "safe_auto_allowed"
		decision.MayDispatch = true
		return decision
	}

	return decision
}

func evaluateApproval(request PolicyRequest, decision PolicyDecision) PolicyDecision {
	approval := request.Approval
	if approval == nil || !approval.Approved || strings.TrimSpace(approval.ApprovedBy) == "" {
		decision.Status = PolicyApprovalRequired
		decision.Reason = "approval_required"
		return decision
	}
	if strings.TrimSpace(approval.InstructionDigest) == "" || approval.InstructionDigest != request.InstructionDigest {
		decision.Status = PolicyBlocked
		decision.Reason = "approval_digest_mismatch"
		return decision
	}

	decision.Status = PolicyAllowed
	decision.Reason = "explicit_approval"
	decision.MayDispatch = true
	decision.ApprovedBy = approval.ApprovedBy
	return decision
}

var safeOperationVerbs = map[string]struct{}{
	"complete": {}, "draft": {}, "inspect": {}, "list": {}, "preview": {},
	"query": {}, "read": {}, "show": {}, "status": {}, "tail": {}, "validate": {},
}

var dangerousTerms = map[string]struct{}{
	"delete": {}, "destroy": {}, "drop": {}, "erase": {}, "force": {}, "kill": {},
	"overwrite": {}, "prod": {}, "production": {}, "purge": {}, "remove": {}, "revoke": {},
	"root": {}, "shutdown": {}, "system": {}, "truncate": {}, "wipe": {},
}

// effectiveRisk allows a caller to escalate risk but never to downgrade an
// obviously destructive or unknown operation to safe. Unknown verbs are
// dangerous by default; adding a new auto-safe verb therefore requires review.
func effectiveRisk(target, operation string, claimed Risk) Risk {
	if claimed == RiskDangerous {
		return RiskDangerous
	}
	for _, token := range policyTokens(target + " " + operation) {
		if _, dangerous := dangerousTerms[token]; dangerous {
			return RiskDangerous
		}
	}
	operationTokens := policyTokens(operation)
	if len(operationTokens) == 0 {
		return RiskDangerous
	}
	if _, safe := safeOperationVerbs[operationTokens[0]]; safe {
		return RiskSafe
	}
	return RiskDangerous
}

func policyTokens(value string) []string {
	return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

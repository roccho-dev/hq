package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"hq/internal/workersafety"
)

func RedactLogEntry(entry LogEntry) (LogEntry, error) {
	count := 0
	if entry.Validation != nil {
		count++
	}
	if entry.Result != nil {
		count++
	}
	if entry.Policy != nil {
		count++
	}
	if count != 1 {
		return LogEntry{}, errors.New("log entry must contain exactly one row")
	}
	if entry.Validation != nil {
		var row ValidationRow
		if err := redactTyped(entry.Validation, &row); err != nil {
			return LogEntry{}, err
		}
		if err := row.Validate(); err != nil {
			return LogEntry{}, err
		}
		return ValidationEntry(row), nil
	}
	if entry.Result != nil {
		var row ResultRow
		if err := redactTyped(entry.Result, &row); err != nil {
			return LogEntry{}, err
		}
		if err := row.Validate(); err != nil {
			return LogEntry{}, err
		}
		return ResultEntry(row), nil
	}
	var decision workersafety.PolicyDecision
	if err := redactTyped(entry.Policy, &decision); err != nil {
		return LogEntry{}, err
	}
	if err := validatePolicyDecision(decision); err != nil {
		return LogEntry{}, err
	}
	return PolicyEntry(decision), nil
}

func redactTyped(input, output any) error {
	encoded, err := json.Marshal(input)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return err
	}
	redacted, _ := workersafety.RedactValue(value)
	encoded, err = json.Marshal(redacted)
	if err != nil {
		return err
	}
	return decodeStrict(encoded, output)
}

func validatePolicyDecision(decision workersafety.PolicyDecision) error {
	if decision.Kind != workersafety.PolicyEventKind {
		return fmt.Errorf("policy kind must be %q", workersafety.PolicyEventKind)
	}
	if strings.TrimSpace(decision.InstructionID) == "" || strings.TrimSpace(decision.RunID) == "" ||
		strings.TrimSpace(decision.Target) == "" || strings.TrimSpace(decision.Operation) == "" ||
		strings.TrimSpace(decision.InstructionDigest) == "" || strings.TrimSpace(decision.Reason) == "" {
		return errors.New("policy decision identity, digest, and reason are required")
	}
	if !decision.EvidenceOnly {
		return errors.New("policy decision must be evidence-only")
	}
	if decision.Status != workersafety.PolicyAllowed && decision.Status != workersafety.PolicyApprovalRequired && decision.Status != workersafety.PolicyBlocked {
		return errors.New("unknown policy status")
	}
	if decision.MayDispatch != (decision.Status == workersafety.PolicyAllowed) {
		return errors.New("may_dispatch must match allowed policy status")
	}
	return nil
}

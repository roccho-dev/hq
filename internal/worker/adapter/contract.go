// Package adapter defines the transient boundary between worker core and
// target-specific executors. It defines no durable event row.
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Adapter executes one already-validated request. It may stream transient
// Output values and return one transient Completion or an error.
type Adapter interface {
	Run(context.Context, Request, Emit) (Completion, error)
}

type Emit func(Output) error

type Request struct {
	RunID         string
	InstructionID string
	Target        string
	Operation     string
	Payload       json.RawMessage
	CWD           string
}

var canonicalTargets = map[string]struct{}{
	"sh": {}, "herdr": {}, "codex": {}, "claude": {},
}

func IsCanonicalTarget(target string) bool {
	_, ok := canonicalTargets[target]
	return ok
}

func (r Request) Validate() error {
	if strings.TrimSpace(r.RunID) == "" {
		return errors.New("run_id is required")
	}
	if strings.TrimSpace(r.InstructionID) == "" {
		return errors.New("instruction_id is required")
	}
	if !IsCanonicalTarget(r.Target) {
		return fmt.Errorf("target %q is not part of instruction.v1", r.Target)
	}
	if strings.TrimSpace(r.Operation) == "" {
		return errors.New("operation is required")
	}
	if len(r.Payload) == 0 || !json.Valid(r.Payload) {
		return errors.New("payload must be valid JSON")
	}
	return nil
}

type OutputKind string

const (
	OutputStdout OutputKind = "stdout"
	OutputStderr OutputKind = "stderr"
)

// Output is transient stream data. Identity, ordering, time, lifecycle kind,
// and status are deliberately absent and therefore cannot be forged by an adapter.
type Output struct {
	Kind            OutputKind
	Message         string
	NativeSessionID *string
}

func (o Output) Validate() error {
	if o.Kind != OutputStdout && o.Kind != OutputStderr {
		return fmt.Errorf("unknown output kind %q", o.Kind)
	}
	return validateNativeSessionID(o.NativeSessionID)
}

// Completion is transient terminal data. A successful completion must carry a
// final text and/or path so result.v1 cannot claim success with no answer.
type Completion struct {
	FinalText       string
	FinalPath       string
	NativeSessionID *string
}

func (c Completion) Validate() error {
	if strings.TrimSpace(c.FinalText) == "" && strings.TrimSpace(c.FinalPath) == "" {
		return errors.New("completion requires final text and/or path")
	}
	return validateNativeSessionID(c.NativeSessionID)
}

func validateNativeSessionID(value *string) error {
	if value != nil && strings.TrimSpace(*value) == "" {
		return errors.New("native_session_id cannot be empty")
	}
	return nil
}

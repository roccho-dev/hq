package worker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

var canonicalTargets = map[string]struct{}{"sh": {}, "herdr": {}, "codex": {}, "claude": {}}

func (r ResultRow) Validate() error {
	if strings.TrimSpace(r.EventID) == "" {
		return errors.New("event_id is required")
	}
	if r.Version != ResultVersionV1 {
		return fmt.Errorf("version must be %q", ResultVersionV1)
	}
	if strings.TrimSpace(r.RunID) == "" || strings.TrimSpace(r.InstructionID) == "" {
		return errors.New("run_id and instruction_id are required")
	}
	if _, ok := canonicalTargets[r.Target]; !ok {
		return fmt.Errorf("unknown target %q", r.Target)
	}
	if r.Seq < 0 {
		return errors.New("seq must be non-negative")
	}
	if r.RecordedAt.IsZero() {
		return errors.New("recorded_at is required")
	}
	_, offset := r.RecordedAt.Zone()
	if offset != 0 {
		return errors.New("recorded_at must be UTC")
	}
	if r.NativeSessionID != nil && strings.TrimSpace(*r.NativeSessionID) == "" {
		return errors.New("native_session_id cannot be empty")
	}
	switch r.Kind {
	case ResultAccepted, ResultStarted:
		if r.Message != nil || r.Final != nil || r.Error != nil {
			return fmt.Errorf("%s cannot carry message, final, or error", r.Kind)
		}
	case ResultStdout, ResultStderr:
		if r.Message == nil || r.Final != nil || r.Error != nil {
			return fmt.Errorf("%s requires message and forbids final/error", r.Kind)
		}
	case ResultCompleted:
		if r.Message != nil || r.Error != nil || r.Final == nil || (strings.TrimSpace(r.Final.Text) == "" && strings.TrimSpace(r.Final.Path) == "") {
			return errors.New("completed requires non-empty final text and/or path")
		}
	case ResultFailed, ResultBlocked, ResultTimeout, ResultCancelled:
		if r.Message != nil || r.Final != nil || r.Error == nil {
			return fmt.Errorf("%s requires error and forbids message/final", r.Kind)
		}
		if err := r.Error.Validate(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown result kind %q", r.Kind)
	}
	return nil
}

func (e ResultError) Validate() error {
	if strings.TrimSpace(e.Code) == "" || strings.TrimSpace(e.Message) == "" {
		return errors.New("error code and message are required")
	}
	return nil
}

func (d DecisionRow) Validate() error {
	if strings.TrimSpace(d.DecisionID) == "" || d.Version != DecisionVersionV1 || d.RecordedAt.IsZero() {
		return errors.New("decision_id, worker.decision.v1, and recorded_at are required")
	}
	if d.Decision != DecisionAccepted && d.Decision != DecisionBlocked && d.Decision != DecisionDuplicate {
		return fmt.Errorf("unknown decision %q", d.Decision)
	}
	if strings.TrimSpace(d.Source.Path) == "" || d.Source.Line <= 0 || d.Source.Raw == "" {
		return errors.New("decision source path, line, and raw row are required")
	}
	if strings.TrimSpace(d.Code) == "" || strings.TrimSpace(d.Message) == "" {
		return errors.New("decision code and message are required")
	}
	return nil
}

func decodeStrict(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values in one row")
		}
		return err
	}
	return nil
}

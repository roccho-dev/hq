package worker

import (
	"context"
	"errors"
	"strings"
	"time"

	"hq/internal/worker/adapter"
)

// AdapterEnvelope is worker-owned metadata. A target adapter cannot choose
// result identity, order, time, lifecycle kind, or status.
type AdapterEnvelope struct {
	EventID    string
	Seq        int
	RecordedAt time.Time
}

func (e AdapterEnvelope) validate() error {
	if strings.TrimSpace(e.EventID) == "" {
		return errors.New("event_id is required")
	}
	if e.Seq < 0 {
		return errors.New("seq must be non-negative")
	}
	if e.RecordedAt.IsZero() {
		return errors.New("recorded_at is required")
	}
	_, offset := e.RecordedAt.Zone()
	if offset != 0 {
		return errors.New("recorded_at must be UTC")
	}
	return nil
}

func ResultForAdapterOutput(request adapter.Request, envelope AdapterEnvelope, output adapter.Output) (ResultRow, error) {
	if err := validateAdapterMappingInput(request, envelope); err != nil {
		return ResultRow{}, err
	}
	if err := output.Validate(); err != nil {
		return ResultRow{}, err
	}
	message := output.Message
	row := newAdapterResultRow(request, envelope)
	row.Message = &message
	row.NativeSessionID = output.NativeSessionID
	if output.Kind == adapter.OutputStdout {
		row.Kind = ResultStdout
	} else {
		row.Kind = ResultStderr
	}
	return row, row.Validate()
}

func ResultForAdapterCompletion(request adapter.Request, envelope AdapterEnvelope, completion adapter.Completion) (ResultRow, error) {
	if err := validateAdapterMappingInput(request, envelope); err != nil {
		return ResultRow{}, err
	}
	if err := completion.Validate(); err != nil {
		return ResultRow{}, err
	}
	row := newAdapterResultRow(request, envelope)
	row.Kind = ResultCompleted
	row.Final = &FinalResult{Text: completion.FinalText, Path: completion.FinalPath}
	row.NativeSessionID = completion.NativeSessionID
	return row, row.Validate()
}

func ResultForAdapterError(request adapter.Request, envelope AdapterEnvelope, nativeSessionID *string, source error) (ResultRow, error) {
	if source == nil {
		return ResultRow{}, errors.New("adapter error is required")
	}
	if err := validateAdapterMappingInput(request, envelope); err != nil {
		return ResultRow{}, err
	}
	if nativeSessionID != nil && strings.TrimSpace(*nativeSessionID) == "" {
		return ResultRow{}, errors.New("native_session_id cannot be empty")
	}
	kind, detail := mapAdapterError(source)
	row := newAdapterResultRow(request, envelope)
	row.Kind = kind
	row.Error = detail
	row.NativeSessionID = nativeSessionID
	return row, row.Validate()
}

func validateAdapterMappingInput(request adapter.Request, envelope AdapterEnvelope) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return envelope.validate()
}

func newAdapterResultRow(request adapter.Request, envelope AdapterEnvelope) ResultRow {
	return ResultRow{
		EventID:       envelope.EventID,
		Version:       ResultVersionV1,
		RunID:         request.RunID,
		InstructionID: request.InstructionID,
		Target:        request.Target,
		Seq:           envelope.Seq,
		RecordedAt:    envelope.RecordedAt,
	}
}

func mapAdapterError(source error) (string, *ResultError) {
	retryable := false
	if errors.Is(source, context.DeadlineExceeded) {
		retryable = true
		return ResultTimeout, &ResultError{Code: "deadline_exceeded", Message: "run exceeded its time bound", Retryable: &retryable}
	}
	if errors.Is(source, context.Canceled) {
		retryable = true
		return ResultCancelled, &ResultError{Code: "cancel_requested", Message: "run was cancelled", Retryable: &retryable}
	}
	var unknown *adapter.UnknownTargetError
	if errors.As(source, &unknown) {
		return ResultBlocked, &ResultError{Code: "unknown_target", Message: unknown.Error(), Retryable: &retryable}
	}
	var unavailable *adapter.AdapterUnavailableError
	if errors.As(source, &unavailable) {
		return ResultBlocked, &ResultError{Code: "adapter_unavailable", Message: unavailable.Error(), Retryable: &retryable}
	}
	var structured *adapter.FailureError
	if errors.As(source, &structured) {
		if structured.Validate() != nil {
			return ResultFailed, &ResultError{Code: "protocol_error", Message: "adapter returned invalid structured failure", Retryable: &retryable}
		}
		retryable = structured.Retryable
		kind := ResultFailed
		if structured.Class == adapter.FailureBlocked {
			kind = ResultBlocked
		}
		return kind, &ResultError{Code: structured.Code, Message: structured.Message, Retryable: &retryable}
	}
	message := strings.TrimSpace(source.Error())
	if message == "" {
		message = "adapter failed without an error message"
	}
	return ResultFailed, &ResultError{Code: "adapter_error", Message: message, Retryable: &retryable}
}

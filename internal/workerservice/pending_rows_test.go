package workerservice

import (
	"testing"

	"hq/internal/worker"
)

func TestPendingRowsTreatsValidationAsTerminalForEveryInvalidShape(t *testing.T) {
	malformed := worker.Diagnostic{Code: "malformed_json", Message: "bad row"}
	rows := []worker.ReadRow{
		{Source: worker.SourceRef{Line: 1}, Instruction: worker.Instruction{ID: "structurally-invalid"}},
		{Source: worker.SourceRef{Line: 2}, ParseError: &malformed},
		{Source: worker.SourceRef{Line: 3}, Instruction: worker.Instruction{ID: "pending"}},
		{Source: worker.SourceRef{Line: 4}, Instruction: worker.Instruction{ID: "completed"}},
	}
	prior := worker.LogData{
		Validations: []worker.ValidationRow{
			{SourceLine: 1},
			{SourceLine: 2},
		},
		Results: []worker.ResultRow{
			{InstructionID: "completed", Kind: worker.ResultCompleted},
		},
	}

	pending := pendingRows(rows, prior)
	if len(pending) != 1 || pending[0].Source.Line != 3 {
		t.Fatalf("pending rows = %#v, want only source line 3", pending)
	}
}

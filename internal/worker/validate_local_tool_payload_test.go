package worker

import (
	"encoding/json"
	"testing"
)

func TestLocalToolPayloadContractIsSemanticAndStrict(t *testing.T) {
	valid := `{"tool_id":"herdr","tool_version":"1","action_id":"version","input":{}}`
	row := ReadRow{Instruction: Instruction{ID: "i", Version: InstructionVersionV1, Op: "run", Target: "local-tool", Payload: json.RawMessage(valid), CreatedAt: "2026-07-12T00:00:00Z"}}
	if diagnostics := DefaultContract().Validate(row); len(diagnostics) != 0 {
		t.Fatalf("valid local-tool instruction diagnostics=%+v", diagnostics)
	}
	for name, payload := range map[string]string{
		"path-injection": `{"tool_id":"herdr","tool_version":"1","action_id":"version","input":{},"executable":"C:/herdr.exe"}`,
		"binding-leak":   `{"tool_id":"herdr","tool_version":"1","action_id":"version","input":{},"binding_ref":"local-tool.herdr"}`,
		"missing-action": `{"tool_id":"herdr","tool_version":"1","input":{}}`,
		"null-input":     `{"tool_id":"herdr","tool_version":"1","action_id":"version","input":null}`,
		"array-input":    `{"tool_id":"herdr","tool_version":"1","action_id":"version","input":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			row.Instruction.Payload = json.RawMessage(payload)
			diagnostics := DefaultContract().Validate(row)
			if len(diagnostics) == 0 || diagnostics[0].Code != "invalid_payload" {
				t.Fatalf("diagnostics=%+v", diagnostics)
			}
		})
	}
}

package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLocalToolBindingWireRoundTripsWithoutCompatibilityAlias(t *testing.T) {
	row := `{"kind":"hq.local-tool.v1","tool_id":"demo","tool_version":"1","binding_ref":"local-tool.demo","binding_contract_version":"2","bindings":[{"name":"child","binding_ref":"local-tool.child","binding_contract_version":"1"}],"actions":[{"action_id":"start","argv":[{"literal":"run"},{"binding_executable":"child"}],"stdin":{"mode":"none","max_bytes":0},"limits":{"timeout_ms":1000,"stdout_bytes":1024,"stderr_bytes":1024},"output":{"format":"text"},"lifecycle":"one-shot","risk":"low","approval":"explicit"}]}`
	var tool LocalToolDefinition
	if err := json.Unmarshal([]byte(row), &tool); err != nil {
		t.Fatal(err)
	}
	argument := tool.Actions[0].Argv[1]
	if argument.BindingExecutable == nil || *argument.BindingExecutable != "child" || argument.Literal == nil {
		t.Fatalf("decoded binding argument=%+v", argument)
	}
	encoded, err := json.Marshal(tool)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"binding_executable":"child"`) || strings.Contains(text, `"literal":"child"`) {
		t.Fatalf("canonical wire leaked compatibility alias: %s", text)
	}
	var roundTrip LocalToolDefinition
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
}

func TestLocalToolBindingWireRejectsMalformedComposition(t *testing.T) {
	base := `{"kind":"hq.local-tool.v1","tool_id":"demo","tool_version":"1","binding_ref":"local-tool.demo","binding_contract_version":"1","bindings":BINDINGS,"actions":[{"action_id":"start","argv":ARGV,"stdin":{"mode":"none","max_bytes":0},"limits":{"timeout_ms":1000,"stdout_bytes":1024,"stderr_bytes":1024},"output":{"format":"text"},"lifecycle":"one-shot","risk":"low","approval":"explicit"}]}`
	tests := map[string]string{
		"unknown dependency": strings.NewReplacer("BINDINGS", `[]`, "ARGV", `[{"binding_executable":"missing"}]`).Replace(base),
		"mixed tagged union": strings.NewReplacer("BINDINGS", `[{"name":"child","binding_ref":"local-tool.child","binding_contract_version":"1"}]`, "ARGV", `[{"literal":"run","binding_executable":"child"}]`).Replace(base),
		"unsorted bindings": strings.NewReplacer("BINDINGS", `[{"name":"z","binding_ref":"local-tool.z","binding_contract_version":"1"},{"name":"a","binding_ref":"local-tool.a","binding_contract_version":"1"}]`, "ARGV", `[{"literal":"run"}]`).Replace(base),
		"duplicate binding ref": strings.NewReplacer("BINDINGS", `[{"name":"a","binding_ref":"local-tool.same","binding_contract_version":"1"},{"name":"b","binding_ref":"local-tool.same","binding_contract_version":"1"}]`, "ARGV", `[{"literal":"run"}]`).Replace(base),
	}
	for name, row := range tests {
		t.Run(name, func(t *testing.T) {
			var tool LocalToolDefinition
			if err := json.Unmarshal([]byte(row), &tool); err == nil {
				t.Fatalf("accepted malformed local-tool wire: %s", row)
			}
		})
	}
}

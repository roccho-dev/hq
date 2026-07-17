package current

import (
	"strings"
	"testing"
)

func TestSelectedWorldAcceptsFiniteBindingExecutableShape(t *testing.T) {
	world := `{"kind":"hq.world.v1","world_id":"world.binding-proof"}
{"kind":"hq.local-tool.v1","tool_id":"demo","tool_version":"1","binding_ref":"local-tool.demo","binding_contract_version":"2","bindings":[{"name":"child","binding_ref":"local-tool.child","binding_contract_version":"1"}],"actions":[{"action_id":"start","argv":[{"literal":"run"},{"binding_executable":"child"}],"stdin":{"mode":"none","max_bytes":0},"limits":{"timeout_ms":1000,"stdout_bytes":1024,"stderr_bytes":1024},"output":{"format":"text"},"lifecycle":"one-shot","risk":"low","approval":"explicit"}]}
`
	loaded, err := LoadSelectedWorldJSONL(strings.NewReader(world))
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := loaded.LocalTool("demo", "1")
	if !ok || len(tool.Bindings) != 1 || len(tool.Actions) != 1 || tool.Actions[0].Argv[1].BindingExecutable == nil || *tool.Actions[0].Argv[1].BindingExecutable != "child" {
		t.Fatalf("loaded tool=%+v", tool)
	}
}

func TestSchemaRejectsMalformedLocalToolComposition(t *testing.T) {
	base := `{"kind":"hq.local-tool.v1","tool_id":"demo","tool_version":"1","binding_ref":"local-tool.demo","binding_contract_version":"1","bindings":BINDINGS,"actions":[{"action_id":"start","argv":ARGV,"stdin":{"mode":"none","max_bytes":0},"limits":{"timeout_ms":1000,"stdout_bytes":1024,"stderr_bytes":1024},"output":{"format":"text"},"lifecycle":"one-shot","risk":"low","approval":"explicit"}]}`
	tests := map[string]string{
		"unknown dependency": strings.NewReplacer("BINDINGS", `[]`, "ARGV", `[{"binding_executable":"missing"}]`).Replace(base),
		"mixed tagged union": strings.NewReplacer("BINDINGS", `[{"name":"child","binding_ref":"local-tool.child","binding_contract_version":"1"}]`, "ARGV", `[{"literal":"run","binding_executable":"child"}]`).Replace(base),
		"unsorted bindings": strings.NewReplacer("BINDINGS", `[{"name":"z","binding_ref":"local-tool.z","binding_contract_version":"1"},{"name":"a","binding_ref":"local-tool.a","binding_contract_version":"1"}]`, "ARGV", `[{"literal":"run"}]`).Replace(base),
		"duplicate binding ref": strings.NewReplacer("BINDINGS", `[{"name":"a","binding_ref":"local-tool.same","binding_contract_version":"1"},{"name":"b","binding_ref":"local-tool.same","binding_contract_version":"1"}]`, "ARGV", `[{"literal":"run"}]`).Replace(base),
		"primary reused": strings.NewReplacer("BINDINGS", `[{"name":"child","binding_ref":"local-tool.demo","binding_contract_version":"1"}]`, "ARGV", `[{"binding_executable":"child"}]`).Replace(base),
	}
	for name, row := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadSchemaJSONL(strings.NewReader(row + "\n")); err == nil {
				t.Fatalf("accepted malformed local-tool wire: %s", row)
			}
		})
	}
}

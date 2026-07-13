package hq

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestWSLCCommandCompletesAndLowersSemanticIdentityOnly(t *testing.T) {
	data, err := os.ReadFile("../../examples/hq.local-tools.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	world, err := LoadSchemaJSONL(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	suggestions := Complete("@wslc", len("@wslc"), world)
	if len(suggestions) != 1 || suggestions[0].Label != "wslc.version" || suggestions[0].Edit.Text != "@wslc.version" {
		t.Fatalf("suggestions=%#v", suggestions)
	}

	draft, err := CompileCommandObject("@wslc.version", 0, world)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]any{
		"version": "instruction.v1",
		"op":      "run",
		"target":  "local-tool",
		"payload": map[string]any{
			"tool_id":      "wslc",
			"tool_version": "2.9.3.0",
			"action_id":    "version",
			"input":        map[string]any{},
		},
	}
	if draft.Kind != "accepted.instruction" || draft.Queue != "instruction.jsonl" || !reflect.DeepEqual(draft.Instruction, expected) {
		t.Fatalf("draft=%#v", draft)
	}
	encoded, err := json.Marshal(draft.Instruction)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"binding_ref", "executable", "materialDigest", "material_digest", "deployment", "envctl", `"argv"`, "Program Files", "wslc.exe"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("provider/runtime data %q leaked into instruction: %s", forbidden, encoded)
		}
	}
}

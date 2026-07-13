package current

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const wslcWorldPath = "../../../examples/hq.local-tools.jsonl"

func TestWSLCLocalToolWorldLoadsExactData(t *testing.T) {
	data := readWSLCWorld(t)
	world, err := LoadSchemaJSONL(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	tool, ok := world.LocalTool("wslc", "2.9.3.0")
	if !ok || tool.BindingRef != "local-tool.wslc" || tool.BindingContractVersion != "1" {
		t.Fatalf("tool=%#v ok=%v", tool, ok)
	}
	if len(tool.Actions) != 1 {
		t.Fatalf("actions=%#v", tool.Actions)
	}
	action := tool.Actions[0]
	if action.ActionID != "version" || len(action.Inputs) != 0 || len(action.Argv) != 1 || action.Argv[0].Literal == nil || *action.Argv[0].Literal != "version" || action.Argv[0].Field != nil {
		t.Fatalf("action=%#v", action)
	}
	command, ok := world.Command("wslc.version")
	if !ok || len(command.Fields) != 0 {
		t.Fatalf("command=%#v ok=%v", command, ok)
	}
	payload, ok := command.Instruction["payload"].(map[string]any)
	if !ok || payload["tool_id"] != "wslc" || payload["tool_version"] != "2.9.3.0" || payload["action_id"] != "version" || !reflect.DeepEqual(payload["input"], map[string]any{}) {
		t.Fatalf("instruction=%#v", command.Instruction)
	}
}

func TestWSLCWorldRowsAreStrictAndContainNoProviderData(t *testing.T) {
	worldData := readWSLCWorld(t)
	for _, forbidden := range []string{"executable", "material_digest", "materialDigest", "deployment_id", "deploymentId", "Program Files", "wslc.exe"} {
		if strings.Contains(worldData, forbidden) {
			t.Fatalf("world leaks provider data %q: %s", forbidden, worldData)
		}
	}
	invalid := strings.Replace(worldData, `"binding_ref":"local-tool.wslc"`, `"binding_ref":"local-tool.wslc","provider":"forbidden"`, 1)
	if _, err := LoadSchemaJSONL(strings.NewReader(invalid)); err == nil {
		t.Fatal("strict loader accepted an unknown provider field")
	}
}

func readWSLCWorld(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(wslcWorldPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

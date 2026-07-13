package current

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const wslcWorldPath = "../../../examples/hq.local-tools.jsonl"

func TestWSLCSelectedWorldLoadsExactDataOnlyContract(t *testing.T) {
	data := readWSLCWorld(t)
	world, err := LoadRuntimeWorldJSONL(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, selected := world.SelectedRef(); !selected {
		t.Fatal("WSLC example did not load as an explicit selected world")
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
	if !ok || command.CommandID != "wslc.version" || command.CommandVersion != "1" || len(command.Fields) != 0 {
		t.Fatalf("command=%#v ok=%v", command, ok)
	}
	payload, ok := command.Instruction["payload"].(map[string]any)
	if !ok || payload["tool_id"] != "wslc" || payload["tool_version"] != "2.9.3.0" || payload["action_id"] != "version" || !reflect.DeepEqual(payload["input"], map[string]any{}) {
		t.Fatalf("instruction=%#v", command.Instruction)
	}
	if ref, ok := world.CommandRef("wslc.version"); !ok || ref.CommandID != "wslc.version" || ref.CommandVersion != "1" || ref.Digest == "" {
		t.Fatalf("command ref=%+v ok=%v", ref, ok)
	}
}

func TestWSLCWorldContainsNoProviderOrKnownFolderData(t *testing.T) {
	worldData := readWSLCWorld(t)
	for _, forbidden := range []string{
		"executable_path", "material_digest", "materialDigest", "deployment_id", "deploymentId",
		"Program Files", "ProgramFiles", "wslc.exe", "envctl.verified-executable-bindings.v1",
	} {
		if strings.Contains(worldData, forbidden) {
			t.Fatalf("world leaks provider data %q: %s", forbidden, worldData)
		}
	}
	invalid := strings.Replace(worldData, `"binding_ref":"local-tool.wslc"`, `"binding_ref":"local-tool.wslc","provider":"forbidden"`, 1)
	if _, err := LoadRuntimeWorldJSONL(strings.NewReader(invalid)); err == nil {
		t.Fatal("strict loader accepted an unknown provider field")
	}
}

func TestWSLCRequiresExactToolVersionAndCommandIdentity(t *testing.T) {
	world, err := LoadRuntimeWorldJSONL(strings.NewReader(readWSLCWorld(t)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := world.LocalTool("wslc", "2.9.3"); ok {
		t.Fatal("tool lookup ignored exact WSLC version")
	}
	if _, ok := world.CommandByID("wslc"); ok {
		t.Fatal("command lookup accepted an abbreviated identity")
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

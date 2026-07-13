package hq

import (
	"encoding/json"
	"strings"
	"testing"
)

const hostCommand = `{"kind":"hq.command.v1","name":"host.open","description":"open a host path","instruction":{"version":"instruction.v1","op":"run","target":"host","payload":{"capability":"host.open"}},"fields":[{"name":"path","type":"path","required":true,"examples":["C:\\work"],"bind":"payload.path"}]}`
const herdrCommand = `{"kind":"hq.command.v1","name":"herdr.read","description":"read agent output","instruction":{"version":"instruction.v1","op":"run","target":"herdr","payload":{"action":"read"}},"fields":[{"name":"agent","type":"string","required":true,"examples":["reviewer"],"bind":"payload.agent"},{"name":"source","type":"enum","required":true,"enum":["recent-unwrapped","screen"],"bind":"payload.source"},{"name":"lines","type":"integer","required":true,"examples":["100"],"bind":"payload.lines"}]}`

func TestCommandWorldDataAloneExpandsCompletion(t *testing.T) {
	hostOnly, err := LoadSchemaJSONL(strings.NewReader(hostCommand))
	if err != nil {
		t.Fatal(err)
	}
	if got := Complete("", 0, hostOnly); len(got) != 1 || got[0].Label != "host.open" {
		t.Fatalf("host-only suggestions=%#v", got)
	}

	expanded, err := LoadSchemaJSONL(strings.NewReader(hostCommand + "\n" + herdrCommand))
	if err != nil {
		t.Fatal(err)
	}
	got := Complete("@her", 4, expanded)
	if len(got) != 1 || got[0].Label != "herdr.read" {
		t.Fatalf("expanded suggestions=%#v", got)
	}
	fieldsBuffer := "@herdr.read\n"
	fields := Complete(fieldsBuffer, len(fieldsBuffer), expanded)
	if !hasSuggestion(fields, "agent") || !hasSuggestion(fields, "source") || !hasSuggestion(fields, "lines") {
		t.Fatalf("field suggestions=%#v", fields)
	}
	for _, suggestion := range fields {
		if suggestion.Label == "source" && (suggestion.Edit.Text != "source=" || suggestion.Edit.Start != len("@herdr.read\n")) {
			t.Fatalf("field completion must replace the whole current line: %#v", suggestion.Edit)
		}
	}
	valuesBuffer := "@herdr.read\nsource=s"
	values := Complete(valuesBuffer, len(valuesBuffer), expanded)
	if !hasSuggestion(values, "screen") {
		t.Fatalf("enum suggestions=%#v", values)
	}
}

func TestLocalToolCommandLowersSemanticIdentityWithoutProviderData(t *testing.T) {
	tool := `{"kind":"hq.local-tool.v1","tool_id":"dummy","tool_version":"1","binding_ref":"local-tool.dummy","binding_contract_version":"1","actions":[{"action_id":"echo","inputs":[{"name":"value","type":"string","required":true}],"argv":[{"literal":"echo"},{"field":"value"}],"stdin":{"mode":"none","max_bytes":0},"limits":{"timeout_ms":1000,"stdout_bytes":1024,"stderr_bytes":1024},"output":{"format":"text"},"lifecycle":"one-shot","risk":"low","approval":"explicit"}]}`
	command := `{"kind":"hq.command.v1","name":"dummy.echo","instruction":{"version":"instruction.v1","op":"run","target":"local-tool","payload":{"tool_id":"dummy","tool_version":"1","action_id":"echo","input":{}}},"fields":[{"name":"value","type":"string","required":true,"bind":"payload.input.value"}]}`
	world, err := LoadSchemaJSONL(strings.NewReader(tool + "\n" + command))
	if err != nil {
		t.Fatal(err)
	}
	draft, err := CompileCommandObject("@dummy.echo\nvalue=\";&|$()\"", 1, world)
	if err != nil {
		t.Fatal(err)
	}
	payload := draft.Instruction["payload"].(map[string]any)
	input := payload["input"].(map[string]any)
	if draft.Instruction["target"] != "local-tool" || payload["tool_id"] != "dummy" || payload["tool_version"] != "1" || payload["action_id"] != "echo" || input["value"] != `;&|$()` {
		t.Fatalf("instruction=%#v", draft.Instruction)
	}
	encoded, _ := json.Marshal(draft.Instruction)
	for _, forbidden := range []string{"binding_ref", "executable", "materialDigest", `"argv"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("provider/runtime data leaked into instruction: %s", encoded)
		}
	}
}

func TestCommandLoweringIsDeclaredByWorld(t *testing.T) {
	world, err := LoadSchemaJSONL(strings.NewReader(hostCommand + "\n" + herdrCommand))
	if err != nil {
		t.Fatal(err)
	}
	hostBuffer := "@host.open\npath=\"C:\\work tree\""
	draft, err := CompileCommandObject(hostBuffer, 1, world)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Instruction["target"] != "host" || draft.Instruction["op"] != "run" {
		t.Fatalf("instruction=%#v", draft.Instruction)
	}
	payload := draft.Instruction["payload"].(map[string]any)
	if payload["capability"] != "host.open" || payload["path"] != `C:\work tree` {
		t.Fatalf("payload=%#v", payload)
	}

	herdrBuffer := "@host.open\npath=C:\\work\n\n@herdr.read\nagent=reviewer\nsource=screen\nlines=100"
	herdr, err := CompileCommandObject(herdrBuffer, 5, world)
	if err != nil {
		t.Fatal(err)
	}
	herdrPayload := herdr.Instruction["payload"].(map[string]any)
	if herdrPayload["action"] != "read" || herdrPayload["lines"] != 100 {
		t.Fatalf("herdr payload=%#v", herdrPayload)
	}
}

func TestCommandValidationIsLineScoped(t *testing.T) {
	world, err := LoadSchemaJSONL(strings.NewReader(hostCommand))
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := ValidateCommandDocument("@host.open\npath=C:\\work\n\n@host.open", world)
	if len(diagnostics) != 1 || diagnostics[0].Line != 3 || !strings.Contains(diagnostics[0].Message, `required field "path"`) {
		t.Fatalf("diagnostics=%#v", diagnostics)
	}
}

func TestCommandWorldCannotOwnAcceptedIdentity(t *testing.T) {
	for _, invalid := range []string{
		`{"kind":"hq.command.v1","name":"bad.base","instruction":{"id":"user-id"}}`,
		`{"kind":"hq.command.v1","name":"bad.bind","instruction":{},"fields":[{"name":"id","type":"string","bind":"id"}]}`,
	} {
		if _, err := LoadSchemaJSONL(strings.NewReader(invalid)); err == nil {
			t.Fatalf("reserved identity definition loaded: %s", invalid)
		}
	}
}

func hasSuggestion(suggestions []Suggestion, label string) bool {
	for _, suggestion := range suggestions {
		if suggestion.Label == label {
			return true
		}
	}
	return false
}

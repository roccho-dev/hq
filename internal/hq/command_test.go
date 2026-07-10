package hq

import (
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

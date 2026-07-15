package hq

import (
	"strings"
	"testing"
)

func TestAcceptedInputComesFromParsedCommandAndOmitsDeniedFields(t *testing.T) {
	world, err := LoadSelectedWorldJSONL(strings.NewReader(
		`{"kind":"hq.world.v1","world_id":"accepted.test"}` + "\n" +
			`{"kind":"hq.command.v1","command_id":"demo","command_version":"1","name":"demo","instruction":{"version":"instruction.v1","op":"run","target":"demo","payload":{}},"fields":[{"name":"safe","type":"string","required":true,"history_policy":"search","bind":"payload.safe"},{"name":"private","type":"string","required":true,"history_policy":"deny","bind":"payload.private"}]}`))
	if err != nil { t.Fatal(err) }
	draft, _, err := CompileSelectedCommandObjectWithRange("@demo\nsafe=visible\nprivate=hidden", 1, world)
	if err != nil { t.Fatal(err) }
	if draft.AcceptedInput == nil || len(draft.AcceptedInput.Fields) != 1 || draft.AcceptedInput.Fields[0].Name != "safe" || draft.AcceptedInput.Fields[0].Value != "visible" {
		t.Fatalf("accepted input=%#v", draft.AcceptedInput)
	}
	if draft.AcceptedInput.RecallComplete || draft.AcceptedInput.SuppliedFieldCount != 2 {
		t.Fatalf("omission state=%#v", draft.AcceptedInput)
	}
	draft.Instruction["id"] = "ins-test"
	draft.Instruction["created_at"] = "2026-07-16T00:00:00Z"
	if err := BindFinalInstructionProvenance(&draft, world, CommandInputKind); err != nil { t.Fatal(err) }
	if draft.AcceptedInput.InstructionDigest != draft.Provenance.InstructionDigest || draft.AcceptedInput.AcceptedInputDigest == "" {
		t.Fatalf("unbound evidence=%#v provenance=%#v", draft.AcceptedInput, draft.Provenance)
	}
	encoded := draft.AcceptedInput.AcceptedInputDigest + draft.AcceptedInput.Fields[0].Name
	if strings.Contains(encoded, "private") || strings.Contains(encoded, "hidden") {
		t.Fatalf("denied field leaked: %s", encoded)
	}
}

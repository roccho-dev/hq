package core

import "testing"

func TestAcceptedDraftDoesNotInventDomainMeaning(t *testing.T) {
	instruction := map[string]any{
		"action": "adapter.defined",
		"value":  42,
	}

	draft := AcceptedDraft(instruction)

	if _, ok := draft.Instruction["op"]; ok {
		t.Fatalf("core invented domain field op: %#v", draft.Instruction)
	}
	if got := draft.Instruction["action"]; got != "adapter.defined" {
		t.Fatalf("core changed opaque adapter value: got %#v", got)
	}
	if got := draft.Instruction["value"]; got != 42 {
		t.Fatalf("core changed opaque data: got %#v", got)
	}
}

func TestAcceptedDraftAllowsEmptyOpaqueInstruction(t *testing.T) {
	draft := AcceptedDraft(nil)

	if draft.Instruction == nil {
		t.Fatal("core must return a writable instruction object")
	}
	if len(draft.Instruction) != 0 {
		t.Fatalf("core invented domain meaning: %#v", draft.Instruction)
	}
}

package hq

import "testing"

func TestRequiredKeysFirst(t *testing.T) {
	w := DefaultWorld()
	got := Complete(`{"`, 2, w)
	if len(got) == 0 {
		t.Fatal("no suggestions")
	}
	if got[0].Draft.Key != "op" && got[0].Draft.Key != "payload" && got[0].Draft.Key != "target" {
		t.Fatalf("first suggestion should be a missing required key, got %#v", got[0])
	}
	if got[0].Draft.Queue != "instruction.jsonl" {
		t.Fatalf("compileDraft missing queue: %#v", got[0].Draft)
	}
}

func TestEnumValueSuggestion(t *testing.T) {
	w := DefaultWorld()
	buf := `{"op":q`
	got := Complete(buf, len(buf), w)
	if len(got) == 0 {
		t.Fatal("no value suggestions")
	}
	if got[0].Draft.Key != "op" {
		t.Fatalf("wrong key: %#v", got[0])
	}
	if got[0].Draft.Value != "queue.create" && got[0].Draft.Value != "queue.dispatch" && got[0].Draft.Value != "queue.preview" {
		t.Fatalf("wrong enum value: %#v", got[0].Draft.Value)
	}
}

func TestApplyEdit(t *testing.T) {
	w := DefaultWorld()
	buf := `{"op":q`
	got := Complete(buf, len(buf), w)
	applied := Apply(buf, got[0].Edit)
	if applied == buf || len(applied) <= len(buf) {
		t.Fatalf("edit did not apply: %q -> %q", buf, applied)
	}
}

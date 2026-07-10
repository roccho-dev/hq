package workeraccept

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"hq/internal/core"
	"hq/internal/worker"
)

const canonicalInstruction = `{"id":"ins-accepted-001","version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["printf","hello"],"cwd":"."},"created_at":"2026-07-10T06:00:00Z"}`

func TestReadActualCoreAcceptedDraftWithoutSemanticRewrite(t *testing.T) {
	var inner map[string]any
	if err := json.Unmarshal([]byte(canonicalInstruction), &inner); err != nil {
		t.Fatal(err)
	}
	accepted, err := json.Marshal(core.AcceptedDraft(inner))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := Read("accepted.jsonl", bytes.NewReader(append(accepted, '\n')))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d", len(rows))
	}
	if diagnostics := worker.DefaultContract().Validate(rows[0]); len(diagnostics) != 0 {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
	got, err := json.Marshal(rows[0].Instruction)
	if err != nil {
		t.Fatal(err)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(canonicalInstruction), &wantValue); err != nil {
		t.Fatal(err)
	}
	gotCanonical, _ := json.Marshal(gotValue)
	wantCanonical, _ := json.Marshal(wantValue)
	if !bytes.Equal(gotCanonical, wantCanonical) {
		t.Fatalf("semantic rewrite\n got=%s\nwant=%s", gotCanonical, wantCanonical)
	}
	if rows[0].Source.Raw != string(accepted) || rows[0].Source.Line != 1 {
		t.Fatalf("source evidence drifted: %+v", rows[0].Source)
	}
}

func TestReadFailsClosedPerLineAndContinues(t *testing.T) {
	valid := `{"kind":"accepted.instruction","queue":"instruction.jsonl","instruction":` + canonicalInstruction + `}`
	input := strings.Join([]string{
		`{"kind":"draft.instruction","queue":"instruction.jsonl","instruction":{}}`,
		`{"kind":"accepted.instruction","queue":"other.jsonl","instruction":{}}`,
		`{"kind":"accepted.instruction","queue":"instruction.jsonl"}`,
		valid,
	}, "\n")
	rows, err := Read("accepted.jsonl", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows=%d", len(rows))
	}
	for index := 0; index < 3; index++ {
		if rows[index].ParseError == nil || rows[index].ParseError.Code != "invalid_accepted_envelope" {
			t.Fatalf("row %d did not fail closed: %+v", index, rows[index])
		}
	}
	if rows[3].Instruction.ID != "ins-accepted-001" {
		t.Fatalf("later valid row hidden: %+v", rows[3])
	}
}

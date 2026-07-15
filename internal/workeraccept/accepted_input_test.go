package workeraccept

import (
	"strings"
	"testing"
)

func TestAcceptedInputIsDiscardedBeforeWorkerMeaning(t *testing.T) {
	row := `{"kind":"accepted.instruction","queue":"instruction.jsonl","instruction":` + canonicalInstruction + `,"accepted_input":{"unsupported":"evidence"}}`
	rows, err := Read("accepted.jsonl", strings.NewReader(row+"\n"))
	if err != nil { t.Fatal(err) }
	if len(rows) != 1 || rows[0].ParseError != nil || rows[0].Instruction.ID != "ins-accepted-001" {
		t.Fatalf("worker row=%#v", rows)
	}
}
